// Package quotaudit plans conservative, evidence-backed historical refunds.
// It never modifies raw usage, personal period anchors or reset events.
package quotaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// Observation is an immutable upstream cumulative reading, not a request charge.
type Observation struct {
	ID        int64     `json:"id"`
	AccountID int64     `json:"account_id"`
	Purpose   string    `json:"purpose"`
	At        time.Time `json:"observed_at"`
	End       time.Time `json:"window_end"`
	Used      string    `json:"used"`
	Source    string    `json:"source"`
	Processed bool      `json:"processed"`
}

// Request retains the original final charge for optimistic concurrency checks.
type Request struct {
	ID          int64      `json:"id"`
	AccountID   int64      `json:"account_id"`
	UserID      int64      `json:"user_id"`
	Purpose     string     `json:"purpose"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt time.Time  `json:"completed_at"`
	End         *time.Time `json:"official_window_end"`
	Weight      string     `json:"weight"`
	Points      string     `json:"points"`
	State       string     `json:"state"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Reset excludes confirmed platform transitions from same-window refunds.
type Reset struct {
	EffectiveAt time.Time `json:"effective_at"`
	Status      string    `json:"status"`
}

// Snapshot is a bounded, consistent, credential-free source for offline review.
type Snapshot struct {
	AccountID    int64         `json:"account_id"`
	From         time.Time     `json:"from"`
	To           time.Time     `json:"to"`
	Observations []Observation `json:"observations"`
	Requests     []Request     `json:"requests"`
	Resets       []Reset       `json:"events"`
}

// Entry appends a credit to a proven duplicated charge without overwriting it.
type Entry struct {
	RequestID int64   `json:"request_row_id"`
	UserID    int64   `json:"user_id"`
	Purpose   string  `json:"purpose"`
	Before    string  `json:"before_points"`
	Delta     string  `json:"delta"`
	Original  Request `json:"original_request"`
}

// Episode preserves the actual three readings and exact affected request rows.
type Episode struct {
	ID       string      `json:"id"`
	High     Observation `json:"high"`
	Drop     Observation `json:"drop"`
	Recovery Observation `json:"recovery"`
	Credit   string      `json:"credit"`
	Entries  []Entry     `json:"entries"`
}

// Finding records ambiguity which must never be silently converted to a refund.
type Finding struct {
	ObservationID int64  `json:"observation_id"`
	Reason        string `json:"reason"`
}

// Plan is explicitly reviewed and pinned by Digest before any database writes.
type Plan struct {
	Algorithm string    `json:"algorithm"`
	AccountID int64     `json:"account_id"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	Episodes  []Episode `json:"episodes"`
	Review    []Finding `json:"review"`
}

// Build prepares only refunds supported by a complete original settlement batch.
func Build(s Snapshot) (Plan, error) {
	p := Plan{Algorithm: "same-window-recovery-v1", AccountID: s.AccountID, From: s.From.UTC(), To: s.To.UTC(), Episodes: []Episode{}, Review: []Finding{}}
	if s.AccountID <= 0 || s.From.IsZero() || !s.To.After(s.From) || s.To.Sub(s.From) > 31*24*time.Hour {
		return p, fmt.Errorf("audit requires one account and a bounded interval of at most 31 days")
	}
	observations := append([]Observation(nil), s.Observations...)
	sort.Slice(observations, func(i, j int) bool {
		if observations[i].At.Equal(observations[j].At) {
			return observations[i].ID < observations[j].ID
		}
		return observations[i].At.Before(observations[j].At)
	})
	conflicts := map[string]bool{}
	seen := map[string]string{}
	for _, o := range observations {
		if o.AccountID != s.AccountID || !o.Processed {
			continue
		}
		value, err := decimal.NewFromString(o.Used)
		if err != nil || value.IsNegative() || value.GreaterThan(decimal.NewFromInt(100)) {
			return p, fmt.Errorf("invalid observation %d", o.ID)
		}
		key := o.At.UTC().Format(time.RFC3339Nano)
		if previous, ok := seen[key]; ok && previous != value.String() {
			conflicts[key] = true
		}
		seen[key] = value.String()
	}
	var high, cursor Observation
	for _, o := range observations {
		if o.AccountID != s.AccountID || !o.Processed || o.At.Before(s.From) || o.At.After(s.To) {
			continue
		}
		o.At, o.End = o.At.UTC(), o.End.UTC()
		if high.ID == 0 || moved(cursor.End, o.End) || cursor.Purpose != o.Purpose || o.Source == "codex_local_reset" {
			high, cursor = o, o
			continue
		}
		if !o.At.After(cursor.At) {
			continue
		}
		value, _ := decimal.NewFromString(o.Used)
		previous, _ := decimal.NewFromString(cursor.Used)
		maximum, _ := decimal.NewFromString(high.Used)
		if value.LessThan(previous) {
			cursor = o
			continue
		}
		if value.Equal(previous) {
			continue
		}
		if previous.LessThan(maximum) {
			reason := ""
			if value.GreaterThan(maximum) {
				reason = "recovery also contains a true increment; manual review required"
			}
			if moved(high.End, o.End) {
				reason = "window drift is ambiguous"
			}
			for _, r := range s.Resets {
				if o.Purpose == "openai" && r.Status == "confirmed" && r.EffectiveAt.After(high.At) && !r.EffectiveAt.After(o.At) {
					reason = "confirmed reset overlaps the interval"
				}
			}
			for at := range conflicts {
				t, _ := time.Parse(time.RFC3339Nano, at)
				if !t.Before(high.At) && !t.After(o.At) {
					reason = "conflicting same-second official readings"
				}
			}
			var episode Episode
			if reason == "" {
				episode, reason = recoveryEpisode(s, high, cursor, o, value.Sub(previous))
			}
			if reason != "" {
				p.Review = append(p.Review, Finding{ObservationID: o.ID, Reason: reason})
			} else {
				p.Episodes = append(p.Episodes, episode)
			}
		}
		cursor = o
		if value.GreaterThan(maximum) {
			high = o
		}
	}
	return p, nil
}

func moved(a, b time.Time) bool { d := a.Sub(b); return d > 5*time.Minute || d < -5*time.Minute }

func recoveryEpisode(s Snapshot, high, drop, recovery Observation, increment decimal.Decimal) (Episode, string) {
	e := Episode{High: high, Drop: drop, Recovery: recovery, Credit: increment.StringFixed(8), Entries: []Entry{}}
	var requests []Request
	for _, r := range s.Requests {
		if r.AccountID == s.AccountID && r.State == "settled" && r.CompletedAt.After(drop.At) && !r.CompletedAt.After(recovery.At) && r.End != nil && !moved(*r.End, recovery.End) {
			requests = append(requests, r)
		}
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].ID < requests[j].ID })
	if len(requests) == 0 {
		return e, "no complete original settlement batch"
	}
	total, weight := decimal.Zero, decimal.Zero
	for _, r := range requests {
		points, err := decimal.NewFromString(r.Points)
		if err != nil || points.IsNegative() {
			return e, "invalid request points"
		}
		w, err := decimal.NewFromString(r.Weight)
		if err != nil || !w.IsPositive() {
			return e, "invalid request weight"
		}
		if !r.UpdatedAt.Equal(requests[0].UpdatedAt) || r.UpdatedAt.Before(recovery.At) {
			return e, "requests do not belong to one original settlement batch"
		}
		total, weight = total.Add(points), weight.Add(w)
	}
	if !total.Equal(increment) {
		return e, "original charges do not conserve the recovered increment"
	}
	remaining := increment
	for i, r := range requests {
		w, _ := decimal.NewFromString(r.Weight)
		points, _ := decimal.NewFromString(r.Points)
		share := increment.Mul(w).DivRound(weight, 8)
		if share.GreaterThan(remaining) || i == len(requests)-1 {
			share = remaining
		}
		if !share.Equal(points) {
			return e, "original charges do not match the deterministic allocation"
		}
		remaining = remaining.Sub(share)
		if points.IsPositive() {
			r.StartedAt, r.CompletedAt, r.UpdatedAt = r.StartedAt.UTC(), r.CompletedAt.UTC(), r.UpdatedAt.UTC()
			end := r.End.UTC()
			r.End = &end
			e.Entries = append(e.Entries, Entry{RequestID: r.ID, UserID: r.UserID, Purpose: r.Purpose, Before: points.StringFixed(8), Delta: points.Neg().StringFixed(8), Original: r})
		}
	}
	id := sha256.Sum256([]byte(fmt.Sprintf("same-window-recovery-v1:%d:%d:%d:%d", s.AccountID, high.ID, drop.ID, recovery.ID)))
	e.ID = "recovery:" + hex.EncodeToString(id[:])
	return e, ""
}

// Digest pins the reviewed plan, including the entries and refund amounts.
func Digest(p Plan) string {
	b, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
