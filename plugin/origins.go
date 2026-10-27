package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// AgentRouter serves one account from two origins:
//
//	https://ps.air-outer.com   mainland origin. Reachable without a proxy and
//	                           the origin inference (/v1/*) and /v1/models run
//	                           against. Its WAF challenges console (/api/*)
//	                           calls from scripted clients the most.
//	https://agentrouter.org    canonical console. Its DNS answer is poisoned on
//	                           mainland networks, so it needs an HTTP proxy.
//
// The plugin therefore never assumes a single origin works. Every request
// resolves its origin at call time: the remembered origin first, the configured
// origin second, the alternate origin last. Only transport failures and WAF
// interstitials switch origins; a real upstream answer (4xx/5xx JSON) never
// does, so behavior stays predictable. An origin that just failed is skipped
// for a short while instead of being dialed again on the next call.
const (
	mainlandOrigin = "https://ps.air-outer.com"
	globalOrigin   = "https://agentrouter.org"

	originRememberTTL = 10 * time.Minute
	originPenaltyTTL  = 2 * time.Minute
)

// alternateOriginTestHook lets tests redirect failover to a stub server.
var alternateOriginTestHook string

// alternateOrigin returns the other origin serving the same account.
func alternateOrigin(origin string) string {
	if alternateOriginTestHook != "" {
		return alternateOriginTestHook
	}
	if originNeedsProxy(origin) {
		return mainlandOrigin
	}
	return globalOrigin
}

// originNeedsProxy reports whether the origin is only reachable through the
// configured proxy (mainland DNS answers for agentrouter.org are poisoned).
func originNeedsProxy(origin string) bool {
	return strings.Contains(strings.ToLower(origin), "agentrouter.org")
}

// proxyForOrigin returns the proxy a call to origin must use. The mainland
// origin always goes direct: routing it through an overseas proxy is what made
// console calls fail with WAF interstitials in the past.
func proxyForOrigin(cfg pluginConfig, origin string) string {
	if originNeedsProxy(origin) {
		return strings.TrimSpace(cfg.ProxyURL)
	}
	return ""
}

// originMemory remembers the origin that served the last successful call and
// the origins that recently failed.
type originMemory struct {
	mu     sync.Mutex
	origin string
	until  time.Time
	failed map[string]time.Time
}

func (m *originMemory) get() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.origin == "" || time.Now().After(m.until) {
		m.origin = ""
		return ""
	}
	return m.origin
}

// remember records origin as the working one and reports whether the
// remembered value changed (used to log a switch only once).
func (m *originMemory) remember(origin string) bool {
	origin = normalizeBaseURL(origin)
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := m.origin != origin
	m.origin = origin
	m.until = time.Now().Add(originRememberTTL)
	if m.failed != nil {
		delete(m.failed, origin)
	}
	return changed
}

// markFailed skips origin for a short period so one broken origin does not
// stall every request with a TCP timeout.
func (m *originMemory) markFailed(origin string) {
	origin = normalizeBaseURL(origin)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failed == nil {
		m.failed = make(map[string]time.Time)
	}
	m.failed[origin] = time.Now().Add(originPenaltyTTL)
	if m.origin == origin {
		m.origin = ""
		m.until = time.Time{}
	}
}

func (m *originMemory) penalized(origin string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	until, ok := m.failed[normalizeBaseURL(origin)]
	if !ok {
		return false
	}
	if now.After(until) {
		delete(m.failed, origin)
		return false
	}
	return true
}

func (m *originMemory) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.origin = ""
	m.until = time.Time{}
	m.failed = nil
}

// candidates orders the origins to try for one call. Penalized origins move to
// the end; when every origin is penalized the full list is returned anyway so a
// stale penalty can never strand a request.
func (m *originMemory) candidates(configured string) []string {
	configured = normalizeBaseURL(configured)
	remembered := m.get()
	now := time.Now()
	seen := make(map[string]struct{}, 3)
	ordered := make([]string, 0, 3)
	penalized := make([]string, 0, 3)
	add := func(origin string) {
		origin = strings.TrimSuffix(strings.TrimSpace(origin), "/")
		if origin == "" {
			return
		}
		if _, ok := seen[origin]; ok {
			return
		}
		seen[origin] = struct{}{}
		if m.penalized(origin, now) {
			penalized = append(penalized, origin)
			return
		}
		ordered = append(ordered, origin)
	}
	add(remembered)
	add(configured)
	add(alternateOrigin(configured))
	ordered = append(ordered, penalized...)
	return ordered
}

var (
	inferenceOriginMemory = &originMemory{}
	consoleOriginMemory   = &originMemory{}
)

// originHTTPClient returns the shared client for one origin. Proxied origins
// use the configured proxy; the mainland origin always connects directly.
func originHTTPClient(cfg pluginConfig, origin string, timeout time.Duration) *http.Client {
	return sharedHTTPClient(proxyForOrigin(cfg, origin), timeout)
}

var sharedHTTPClients = struct {
	mu    sync.Mutex
	byKey map[string]*http.Client
}{byKey: map[string]*http.Client{}}

// sharedHTTPClient builds (and caches) one client per proxy/timeout pair.
// Dialing prefers IPv4 because the console publishes an AAAA record that is
// unreachable on some mainland networks; a zero timeout means "no overall
// deadline", which streaming responses need.
func sharedHTTPClient(proxyURL string, timeout time.Duration) *http.Client {
	key := proxyURL + "|" + timeout.String()
	sharedHTTPClients.mu.Lock()
	defer sharedHTTPClients.mu.Unlock()
	if client, ok := sharedHTTPClients.byKey[key]; ok {
		return client
	}
	dialer := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, errDial := dialer.DialContext(ctx, "tcp4", addr)
			if errDial == nil {
				return conn, nil
			}
			return dialer.DialContext(ctx, network, addr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if proxyURL != "" {
		if parsed, errParse := url.Parse(proxyURL); errParse == nil && parsed.Host != "" {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	client := &http.Client{Transport: transport, Timeout: timeout}
	sharedHTTPClients.byKey[key] = client
	return client
}

// isTransportFailure reports whether err means the request never reached the
// upstream (DNS, TCP, TLS or proxy CONNECT). Those are the only failures worth
// retrying on the alternate origin; HTTP status errors are handled as answers.
func isTransportFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var netError net.Error
	if errors.As(err, &netError) {
		return true
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return true
	}
	var opError *net.OpError
	if errors.As(err, &opError) {
		return true
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, needle := range []string{
		"dial tcp", "connectex", "no such host", "connection refused",
		"connection reset", "network is unreachable", "i/o timeout",
		"tls handshake timeout", "proxyconnect", "eof",
	} {
		if strings.Contains(message, needle) {
			return true
		}
	}
	return false
}
