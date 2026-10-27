package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// Read-only dashboard payloads change slowly and the upstream console is the
// bottleneck of every tab load. These TTLs keep repeat visits instant while
// staying short enough that balances and usage charts still look live.
const (
	cacheTTLDashboard     = 15 * time.Second
	cacheTTLLogs          = 15 * time.Second
	cacheTTLAccount       = 20 * time.Second
	cacheTTLAnnouncements = 120 * time.Second
	cacheTTLTokens        = 30 * time.Second
)

// maxCacheEntries bounds the cache; log-page keys are the unbounded part.
const maxCacheEntries = 256

type cacheEntry struct {
	payload    map[string]any
	expiresAt  time.Time
	generation uint64
}

// responseCache is a tiny TTL cache for management JSON payloads. Entries are
// scoped by a caller-supplied key (credentials + query parameters) and carry
// the generation they were stored in so an invalidation also discards results
// of requests that were already in flight when the credentials changed.
type responseCache struct {
	mu         sync.Mutex
	now        func() time.Time
	generation uint64
	entries    map[string]cacheEntry
}

func newResponseCache() *responseCache {
	return &responseCache{now: time.Now, entries: map[string]cacheEntry{}}
}

// currentGeneration returns the value to hold on to while fetching. A result
// may only be stored if the generation is still current at store time.
func (c *responseCache) currentGeneration() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation
}

// get returns a shallow copy of a live entry. Nested values are never mutated
// after storage; callers only add top-level flags to their copy.
func (c *responseCache) get(key string, generation uint64) (map[string]any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || entry.generation != generation {
		return nil, false
	}
	if !c.now().Before(entry.expiresAt) {
		delete(c.entries, key)
		return nil, false
	}
	out := make(map[string]any, len(entry.payload))
	for name, value := range entry.payload {
		out[name] = value
	}
	return out, true
}

// set stores a payload if no invalidation happened since the fetch started.
func (c *responseCache) set(key string, generation uint64, payload map[string]any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return
	}
	now := c.now()
	if len(c.entries) >= maxCacheEntries {
		c.sweepLocked(now)
	}
	c.entries[key] = cacheEntry{payload: payload, expiresAt: now.Add(ttl), generation: generation}
}

func (c *responseCache) sweepLocked(now time.Time) {
	for key, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
	if len(c.entries) >= maxCacheEntries {
		c.entries = map[string]cacheEntry{}
	}
}

// invalidate drops every entry and starts a new generation, which also
// prevents in-flight fetches from repopulating the cache with pre-change data.
func (c *responseCache) invalidate() {
	c.mu.Lock()
	c.generation++
	c.entries = map[string]cacheEntry{}
	c.mu.Unlock()
}

// flightResult is one in-flight or finished upstream fetch shared by every
// caller that asked for the same key while it was running.
type flightResult struct {
	done      chan struct{}
	payload   map[string]any
	status    int
	cacheable bool
}

// flightGroup is a minimal singleflight implementation. The plugin module does
// not depend on golang.org/x/sync, so this is hand rolled.
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightResult
}

func newFlightGroup() *flightGroup {
	return &flightGroup{calls: map[string]*flightResult{}}
}

// do runs fetch unless an identical call is already running, in which case it
// waits for and shares that call's result. The bool reports whether this
// caller led the fetch.
func (g *flightGroup) do(key string, fetch func() (map[string]any, int, bool)) (*flightResult, bool) {
	g.mu.Lock()
	if pending, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-pending.done
		return pending, false
	}
	pending := &flightResult{done: make(chan struct{})}
	g.calls[key] = pending
	g.mu.Unlock()

	payload, status, cacheable := fetch()
	pending.payload = payload
	pending.status = status
	pending.cacheable = cacheable
	close(pending.done)

	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
	return pending, true
}

// responseStore combines the TTL cache and the coalescing group for one class
// of read-only management responses.
type responseStore struct {
	cache   *responseCache
	flights *flightGroup
}

func newResponseStore() *responseStore {
	return &responseStore{cache: newResponseCache(), flights: newFlightGroup()}
}

var managementStore = newResponseStore()

// invalidateManagementCache is called whenever credentials or configuration
// change (login, token save, settings save, check-in).
func invalidateManagementCache() {
	managementStore.cache.invalidate()
}

