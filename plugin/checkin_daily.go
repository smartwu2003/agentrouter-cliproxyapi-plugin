package main

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// dailyCheckin is today's check-in evidence. AgentRouter redeems the daily
// reward through the console login flow, so the only authoritative record of
// an *external* check-in is the "签到" entry of the usage log (type 4). The
// generic New API /api/user/self/checkin endpoint is not implemented upstream
// (it answers 404), and last_login_time must never be treated as proof: a
// dashboard read is not a login.
type dailyCheckin struct {
	CheckedIn   bool    `json:"checked_in"`
	Known       bool    `json:"checked_in_known"`
	CheckedInAt string  `json:"checked_in_at,omitempty"`
	Date        string  `json:"checked_in_date,omitempty"`
	RewardUSD   float64 `json:"reward_usd,omitempty"`
	Source      string  `json:"checkin_source,omitempty"`
	Message     string  `json:"-"`
}

// dailyCheckinMemory caches the resolved status so the account card and the
// check-in panel never query the log twice in one page load. The key contains
// the Beijing date, so a midnight rollover always starts from a new entry.
type dailyCheckinMemoryState struct {
	mu    sync.Mutex
	key   string
	value dailyCheckin
	at    time.Time
}

var (
	dailyCheckinStore   = &dailyCheckinMemoryState{}
	dailyCheckinFetchMu sync.Mutex
)

const dailyCheckinTTL = 45 * time.Second

// beijingToday returns the current date in the account's home timezone. The
// console issues the reward on Beijing time; using the host zone would show
// yesterday's status for up to eight hours after midnight.
func beijingToday(now time.Time) string {
	return now.In(beijingLocation()).Format("2006-01-02")
}

func beijingNowText() string {
	return time.Now().In(beijingLocation()).Format("2006-01-02 15:04:05")
}

// beijingDayWindow returns [start, end) of the current Beijing day.
func beijingDayWindow(now time.Time) (time.Time, time.Time) {
	local := now.In(beijingLocation())
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, beijingLocation())
	return start, start.AddDate(0, 0, 1)
}

var checkinRewardPattern = regexp.MustCompile(`[＄$]\s*([0-9]+(?:\.[0-9]+)?)`)

// checkinRewardUSD extracts the awarded amount from a reward log line such as
// "每日签到成功，增加额度 ＄25.000000 额度". The structured quota field of a
// check-in log row is zero, so the text is the only source.
func checkinRewardUSD(content string) float64 {
	match := checkinRewardPattern.FindStringSubmatch(content)
	if len(match) < 2 {
		return 0
	}
	value, errParse := strconv.ParseFloat(match[1], 64)
	if errParse != nil {
		return 0
	}
	return value
}

// fetchDailyCheckin reads today's check-in log entry from the console.
func fetchDailyCheckin(ctx context.Context, client accountClient) (dailyCheckin, error) {
	now := time.Now()
	day := beijingToday(now)
	start, end := beijingDayWindow(now)
	page, errLogs := client.fetchLogs(ctx, 4, 0, 20, "", start.Unix(), end.Unix()-1)
	if errLogs != nil {
		return dailyCheckin{Date: day}, errLogs
	}
	for _, item := range page.Items {
		if item.Type != 4 {
			continue
		}
		if item.CreatedAt < start.Unix() || item.CreatedAt >= end.Unix() {
			continue
		}
		return dailyCheckin{
			CheckedIn:   true,
			Known:       true,
			Date:        day,
			CheckedInAt: time.Unix(item.CreatedAt, 0).In(beijingLocation()).Format("2006-01-02 15:04:05"),
			RewardUSD:   checkinRewardUSD(item.Content),
			Source:      "log",
		}, nil
	}
	return dailyCheckin{CheckedIn: false, Known: true, Date: day, Source: "log"}, nil
}

func (a *storedAccount) checkedInOn(day string) bool {
	return a != nil && a.CheckedIn && strings.TrimSpace(a.CheckedInDate) == strings.TrimSpace(day)
}

// persistedDailyCheckin returns the last known status only when it belongs to
// the given day. A check-in from yesterday must never render as today's.
func persistedDailyCheckin(day string) (dailyCheckin, bool) {
	state := cachedState()
	if !state.Account.checkedInOn(day) {
		return dailyCheckin{Date: day}, false
	}
	return dailyCheckin{
		CheckedIn:   true,
		Known:       true,
		Date:        day,
		CheckedInAt: state.Account.CheckedInAt,
		Source:      "state",
	}, true
}

