package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestPurposeAdjustSetLimitDoesNotChangeAnotherUserOrSettings(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	window := now.Add(-time.Hour)
	store := newMemoryAdjustStore()
	store.users[12] = true
	store.users[13] = true
	store.wallets[walletKey(12, PurposeExperiment)] = walletRow(12, PurposeExperiment, "500.00000000", "1.25000000", "month", window)
	store.wallets[walletKey(13, PurposeExperiment)] = walletRow(13, PurposeExperiment, "90.00000000", "2.00000000", "month", window)
	store.settings = `{"experiment":{"limit":500,"period":"month"}}`

	out, err := newAdjustService(store, now).Adjust(context.Background(), PurposeAdjustInput{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     PurposeExperiment,
		Action:      PurposeAdjustSetLimit,
		Value:       "80",
		Note:        "less",
		Confirm:     true,
	})
	require.NoError(t, err)
	require.True(t, out.Changed)
	require.Equal(t, "80.00000000", out.Wallet.LimitAmount)
	require.Equal(t, "1.25000000", out.Wallet.StoredUsedAmount)
	require.Equal(t, "1.25000000", out.Wallet.EffectiveUsedAmount)
	require.Equal(t, "month", out.Wallet.Period)
	require.True(t, out.Wallet.WindowStart.Equal(window))
	require.NotNil(t, out.Wallet.Official)
	require.Equal(t, PurposeAdjustOriginTyped, out.Audit.Origin)
	require.Equal(t, PurposeAdjustFieldLimit, out.Audit.Field)
	require.Equal(t, int64(7), out.Audit.ActorUserID)
	require.Equal(t, int64(12), out.Audit.TargetUserID)
	require.Equal(t, "500.00000000", out.Audit.OldValue)
	require.Equal(t, "80.00000000", out.Audit.NewValue)

	other := store.wallets[walletKey(13, PurposeExperiment)]
	require.Equal(t, "90.00000000", other.LimitAmount)
	require.Equal(t, "2.00000000", other.UsedAmount)
	require.True(t, other.WindowStart.Equal(window))
	require.Equal(t, 0, store.settingsWrites)
	require.Equal(t, `{"experiment":{"limit":500,"period":"month"}}`, store.settings)
	require.NotContains(t, store.touchedTables, "settings")
}

func TestPurposeAdjustAlignWindowKeepsUsedAmount(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	window := now.Add(-8 * 24 * time.Hour)
	store := newMemoryAdjustStore()
	store.users[12] = true
	store.wallets[walletKey(12, PurposeOpenAI)] = walletRow(12, PurposeOpenAI, "100.00000000", "4.25000000", "week", window)

	_, err := newAdjustService(store, now).Adjust(context.Background(), PurposeAdjustInput{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     PurposeOpenAI,
		Action:      PurposeAdjustAlignWindow,
		Value:       now.Format(time.RFC3339),
		Confirm:     true,
	})
	require.Equal(t, "PURPOSE_YUAN_RETIRED", infraerrors.Reason(err))
	require.Equal(t, "4.25000000", store.wallets[walletKey(12, PurposeOpenAI)].UsedAmount)
}

func TestPurposeAdjustRebuildDoesNotDeleteUsageLogs(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	window := now.Add(-2 * time.Hour)
	store := newMemoryAdjustStore()
	store.users[12] = true
	store.wallets[walletKey(12, PurposeGrok)] = walletRow(12, PurposeGrok, "100.00000000", "0.25000000", "week", window)
	store.logs = []memoryUsageLog{
		{UserID: 12, Purpose: PurposeGrok, Cost: "0.50000000", At: window.Add(time.Minute)},
		{UserID: 12, Purpose: PurposeGrok, Cost: "0.75000000", At: window.Add(2 * time.Minute)},
		{UserID: 12, Purpose: PurposeOpenAI, Cost: "9.00000000", At: window.Add(time.Minute)},
		{UserID: 13, Purpose: PurposeGrok, Cost: "8.00000000", At: window.Add(time.Minute)},
		{UserID: 12, Purpose: PurposeGrok, Cost: "4.00000000", At: window.Add(-time.Minute)},
	}
	before := append([]memoryUsageLog(nil), store.logs...)

	_, err := newAdjustService(store, now).Adjust(context.Background(), PurposeAdjustInput{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     PurposeGrok,
		Action:      PurposeAdjustRebuildUsed,
		Confirm:     true,
	})
	require.Equal(t, "PURPOSE_YUAN_RETIRED", infraerrors.Reason(err))
	require.Equal(t, before, store.logs)
	require.Equal(t, "0.25000000", store.wallets[walletKey(12, PurposeGrok)].UsedAmount)
	require.NotContains(t, store.touchedTables, "usage_logs_delete")
}