// serve answers a read-only management request from the cache, from an
// identical in-flight fetch, or by calling fetch. Only successful payloads are
// cached; errors always go upstream so a transient failure is never sticky.
// A refresh skips both the cache and coalescing: it must observe upstream now.
func (s *responseStore) serve(key string, ttl time.Duration, refresh bool, fetch func() (map[string]any, int, bool)) pluginapi.ManagementResponse {
	if refresh {
		payload, status, cacheable := fetch()
		result := withCachedFlag(payload, false)
		// A forced refresh must also replace the normal entry; otherwise the next
		// ordinary read would serve the pre-refresh snapshot for a whole TTL.
		if cacheable && status == http.StatusOK {
			s.cache.set(key, s.cache.currentGeneration(), result, ttl)
		}
		return jsonResponse(status, result)
	}
	generation := s.cache.currentGeneration()
	if payload, ok := s.cache.get(key, generation); ok {
		return jsonResponse(http.StatusOK, withCachedFlag(payload, true))
	}
	// Scope coalescing to the credential generation too, so a caller that
	// arrives after an invalidation never joins a fetch started with the old
	// settings.
	result, _ := s.flights.do(strconv.FormatUint(generation, 10)+"|"+key, fetch)
	payload := withCachedFlag(result.payload, false)
	if result.cacheable && result.status == http.StatusOK {
		s.cache.set(key, generation, payload, ttl)
	}
	return jsonResponse(result.status, payload)
}

// withCachedFlag returns a shallow copy carrying the UI hint that tells the
// console whether it is looking at cached data.
func withCachedFlag(payload map[string]any, cached bool) map[string]any {
	out := make(map[string]any, len(payload)+1)
	for name, value := range payload {
		out[name] = value
	}
	out["cached"] = cached
	return out
}

// managementCacheKey scopes a cached payload to the effective credentials
// identity (user id plus token/session fingerprints) and the query parameters.
// The "refresh" flag is stripped: a refresh bypasses the cache read, it is not
// a different dataset.
func managementCacheKey(kind string, cfg pluginConfig, client accountClient, query url.Values) string {
	var builder strings.Builder
	builder.WriteString(kind)
	builder.WriteString("|host=")
	builder.WriteString(strings.ToLower(accountBaseURL(cfg)))
	builder.WriteString("|user=")
	builder.WriteString(strings.TrimSpace(client.userID))
	builder.WriteString("|token=")
	builder.WriteString(secretFingerprint(client.token))
	builder.WriteString("|session=")
	builder.WriteString(secretFingerprint(client.session))
	builder.WriteString("|key=")
	builder.WriteString(secretFingerprint(cfg.APIKey))
	if len(query) > 0 {
		cleaned := url.Values{}
		for name, values := range query {
			if name == "refresh" {
				continue
			}
			cleaned[name] = append([]string(nil), values...)
		}
		if encoded := cleaned.Encode(); encoded != "" {
			builder.WriteString("|q=")
			builder.WriteString(encoded)
		}
	}
	return builder.String()
}

// secretFingerprint derives a stable, non-reversible id from a credential so
// the cache key never contains the secret itself.
func secretFingerprint(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "-"
	}
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:8])
}

// logsCacheQuery normalizes the usage-log query for cache keying. The console
// computes start/end from the wall clock on every load, so the raw second
// values would never repeat; bucketing them to 30s keeps repeated tab switches
// cacheable while the window still shifts by less than the cache TTL.
func logsCacheQuery(query url.Values) url.Values {
	normalized := url.Values{}
	for name, values := range query {
		if name == "refresh" {
			continue
		}
		normalized[name] = append([]string(nil), values...)
	}
	for _, name := range []string{"start", "end"} {
		value := strings.TrimSpace(normalized.Get(name))
		if value == "" {
			continue
		}
		if parsed, errParse := strconv.ParseInt(value, 10, 64); errParse == nil {
			normalized.Set(name, strconv.FormatInt(parsed/30*30, 10))
		}
	}
	return normalized
}

// refreshRequested reports whether the console asked to bypass the cache.
func refreshRequested(req pluginapi.ManagementRequest) bool {
	value := strings.TrimSpace(req.Query.Get("refresh"))
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}