func dailyCheckinCached(key string, force bool) (dailyCheckin, bool) {
	if force {
		return dailyCheckin{}, false
	}
	dailyCheckinStore.mu.Lock()
	defer dailyCheckinStore.mu.Unlock()
	if dailyCheckinStore.key != key || time.Since(dailyCheckinStore.at) >= dailyCheckinTTL {
		return dailyCheckin{}, false
	}
	return dailyCheckinStore.value, true
}

func storeDailyCheckin(key string, value dailyCheckin) {
	dailyCheckinStore.mu.Lock()
	dailyCheckinStore.key = key
	dailyCheckinStore.value = value
	dailyCheckinStore.at = time.Now()
	dailyCheckinStore.mu.Unlock()
}

// invalidateDailyCheckin forces the next read to hit the console. It is called
// after an explicit check-in and after credentials change.
func invalidateDailyCheckin() {
	dailyCheckinStore.mu.Lock()
	dailyCheckinStore.key = ""
	dailyCheckinStore.value = dailyCheckin{}
	dailyCheckinStore.at = time.Time{}
	dailyCheckinStore.mu.Unlock()
}

// persistDailyCheckin records a confirmed status so the card stays truthful
// while the console is unreachable (WAF, proxy down) later in the day.
func persistDailyCheckin(cfg pluginConfig, value dailyCheckin) {
	if !value.Known || value.Date == "" {
		return
	}
	// The status is re-read every cache TTL; only touch the state file when the
	// persisted record would actually change.
	current := cachedState().Account
	if value.CheckedIn {
		if current.checkedInOn(value.Date) && current.CheckedInAt == value.CheckedInAt {
			return
		}
	} else if current == nil || current.CheckedInDate != value.Date {
		return
	}
	_, _ = updateState(cfg, func(state *pluginState) {
		if state.Account == nil {
			state.Account = &storedAccount{}
		}
		if value.CheckedIn {
			state.Account.CheckedIn = true
			state.Account.CheckedInAt = value.CheckedInAt
			state.Account.CheckedInDate = value.Date
			return
		}
		// Only a read for the current day may clear the flag; a stale day is
		// cleared separately when it is displayed.
		if state.Account.CheckedInDate == value.Date {
			state.Account.CheckedIn = false
			state.Account.CheckedInAt = ""
			state.Account.CheckedInDate = ""
		}
	})
}

// resolveDailyCheckin returns today's status, preferring the live log and
// falling back to a same-day persisted value when the console is unreachable.
func resolveDailyCheckin(ctx context.Context, cfg pluginConfig, client accountClient, force bool) dailyCheckin {
	day := beijingToday(time.Now())
	key := strings.Join([]string{
		strings.TrimSpace(client.userID),
		day,
		secretFingerprint(client.token),
		secretFingerprint(client.session),
	}, "|")
	if value, ok := dailyCheckinCached(key, force); ok {
		return value
	}
	dailyCheckinFetchMu.Lock()
	defer dailyCheckinFetchMu.Unlock()
	if value, ok := dailyCheckinCached(key, force); ok {
		return value
	}
	value, hasPersisted := persistedDailyCheckin(day)
	if !hasPersisted {
		value = dailyCheckin{Date: day}
	}
	value.CheckedInAt = firstNonEmptyString(value.CheckedInAt, "")
	if client.hasCredentials() {
		requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		live, errLive := fetchDailyCheckin(requestCtx, client)
		cancel()
		if errLive == nil {
			value = live
			persistDailyCheckin(cfg, live)
		} else {
			value.Message = errLive.Error()
			if !value.CheckedIn {
				value.Known = false
			}
		}
	} else if !value.CheckedIn {
		value.Known = false
	}
	storeDailyCheckin(key, value)
	return value
}

// applyDailyCheckin copies a resolved status onto the account card.
func applyDailyCheckin(info *accountInfo, status dailyCheckin) {
	info.CheckedIn = status.CheckedIn
	info.CheckedInAt = status.CheckedInAt
	info.CheckedInDate = status.Date
	info.CheckedInKnown = status.Known
	info.CheckinSource = status.Source
}

// applyPersistedDailyCheckin is used when the console cannot be read at all.
func applyPersistedDailyCheckin(info *accountInfo) {
	day := beijingToday(time.Now())
	status, ok := persistedDailyCheckin(day)
	if !ok {
		status = dailyCheckin{Date: day, Known: false}
	}
	applyDailyCheckin(info, status)
}