func TestPurposeAdjustUnconfirmed(t *testing.T) {
	store := newMemoryAdjustStore()
	_, err := newAdjustService(store, time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)).Adjust(context.Background(), PurposeAdjustInput{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     PurposeOpenAI,
		Action:      PurposeAdjustSetLimit,
		Value:       "80",
		Confirm:     false,
	})
	require.Equal(t, "CONFIRMATION_REQUIRED", infraerrors.Reason(err))
	require.Equal(t, 400, infraerrors.Code(err))
	require.Equal(t, 0, store.adjustCalls)
}

func TestPurposeAdjustExpiredSetUsed(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	window := now.Add(-8 * 24 * time.Hour)
	store := newMemoryAdjustStore()
	store.users[12] = true
	store.wallets[walletKey(12, PurposeOpenAI)] = walletRow(12, PurposeOpenAI, "100.00000000", "3.00000000", "week", window)

	_, err := newAdjustService(store, now).Adjust(context.Background(), PurposeAdjustInput{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     PurposeOpenAI,
		Action:      PurposeAdjustSetUsed,
		Value:       "1",
		Confirm:     true,
	})
	require.Equal(t, "PURPOSE_YUAN_RETIRED", infraerrors.Reason(err))
	require.Equal(t, 400, infraerrors.Code(err))
	require.Equal(t, "3.00000000", store.wallets[walletKey(12, PurposeOpenAI)].UsedAmount)
	require.Empty(t, store.audits)

	_, err = newAdjustService(store, now).Adjust(context.Background(), PurposeAdjustInput{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     PurposeOpenAI,
		Action:      PurposeAdjustRebuildUsed,
		Confirm:     true,
	})
	require.Equal(t, "PURPOSE_YUAN_RETIRED", infraerrors.Reason(err))
	require.Empty(t, store.logs)
}

func TestPurposeAdjustValueRange(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	store := newMemoryAdjustStore()
	svc := newAdjustService(store, now)
	base := PurposeAdjustInput{ActorUserID: 7, UserID: 12, Purpose: PurposeExperiment, Confirm: true}

	_, err := parseAdjustmentAmount("999999999999.99999999")
	require.NoError(t, err)
	_, err = parseAdjustmentAmount("1000000000000")
	require.Equal(t, "INVALID_ADJUSTMENT_VALUE", infraerrors.Reason(err))
	_, err = parseAdjustmentAmount("-1")
	require.Error(t, err)
	_, err = parseAdjustmentAmount("1e2")
	require.Error(t, err)
	_, err = parseAdjustmentAmount("80.")
	require.Error(t, err)

	limit := base
	limit.Action = PurposeAdjustSetLimit
	limit.Value = ""
	_, err = svc.Adjust(context.Background(), limit)
	require.Equal(t, "INVALID_ADJUSTMENT_VALUE", infraerrors.Reason(err))

	note := base
	note.Action = PurposeAdjustSetLimit
	note.Value = "1"
	note.Note = strings.Repeat("注", 501)
	_, err = svc.Adjust(context.Background(), note)
	require.Equal(t, "INVALID_ADJUSTMENT_NOTE", infraerrors.Reason(err))

	early := base
	early.Action = PurposeAdjustAlignWindow
	early.Value = "2019-12-31T23:59:59Z"
	_, err = svc.Adjust(context.Background(), early)
	require.Equal(t, "PURPOSE_YUAN_RETIRED", infraerrors.Reason(err))

	late := base
	late.Action = PurposeAdjustSetUsed
	late.Value = "1"
	_, err = svc.Adjust(context.Background(), late)
	require.Equal(t, "PURPOSE_YUAN_RETIRED", infraerrors.Reason(err))

	_, err = svc.Adjust(context.Background(), base)
	require.Equal(t, "PURPOSE_YUAN_RETIRED", infraerrors.Reason(err))
}

