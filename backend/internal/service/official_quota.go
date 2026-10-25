package service

import (
	"net/http"
	"sync"
	"time"
)

// officialWindowResetGap 是同一官方窗口内 reset_at 的抖动上限。
// 响应头里的 reset_at 由「现在 + 剩余秒数」算出，同一窗口会有秒级漂移。
// 超过这个间隔才当成官方窗口被重置（周切期或重置卡把窗口往前拉）。
const officialWindowResetGap = 5 * time.Minute

// officialWindowTracker 记住每个订阅账号上次写进用量行的官方重置时间。
// 进程重启后用账号上已保存的重置时间做第一次比较，避免把重启误记成重置。
type officialWindowTracker struct {
	mu   sync.Mutex
	last map[int64]time.Time
}

func newOfficialWindowTracker() *officialWindowTracker {
	return &officialWindowTracker{last: map[int64]time.Time{}}
}

func (s *OpenAIGatewayService) officialWindowTracker() *officialWindowTracker {
	if s == nil {
		return nil
	}
	s.officialWindowsOnce.Do(func() {
		s.officialWindows = newOfficialWindowTracker()
	})
	return s.officialWindows
}

// canonical 返回要写进用量行的窗口结束时间，以及这次读数是否换了窗口。
// 同一窗口内「现在 + 剩余秒数」会秒级漂移，行上保留第一次见到的结束时间，
// 这样多周之后仍能按订阅账号和这一个时间把记录归到同一周。
func (t *officialWindowTracker) canonical(accountID int64, reset time.Time, stored *time.Time) (time.Time, bool) {
	reset = reset.UTC()
	if t == nil || accountID <= 0 || reset.IsZero() {
		if stored != nil && !officialResetMoved(*stored, reset) {
			return stored.UTC(), false
		}
		return reset, stored != nil && officialResetMoved(*stored, reset)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		t.last = map[int64]time.Time{}
	}
	if prev, ok := t.last[accountID]; ok {
		if officialResetMoved(prev, reset) {
			t.last[accountID] = reset
			return reset, true
		}
		return prev, false
	}
	if stored != nil && !officialResetMoved(*stored, reset) {
		stable := stored.UTC()
		t.last[accountID] = stable
		return stable, false
	}
	t.last[accountID] = reset
	return reset, stored != nil && officialResetMoved(*stored, reset)
}

func officialResetMoved(a, b time.Time) bool {
	delta := a.Sub(b)
	if delta < 0 {
		delta = -delta
	}
	return delta > officialWindowResetGap
}

// SharedOfficialReset is the reset ordinary users can follow. It is set only
// when every readable subscription account of that purpose ends together.
// Header drift of a few minutes is still one reset day. A wider spread is not
// a personal timer and must not replace the person's own 7-day window.
func SharedOfficialReset(resets []time.Time) (time.Time, bool) {
	var earliest, latest time.Time
	count := 0
	for _, reset := range resets {
		if reset.IsZero() {
			continue
		}
		reset = reset.UTC()
		if count == 0 || reset.Before(earliest) {
			earliest = reset
		}
		if count == 0 || reset.After(latest) {
			latest = reset
		}
		count++
	}
	if count == 0 || officialResetMoved(earliest, latest) {
		return time.Time{}, false
	}
	return earliest, true
}

// StudentCodingReset is the countdown on an ordinary Codex or Grok card.
// A shared official reset is a collective refresh: after it, that subscription
// window is new and the person can use again. Differing account clocks keep
// the personal window, so routing does not move the countdown.
func StudentCodingReset(personal time.Time, official []time.Time) time.Time {
	if shared, ok := SharedOfficialReset(official); ok {
		return shared
	}
	return personal
}

// stampOfficialQuota 把这一笔网关计费发生时、该订阅账号的官方额度写到用量行上。
// 影子账号的实扣仍记在用量行的 account_id，官方额度记母账号。
// 没有新鲜读数时沿用账号上尚未过期的快照，并保留快照自己的观察时间。
func stampOfficialQuota(log *UsageLog, serving, credential *Account, headers http.Header, now time.Time, tracker *officialWindowTracker) {
	if log == nil {
		return
	}
	meter := officialMeterAccount(serving, credential)
	if meter == nil {
		return
	}
	switch meter.Platform {
	case PlatformOpenAI:
		stampOpenAIOfficial(log, meter, headers, now, tracker)
	case PlatformGrok:
		stampGrokOfficial(log, meter, now, tracker)
	}
}

func officialMeterAccount(serving, credential *Account) *Account {
	if serving != nil && serving.IsShadow() {
		if credential == nil || credential.IsShadow() {
			return nil
		}
		return credential
	}
	if serving != nil {
		return serving
	}
	return credential
}

func stampOpenAIOfficial(log *UsageLog, meter *Account, headers http.Header, now time.Time, tracker *officialWindowTracker) {
	id := meter.ID
	log.OfficialAccountID = &id
	used, reset, observed, source := readCodexOfficial(meter.Extra, headers, now)
	if source == "" {
		source = "none"
	}
	log.OfficialSource = &source
	if used == nil {
		return
	}
	applyOfficialPercents(log, *used)
	if observed != nil {
		log.OfficialObservedAt = observed
	}
	if reset == nil {
		return
	}
	stable, changed := tracker.canonical(id, *reset, extraTime(meter.Extra, "codex_7d_reset_at"))
	log.OfficialResetAt = &stable
	log.OfficialWindowReset = &changed
}

func stampGrokOfficial(log *UsageLog, meter *Account, now time.Time, tracker *officialWindowTracker) {
	id := meter.ID
	log.OfficialAccountID = &id
	used, reset, observed, source, _ := grokOfficial(meter.Extra, now)
	if source == "" {
		source = "grok_billing_no_weekly_percent"
	}
	log.OfficialSource = &source
	if used == nil {
		return
	}
	applyOfficialPercents(log, *used)
	if observed != nil {
		log.OfficialObservedAt = observed
	}
	if reset == nil {
		return
	}
	stable, changed := tracker.canonical(id, *reset, reset)
	log.OfficialResetAt = &stable
	log.OfficialWindowReset = &changed
}

func readCodexOfficial(extra map[string]any, headers http.Header, now time.Time) (*float64, *time.Time, *time.Time, string) {
	if headers != nil {
		if snapshot := ParseCodexRateLimitHeaders(headers); snapshot != nil {
			if normalized := snapshot.Normalize(); normalized != nil && normalized.Used7dPercent != nil {
				var reset *time.Time
				if text := codexResetAtRFC3339(now, normalized.Reset7dSeconds); text != nil {
					if parsed, err := time.Parse(time.RFC3339, *text); err == nil {
						reset = &parsed
					}
				}
				observed := now
				return normalized.Used7dPercent, reset, &observed, "codex_7d"
			}
		}
	}
	return codexOfficial(extra, now)
}

func applyOfficialPercents(log *UsageLog, used float64) {
	rounded := round4(used)
	log.OfficialUsedPercent = &rounded
	if rounded < 0 {
		return
	}
	remaining := round4(100 - rounded)
	if remaining < 0 {
		remaining = 0
	}
	log.OfficialRemainingPercent = &remaining
}

func extraTime(extra map[string]any, key string) *time.Time {
	if extra == nil {
		return nil
	}
	text, ok := extra[key].(string)
	if !ok || text == "" {
		return nil
	}
	parsed, err := parseTime(text)
	if err != nil {
		return nil
	}
	return &parsed
}
