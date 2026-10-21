package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func newTestStore() (*responseStore, *fakeClock) {
	store := newResponseStore()
	clock := &fakeClock{now: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
	store.cache.now = clock.Now
	return store, clock
}

func fetchOnce(calls *int32, payload map[string]any) func() (map[string]any, int, bool) {
	return func() (map[string]any, int, bool) {
		atomic.AddInt32(calls, 1)
		return payload, http.StatusOK, true
	}
}

func TestCacheServesPayloadWithinTTL(t *testing.T) {
	store, clock := newTestStore()
	var calls int32
	fetch := fetchOnce(&calls, map[string]any{"success": true, "value": 1})

	first := store.serve("k", time.Minute, false, fetch)
	if calls != 1 {
		t.Fatalf("calls after first = %d", calls)
	}
	if cacheFlag(t, first) {
		t.Fatal("first response must be marked cached:false")
	}
	clock.Advance(30 * time.Second)
	second := store.serve("k", time.Minute, false, fetch)
	if calls != 1 {
		t.Fatalf("calls within TTL = %d, want 1", calls)
	}
	if !cacheFlag(t, second) {
		t.Fatal("cached response must be marked cached:true")
	}
	if payloadField(second) != float64(1) {
		t.Fatalf("cached payload missing value: %s", second.Body)
	}
}

func TestCacheExpiresAfterTTL(t *testing.T) {
	store, clock := newTestStore()
	var calls int32
	fetch := fetchOnce(&calls, map[string]any{"success": true})

	store.serve("k", 15*time.Second, false, fetch)
	clock.Advance(15 * time.Second)
	store.serve("k", 15*time.Second, false, fetch)
	if calls != 2 {
		t.Fatalf("calls after TTL = %d, want 2", calls)
	}
}

func TestCacheInvalidationDropsEntries(t *testing.T) {
	store, _ := newTestStore()
	var calls int32
	fetch := fetchOnce(&calls, map[string]any{"success": true})

	store.serve("k", time.Minute, false, fetch)
	store.cache.invalidate()
	store.serve("k", time.Minute, false, fetch)
	if calls != 2 {
		t.Fatalf("calls after invalidate = %d, want 2", calls)
	}
}

func TestCacheInvalidationRejectsInFlightResult(t *testing.T) {
	store, _ := newTestStore()
	generation := store.cache.currentGeneration()
	store.cache.invalidate()
	store.cache.set("k", generation, map[string]any{"success": true}, time.Minute)
	if _, ok := store.cache.get("k", store.cache.currentGeneration()); ok {
		t.Fatal("result stored before invalidation must be discarded")
	}
}

func TestCacheNeverStoresErrors(t *testing.T) {
	store, _ := newTestStore()
	var calls int32
	failing := func() (map[string]any, int, bool) {
		atomic.AddInt32(&calls, 1)
		return map[string]any{"success": false, "message": "boom"}, http.StatusBadGateway, false
	}
	store.serve("k", time.Minute, false, failing)
	store.serve("k", time.Minute, false, failing)
	if calls != 2 {
		t.Fatalf("error response was cached: calls = %d", calls)
	}
}

func TestCacheRefreshBypassesCachedEntry(t *testing.T) {
	store, _ := newTestStore()
	var calls int32
	fetch := fetchOnce(&calls, map[string]any{"success": true})

	store.serve("k", time.Minute, false, fetch)
	refreshed := store.serve("k", time.Minute, true, fetch)
	if calls != 2 {
		t.Fatalf("refresh did not hit upstream: calls = %d", calls)
	}
	if cacheFlag(t, refreshed) {
		t.Fatal("refresh response must be marked cached:false")
	}
}

func TestFlightGroupCoalescesConcurrentFetches(t *testing.T) {
	group := newFlightGroup()
	var calls int32
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	fetch := func() (map[string]any, int, bool) {
		atomic.AddInt32(&calls, 1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return map[string]any{"success": true}, http.StatusOK, true
	}

	const callers = 16
	var wg sync.WaitGroup
	results := make([]*flightResult, callers)
	wg.Add(callers)
	for index := 0; index < callers; index++ {
		go func(slot int) {
			defer wg.Done()
			results[slot], _ = group.do("same-key", fetch)
		}(index)
	}
	<-started
	// Give every other caller time to join the in-flight call before it ends;
	// the leader is blocked on release, so they can only wait, not re-fetch.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1 (coalesced)", calls)
	}
	for index, result := range results {
		if result == nil || result.payload["success"] != true {
			t.Fatalf("caller %d did not receive the shared result: %#v", index, result)
		}
	}
}

func TestFlightGroupRunsAgainAfterCompletion(t *testing.T) {
	group := newFlightGroup()
	var calls int32
	fetch := fetchOnce(&calls, map[string]any{"success": true})
	group.do("k", fetch)
	group.do("k", fetch)
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestCacheKeyScopesCredentialsAndQuery(t *testing.T) {
	cfg := defaultConfig()
	clientA := accountClient{cfg: cfg, token: "token-a", userID: "1"}
	clientB := accountClient{cfg: cfg, token: "token-b", userID: "1"}
	clientC := accountClient{cfg: cfg, token: "token-a", userID: "2"}
	query := url.Values{"days": {"7"}}

	keyA := managementCacheKey("dashboard", cfg, clientA, query)
	if keyA != managementCacheKey("dashboard", cfg, clientA, query) {
		t.Fatal("cache key must be stable")
	}
	if keyA == managementCacheKey("dashboard", cfg, clientB, query) {
		t.Fatal("token change must change the key")
	}
	if keyA == managementCacheKey("dashboard", cfg, clientC, query) {
		t.Fatal("user id change must change the key")
	}
	if keyA == managementCacheKey("dashboard", cfg, clientA, url.Values{"days": {"30"}}) {
		t.Fatal("query change must change the key")
	}
	if keyA == managementCacheKey("logs", cfg, clientA, query) {
		t.Fatal("dataset change must change the key")
	}
	withRefresh := url.Values{"days": {"7"}, "refresh": {"1"}}
	if keyA != managementCacheKey("dashboard", cfg, clientA, withRefresh) {
		t.Fatal("refresh flag must not change the cache key")
	}
	if keyA == "" || strings.Contains(keyA, "token-a") {
		t.Fatalf("cache key must not embed the raw secret: %q", keyA)
	}
}

func TestLogsCacheQueryBucketsTimestamps(t *testing.T) {
	query := url.Values{
		"page":    {"0"},
		"start":   {"1700000001"},
		"end":     {"1700000099"},
		"refresh": {"1"},
	}
	normalized := logsCacheQuery(query)
	if normalized.Get("start") != "1699999980" || normalized.Get("end") != "1700000070" {
		t.Fatalf("timestamps not bucketed: %v", normalized)
	}
	if normalized.Get("refresh") != "" {
		t.Fatal("refresh flag must be stripped from the cache key")
	}
	if normalized.Get("page") != "0" {
		t.Fatalf("page lost: %v", normalized)
	}
}

func TestRefreshRequested(t *testing.T) {
	cases := map[string]bool{"1": true, "true": true, "yes": true, "": false, "0": false}
	for value, want := range cases {
		req := pluginapi.ManagementRequest{Query: url.Values{"refresh": {value}}}
		if got := refreshRequested(req); got != want {
			t.Fatalf("refreshRequested(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestWithCachedFlagDoesNotMutateSource(t *testing.T) {
	source := map[string]any{"success": true}
	out := withCachedFlag(source, true)
	if _, ok := source["cached"]; ok {
		t.Fatal("source payload was mutated")
	}
	if cached, ok := out["cached"].(bool); !ok || !cached {
		t.Fatalf("cached flag = %#v", out["cached"])
	}
}

func cacheFlag(t *testing.T, response pluginapi.ManagementResponse) bool {
	t.Helper()
	var payload map[string]any
	if errUnmarshal := json.Unmarshal(response.Body, &payload); errUnmarshal != nil {
		t.Fatalf("decode response: %v", errUnmarshal)
	}
	flag, _ := payload["cached"].(bool)
	return flag
}

func payloadField(response pluginapi.ManagementResponse) any {
	var payload map[string]any
	if errUnmarshal := json.Unmarshal(response.Body, &payload); errUnmarshal != nil {
		return nil
	}
	return payload["value"]
}

// TestManagementStoreServesCachedFlag covers the UI contract: repeat reads are
// served from cache with cached=true, and refresh=1 bypasses the cache.
func TestManagementStoreServesCachedFlag(t *testing.T) {
	store := newResponseStore()
	var fetches int
	fetch := func() (map[string]any, int, bool) {
		fetches++
		return map[string]any{"success": true, "value": fetches}, 200, true
	}
	first := store.serve("k", 30*time.Second, false, fetch)
	second := store.serve("k", 30*time.Second, false, fetch)
	third := store.serve("k", 30*time.Second, true, fetch)
	if fetches != 2 {
		t.Fatalf("fetches = %d, want 2 (one miss, one refresh)", fetches)
	}
	if !bodyHasCachedFlag(first.Body, false) || !bodyHasCachedFlag(second.Body, true) || !bodyHasCachedFlag(third.Body, false) {
		t.Fatalf("cached flags = %s / %s / %s", first.Body, second.Body, third.Body)
	}
}

func bodyHasCachedFlag(body []byte, want bool) bool {
	wantText := `"cached":false`
	if want {
		wantText = `"cached":true`
	}
	return strings.Contains(string(body), wantText)
}