func TestPurposeAdjustEqualAmountSkipsAudit(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	window := now.Add(-time.Hour)
	store := newMemoryAdjustStore()
	store.users[12] = true
	store.users[13] = true
	store.wallets[walletKey(12, PurposeExperiment)] = walletRow(12, PurposeExperiment, "100.00000000", "1.25000000", "month", window)
	store.wallets[walletKey(13, PurposeExperiment)] = walletRow(13, PurposeExperiment, "90.00000000", "2.00000000", "month", window)
	store.settings = "policy"

	out, err := newAdjustService(store, now).Adjust(context.Background(), PurposeAdjustInput{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     PurposeExperiment,
		Action:      PurposeAdjustSetLimit,
		Value:       "100.000000004",
		Confirm:     true,
	})
	require.NoError(t, err)
	require.False(t, out.Changed)
	require.Nil(t, out.Audit)
	require.Equal(t, "100.00000000", out.Wallet.LimitAmount)
	require.Equal(t, "90.00000000", store.wallets[walletKey(13, PurposeExperiment)].LimitAmount)
	require.Equal(t, "policy", store.settings)
	require.Empty(t, store.audits)

	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"changed":false`)
	require.Contains(t, string(encoded), `"audit":null`)
	require.Contains(t, string(encoded), `"stored_used_amount"`)
}

func TestPurposeAdjustListKeyset(t *testing.T) {
	store := newMemoryAdjustStore()
	store.audits = []PurposeAdjustmentAudit{
		{ID: 3, TargetUserID: 12},
		{ID: 2, TargetUserID: 13},
		{ID: 1, TargetUserID: 12},
	}
	userID := int64(12)
	before := int64(3)
	out, err := newAdjustService(store, time.Now().UTC()).ListAdjustments(context.Background(), PurposeAdjustmentQuery{
		UserID:   &userID,
		BeforeID: &before,
		Limit:    500,
	})
	require.NoError(t, err)
	require.Equal(t, 200, store.listQuery.Limit)
	require.Equal(t, []int64{1}, auditIDs(out.Items))

	out, err = newAdjustService(store, time.Now().UTC()).ListAdjustments(context.Background(), PurposeAdjustmentQuery{})
	require.NoError(t, err)
	require.Equal(t, 50, store.listQuery.Limit)
	require.Equal(t, []int64{3, 2, 1}, auditIDs(out.Items))
}

func TestPurposeAdjustMissingUser(t *testing.T) {
	store := newMemoryAdjustStore()
	_, err := newAdjustService(store, time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)).Adjust(context.Background(), PurposeAdjustInput{
		ActorUserID: 7,
		UserID:      99,
		Purpose:     PurposeExperiment,
		Action:      PurposeAdjustSetLimit,
		Value:       "80",
		Confirm:     true,
	})
	require.Equal(t, "USER_NOT_FOUND", infraerrors.Reason(err))
	require.Equal(t, 404, infraerrors.Code(err))
	require.Equal(t, 0, store.settingsWrites)
}

type memoryUsageLog struct {
	UserID  int64
	Purpose string
	Cost    string
	At      time.Time
}

type memoryAdjustStore struct {
	users          map[int64]bool
	wallets        map[string]UsageMonitorUserRow
	logs           []memoryUsageLog
	settings       string
	settingsWrites int
	touchedTables  []string
	audits         []PurposeAdjustmentAudit
	nextID         int64
	adjustCalls    int
	listQuery      PurposeAdjustmentQuery
}

func newMemoryAdjustStore() *memoryAdjustStore {
	return &memoryAdjustStore{
		users:   map[int64]bool{},
		wallets: map[string]UsageMonitorUserRow{},
	}
}

func newAdjustService(store *memoryAdjustStore, now time.Time) *UsageMonitorService {
	svc := NewUsageMonitorService(store)
	svc.now = func() time.Time { return now }
	return svc
}

func walletKey(userID int64, purpose string) string {
	return fmt.Sprintf("%d:%s", userID, purpose)
}

func walletRow(userID int64, purpose, limit, used, period string, window time.Time) UsageMonitorUserRow {
	return UsageMonitorUserRow{
		ID: userID, Purpose: purpose, LimitAmount: limit, UsedAmount: used,
		Period: period, WindowStart: window, HasWallet: true,
	}
}

func (s *memoryAdjustStore) AdjustPurposeWallet(_ context.Context, cmd PurposeWalletAdjust) (PurposeWalletAdjustResult, error) {
	s.adjustCalls++
	if !s.users[cmd.UserID] {
		return PurposeWalletAdjustResult{}, infraerrors.NotFound("USER_NOT_FOUND", "user not found")
	}
	key := walletKey(cmd.UserID, cmd.Purpose)
	row, ok := s.wallets[key]
	if !ok {
		row = walletRow(cmd.UserID, cmd.Purpose, "100.00000000", "0.00000000", PurposePeriod(cmd.Purpose), cmd.Now)
		s.wallets[key] = row
	}
	rebuilt := ""
	if cmd.Action == PurposeAdjustRebuildUsed {
		rebuilt = s.sumCost(cmd.UserID, cmd.Purpose, row)
	}
	effect, err := PurposeAdjustmentEffect(row, cmd, rebuilt)
	if err != nil {
		return PurposeWalletAdjustResult{}, err
	}
	if !effect.Changed {
		return PurposeWalletAdjustResult{Changed: false, Field: effect.Field, Origin: effect.Origin, Row: row}, nil
	}
	switch cmd.Action {
	case PurposeAdjustSetLimit:
		row.LimitAmount = effect.Amount
	case PurposeAdjustSetUsed, PurposeAdjustRebuildUsed:
		row.UsedAmount = effect.Amount
	case PurposeAdjustAlignWindow:
		row.WindowStart = effect.Window
	}
	s.wallets[key] = row
	s.nextID++
	audit := PurposeAdjustmentAudit{
		ID:           s.nextID,
		ActorUserID:  cmd.ActorUserID,
		TargetUserID: cmd.UserID,
		Purpose:      cmd.Purpose,
		Field:        effect.Field,
		Origin:       effect.Origin,
		OldValue:     effect.OldValue,
		NewValue:     effect.NewValue,
		Note:         cmd.Note,
		CreatedAt:    cmd.Now,
	}
	s.audits = append(s.audits, audit)
	return PurposeWalletAdjustResult{Changed: true, Field: effect.Field, Origin: effect.Origin, Row: row, Audit: &s.audits[len(s.audits)-1]}, nil
}

func (s *memoryAdjustStore) sumCost(userID int64, purpose string, row UsageMonitorUserRow) string {
	period := AllowancePeriod(purpose, row.Period)
	end := PurposeResetsAt(period, row.WindowStart)
	acc := mustRat("0")
	for _, item := range s.logs {
		if item.UserID != userID || item.Purpose != purpose {
			continue
		}
		if item.At.Before(row.WindowStart) || !item.At.Before(end) {
			continue
		}
		part, ok := parseDecimal(item.Cost)
		if !ok {
			continue
		}
		acc.Add(acc, part)
	}
	return amountText(acc)
}

func (s *memoryAdjustStore) ListPurposeAdjustments(_ context.Context, query PurposeAdjustmentQuery) ([]PurposeAdjustmentAudit, error) {
	s.listQuery = query
	out := make([]PurposeAdjustmentAudit, 0)
	for _, item := range s.audits {
		if query.UserID != nil && item.TargetUserID != *query.UserID {
			continue
		}
		if query.BeforeID != nil && item.ID >= *query.BeforeID {
			continue
		}
		out = append(out, item)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].ID > out[i].ID {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	limit := query.Limit
	if limit <= 0 || limit > len(out) {
		if limit <= 0 {
			limit = len(out)
		}
	}
	if limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}

func (s *memoryAdjustStore) ListOfficialParents(context.Context) ([]UsageMonitorParentRow, error) {
	return nil, nil
}
func (s *memoryAdjustStore) ListStampedParentIDs(context.Context, time.Time) ([]int64, error) {
	return nil, nil
}
func (s *memoryAdjustStore) ListLatestStamps(context.Context, []int64, time.Time) ([]UsageMonitorStamp, error) {
	return nil, nil
}
func (s *memoryAdjustStore) ListWindowUsage(context.Context, []UsageMonitorWindowQuery, time.Duration) ([]UsageMonitorWindowUsage, error) {
	return nil, nil
}
func (s *memoryAdjustStore) ListLiveUsers(context.Context) ([]UsageMonitorUserRow, error) {
	return nil, nil
}
func (s *memoryAdjustStore) ListExperimentGroupIDs(context.Context) ([]int64, error) {
	return nil, nil
}
func (s *memoryAdjustStore) ListExperimentRequestCounts(context.Context, []int64, time.Time, time.Time) ([]UsageMonitorExperimentCount, error) {
	return nil, nil
}

func auditIDs(items []PurposeAdjustmentAudit) []int64 {
	out := make([]int64, len(items))
	for i, item := range items {
		out[i] = item.ID
	}
	return out
}

var _ UsageMonitorStore = (*memoryAdjustStore)(nil)
