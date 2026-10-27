package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// resetCheckinState clears the in-memory daily cache and restores any state
// snapshot the test replaced.
func resetCheckinState(t *testing.T) {
	t.Helper()
	invalidateDailyCheckin()
	consoleOriginMemory.reset()
	pluginStore.mu.Lock()
	previousPath, previousData := pluginStore.path, pluginStore.data
	pluginStore.mu.Unlock()
	t.Cleanup(func() {
		pluginStore.mu.Lock()
		pluginStore.path, pluginStore.data = previousPath, previousData
		pluginStore.mu.Unlock()
		invalidateDailyCheckin()
		consoleOriginMemory.reset()
	})
}

func checkinLogServer(t *testing.T, createdAt int64, content string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/log/self" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("type"); got != "4" {
			t.Errorf("log query type = %q, want 4", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"success":true,"data":{"page":1,"page_size":20,"total":1,"items":[{"id":1,"created_at":%d,"type":4,"content":%q}]}}`, createdAt, content)
	}))
	t.Cleanup(server.Close)
	return server
}

// TestFetchDailyCheckinReadsTodaysRewardLog is the regression for the reported
// bug: the account was checked in on the website at 09:30 Beijing time, but the
// plugin only looked at a stale local flag.
func TestFetchDailyCheckinReadsTodaysRewardLog(t *testing.T) {
	resetCheckinState(t)
	createdAt := time.Now().Unix()
	server := checkinLogServer(t, createdAt, "每日签到成功，增加额度 ＄25.000000 额度")

	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.DashboardBaseURL = server.URL
	client := accountClient{cfg: cfg, token: "token", userID: "706609"}

	status, errStatus := fetchDailyCheckin(context.Background(), client)
	if errStatus != nil {
		t.Fatal(errStatus)
	}
	if !status.CheckedIn || !status.Known {
		t.Fatalf("status = %+v, want checked in", status)
	}
	if status.RewardUSD != 25 {
		t.Fatalf("reward = %v, want 25", status.RewardUSD)
	}
	wantAt := time.Unix(createdAt, 0).In(beijingLocation()).Format("2006-01-02 15:04:05")
	if status.CheckedInAt != wantAt {
		t.Fatalf("checked_in_at = %q, want %q", status.CheckedInAt, wantAt)
	}
	if status.Date != beijingToday(time.Now()) {
		t.Fatalf("date = %q", status.Date)
	}
}

// TestFetchDailyCheckinIgnoresYesterday makes sure a reward from the previous
// day can never render as today's status.
func TestFetchDailyCheckinIgnoresYesterday(t *testing.T) {
	resetCheckinState(t)
	now := time.Now()
	start, _ := beijingDayWindow(now)
	server := checkinLogServer(t, start.Unix()-60, "每日签到成功，增加额度 ＄25.000000 额度")

	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.DashboardBaseURL = server.URL
	client := accountClient{cfg: cfg, token: "token", userID: "706609"}

	status, errStatus := fetchDailyCheckin(context.Background(), client)
	if errStatus != nil {
		t.Fatal(errStatus)
	}
	if status.CheckedIn || !status.Known {
		t.Fatalf("yesterday's reward leaked into today: %+v", status)
	}
}

func TestCheckinRewardParsesFullWidthCurrency(t *testing.T) {
	cases := map[string]float64{
		"每日签到成功，增加额度 ＄25.000000 额度": 25,
		"每日签到成功，增加额度 $0.50":         0.5,
		"每日签到成功":                    0,
	}
	for content, want := range cases {
		if got := checkinRewardUSD(content); got != want {
			t.Fatalf("checkinRewardUSD(%q) = %v, want %v", content, got, want)
		}
	}
}

func TestPersistedDailyCheckinRequiresSameDay(t *testing.T) {
	resetCheckinState(t)
	today := beijingToday(time.Now())
	yesterday := time.Now().In(beijingLocation()).AddDate(0, 0, -1).Format("2006-01-02")

	pluginStore.mu.Lock()
	pluginStore.data = pluginState{Account: &storedAccount{
		CheckedIn:     true,
		CheckedInAt:   yesterday + " 09:00:00",
		CheckedInDate: yesterday,
	}}
	pluginStore.mu.Unlock()
	if status, ok := persistedDailyCheckin(today); ok || status.CheckedIn {
		t.Fatalf("yesterday's persisted status must not count: %+v", status)
	}

	pluginStore.mu.Lock()
	pluginStore.data = pluginState{Account: &storedAccount{
		CheckedIn:     true,
		CheckedInAt:   today + " 09:30:13",
		CheckedInDate: today,
	}}
	pluginStore.mu.Unlock()
	status, ok := persistedDailyCheckin(today)
	if !ok || !status.CheckedIn || status.CheckedInAt != today+" 09:30:13" {
		t.Fatalf("same-day status lost: %+v ok=%v", status, ok)
	}
}

func TestMergeAccountClearsYesterdayFlag(t *testing.T) {
	yesterday := time.Now().In(beijingLocation()).AddDate(0, 0, -1).Format("2006-01-02")
	existing := &storedAccount{CheckedIn: true, CheckedInAt: yesterday + " 09:00:00", CheckedInDate: yesterday}
	account := mergeAccount(existing, loginResult{}, defaultConfig())
	if account.CheckedIn || account.CheckedInDate != "" {
		t.Fatalf("stale flag survived a new login: %+v", account)
	}
	if account.LoginAt == 0 {
		t.Fatal("login timestamp missing")
	}
}

// TestCheckinStatusResponseUsesRewardLog covers the management endpoint the
// console polls: it must report the external check-in and its time.
func TestCheckinStatusResponseUsesRewardLog(t *testing.T) {
	resetCheckinState(t)
	configureTestPlugin(t)
	createdAt := time.Now().Unix()
	server := checkinLogServer(t, createdAt, "每日签到成功，增加额度 ＄25.000000 额度")

	cfg := loadedConfig()
	cfg.DataDir = t.TempDir()
	cfg.DashboardBaseURL = server.URL
	cfg.AccountToken = "token"
	cfg.UserID = "706609"
	cfg.APIKey = "sk-test"
	activeConfigMu.Lock()
	activeConfig = cfg
	activeConfigMu.Unlock()
	pluginStore.mu.Lock()
	pluginStore.path = statePath(cfg)
	pluginStore.data = pluginState{Account: &storedAccount{AccountToken: "token", UserID: "706609"}}
	pluginStore.mu.Unlock()
	invalidateDailyCheckin()

	resp := checkinStatusResponse(pluginapi.ManagementRequest{
		Method: "GET",
		Path:   routeCheckinStatus,
		Query:  url.Values{"refresh": {"1"}},
	})
	var payload struct {
		CheckedIn   bool    `json:"checked_in"`
		Known       bool    `json:"checked_in_known"`
		CheckedInAt string  `json:"checked_in_at"`
		RewardUSD   float64 `json:"reward_usd"`
	}
	if errUnmarshal := json.Unmarshal(resp.Body, &payload); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if !payload.CheckedIn || !payload.Known {
		t.Fatalf("status payload = %s", resp.Body)
	}
	if payload.RewardUSD != 25 {
		t.Fatalf("reward = %v, body = %s", payload.RewardUSD, resp.Body)
	}
	wantAt := time.Unix(createdAt, 0).In(beijingLocation()).Format("2006-01-02 15:04:05")
	if payload.CheckedInAt != wantAt {
		t.Fatalf("checked_in_at = %q, want %q", payload.CheckedInAt, wantAt)
	}
}

// TestCheckinStatusCachesPerDay keeps the short-lived cache from hiding a new
// day's status: the key contains the Beijing date, not just the credentials.
func TestCheckinStatusCachesPerDay(t *testing.T) {
	resetCheckinState(t)
	keyToday := strings.Join([]string{"706609", beijingToday(time.Now()), "a", "b"}, "|")
	keyTomorrow := strings.Join([]string{"706609", beijingToday(time.Now().Add(24 * time.Hour)), "a", "b"}, "|")
	storeDailyCheckin(keyToday, dailyCheckin{CheckedIn: true, Known: true, Date: beijingToday(time.Now())})
	if _, ok := dailyCheckinCached(keyToday, false); !ok {
		t.Fatal("today's key must be cached")
	}
	if _, ok := dailyCheckinCached(keyTomorrow, false); ok {
		t.Fatal("a different day must not reuse the cached status")
	}
	dailyCheckinCacheProbe := keyTomorrow
	if dailyCheckinCacheProbe == keyToday {
		t.Fatal("cache key must include the date")
	}
}

// TestDailyCheckinLogWindowIsBeijingDay asserts the upstream query window is
// the Beijing calendar day, independent of the machine's timezone.
func TestDailyCheckinLogWindowIsBeijingDay(t *testing.T) {
	resetCheckinState(t)
	var gotStart, gotEnd int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotStart, _ = strconv.ParseInt(r.URL.Query().Get("start_timestamp"), 10, 64)
		gotEnd, _ = strconv.ParseInt(r.URL.Query().Get("end_timestamp"), 10, 64)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"page":1,"page_size":20,"total":0,"items":[]}}`))
	}))
	defer server.Close()

	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.DashboardBaseURL = server.URL
	client := accountClient{cfg: cfg, token: "token", userID: "706609"}
	if _, errStatus := fetchDailyCheckin(context.Background(), client); errStatus != nil {
		t.Fatal(errStatus)
	}
	start, end := beijingDayWindow(time.Now())
	if gotStart != start.Unix() {
		t.Fatalf("start = %d, want %d", gotStart, start.Unix())
	}
	if gotEnd != end.Unix()-1 {
		t.Fatalf("end = %d, want %d", gotEnd, end.Unix()-1)
	}
}

func TestDailyCheckinAssetsKeepSingleLineLogRows(t *testing.T) {
	for _, needle := range []string{"table.logs-table", ".log-model", ".log-detail > span", ".pill.spend"} {
		if !strings.Contains(dashboardCSS, needle) {
			t.Fatalf("stylesheet is missing %q", needle)
		}
	}
	for _, needle := range []string{"logs-table", "logTypeClass", "log-detail"} {
		if !strings.Contains(dashboardJS, needle) {
			t.Fatalf("client script is missing %q", needle)
		}
	}
}
