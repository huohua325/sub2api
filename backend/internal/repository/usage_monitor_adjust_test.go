package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestPurposeAdjustSetLimitDoesNotChangeAnotherUserOrSettings(t *testing.T) {
	ctx := context.Background()
	db, mock := newAdjustMock(t)
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	window := now.Add(-time.Hour)
	setClause := sqlSetClause(purposeAdjustLimitSQL)

	require.Contains(t, purposeAdjustLimitSQL, "WHERE user_id = $1 AND purpose = $2")
	require.Contains(t, setClause, "limit_amount")
	require.NotContains(t, setClause, "used_amount")
	require.NotContains(t, setClause, "period")
	require.NotContains(t, setClause, "window_start")
	require.NotContains(t, strings.ToLower(purposeAdjustLimitSQL), "settings")
	require.NotContains(t, purposeAdjustLimitSQL, "purpose_allowance_policy")
	require.NotContains(t, purposeAdjustUserSQL, "13")
	require.NotContains(t, purposeAdjustLockSQL, "13")

	mock.ExpectBegin()
	expectLiveUser(mock, 12)
	mock.ExpectQuery(purposeAdjustLockSQL).WithArgs(int64(12), "openai").
		WillReturnRows(walletRows("100.00000000", "1.25000000", "week", window))
	mock.ExpectQuery(purposeAdjustLimitSQL).WithArgs(int64(12), "openai", "80.00000000").
		WillReturnRows(walletRows("80.00000000", "1.25000000", "week", window))
	mock.ExpectQuery(purposeAdjustAuditSQL).WithArgs(
		int64(7), int64(12), "openai", "limit_amount", "typed", "100.00000000", "80.00000000", "less",
	).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(4), now))
	mock.ExpectCommit()

	result, err := NewUsageMonitorRepository(db).AdjustPurposeWallet(ctx, service.PurposeWalletAdjust{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     "openai",
		Action:      service.PurposeAdjustSetLimit,
		AmountText:  "80.00000000",
		Note:        "less",
		Now:         now,
	})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, int64(12), result.Row.ID)
	require.Equal(t, "80.00000000", result.Row.LimitAmount)
	require.Equal(t, "1.25000000", result.Row.UsedAmount)
	require.Equal(t, "week", result.Row.Period)
	require.True(t, result.Row.WindowStart.Equal(window))
	require.Equal(t, "typed", result.Audit.Origin)
	require.NotEqual(t, int64(13), result.Row.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPurposeAdjustAlignWindowDoesNotChangeUsedAmount(t *testing.T) {
	ctx := context.Background()
	db, mock := newAdjustMock(t)
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	window := now.Add(-8 * 24 * time.Hour)
	next := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	setClause := sqlSetClause(purposeAdjustWindowSQL)
	require.Contains(t, setClause, "window_start")
	require.NotContains(t, setClause, "used_amount")
	require.NotContains(t, setClause, "limit_amount")
	require.NotContains(t, purposeAdjustWindowSQL, "used_amount = 0")
	require.NotContains(t, strings.ToUpper(purposeAdjustWindowSQL), "DELETE")

	oldText := window.UTC().Format(time.RFC3339Nano)
	newText := next.UTC().Format(time.RFC3339Nano)
	mock.ExpectBegin()
	expectLiveUser(mock, 12)
	mock.ExpectQuery(purposeAdjustLockSQL).WithArgs(int64(12), "openai").
		WillReturnRows(walletRows("100.00000000", "4.25000000", "week", window))
	mock.ExpectQuery(purposeAdjustWindowSQL).WithArgs(int64(12), "openai", next).
		WillReturnRows(walletRows("100.00000000", "4.25000000", "week", next))
	mock.ExpectQuery(purposeAdjustAuditSQL).WithArgs(
		int64(7), int64(12), "openai", "window_start", "align", oldText, newText, "",
	).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(5), now))
	mock.ExpectCommit()

	result, err := NewUsageMonitorRepository(db).AdjustPurposeWallet(ctx, service.PurposeWalletAdjust{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     "openai",
		Action:      service.PurposeAdjustAlignWindow,
		Window:      &next,
		Now:         now,
	})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, "4.25000000", result.Row.UsedAmount)
	require.NotEqual(t, "0", result.Row.UsedAmount)
	require.NotEqual(t, "0.00000000", result.Row.UsedAmount)
	require.Equal(t, "100.00000000", result.Row.LimitAmount)
	require.True(t, result.Row.WindowStart.Equal(next))
	require.Equal(t, "align", result.Audit.Origin)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPurposeAdjustRebuildDoesNotDeleteUsageLogs(t *testing.T) {
	ctx := context.Background()
	db, mock := newAdjustMock(t)
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	window := now.Add(-2 * time.Hour)
	end := window.Add(7 * 24 * time.Hour)
	require.Contains(t, purposeAdjustSumSQL, "ROUND(SUM(ul.actual_cost), 8)")
	require.Contains(t, purposeAdjustSumSQL, "COALESCE(NULLIF(lower(btrim(g.usage_kind)), ''), 'experiment')")
	require.NotContains(t, strings.ToUpper(purposeAdjustSumSQL), "DELETE")
	require.NotContains(t, purposeAdjustSumSQL, "deleted_at")
	require.NotContains(t, strings.ToUpper(purposeAdjustUsedSQL), "DELETE")
	require.NotContains(t, sqlSetClause(purposeAdjustUsedSQL), "limit_amount")
	require.NotContains(t, sqlSetClause(purposeAdjustUsedSQL), "window_start")

	mock.ExpectBegin()
	expectLiveUser(mock, 12)
	mock.ExpectQuery(purposeAdjustLockSQL).WithArgs(int64(12), "grok").
		WillReturnRows(walletRows("100.00000000", "0.25000000", "week", window))
	mock.ExpectQuery(purposeAdjustSumSQL).WithArgs(int64(12), window, end, "grok").
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow("1.25000000"))
	mock.ExpectQuery(purposeAdjustUsedSQL).WithArgs(int64(12), "grok", "1.25000000").
		WillReturnRows(walletRows("100.00000000", "1.25000000", "week", window))
	mock.ExpectQuery(purposeAdjustAuditSQL).WithArgs(
		int64(7), int64(12), "grok", "used_amount", "rebuild", "0.25000000", "1.25000000", "",
	).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(6), now))
	mock.ExpectCommit()

	result, err := NewUsageMonitorRepository(db).AdjustPurposeWallet(ctx, service.PurposeWalletAdjust{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     "grok",
		Action:      service.PurposeAdjustRebuildUsed,
		Now:         now,
	})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, "1.25000000", result.Row.UsedAmount)
	require.Equal(t, "100.00000000", result.Row.LimitAmount)
	require.True(t, result.Row.WindowStart.Equal(window))
	require.Equal(t, "rebuild", result.Audit.Origin)
	require.Equal(t, "used_amount", result.Audit.Field)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPurposeAdjustSetUsedExpiredWindow(t *testing.T) {
	ctx := context.Background()
	db, mock := newAdjustMock(t)
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	window := now.Add(-8 * 24 * time.Hour)
	mock.ExpectBegin()
	expectLiveUser(mock, 12)
	mock.ExpectQuery(purposeAdjustLockSQL).WithArgs(int64(12), "openai").
		WillReturnRows(walletRows("100.00000000", "3.00000000", "week", window))
	mock.ExpectRollback()

	_, err := NewUsageMonitorRepository(db).AdjustPurposeWallet(ctx, service.PurposeWalletAdjust{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     "openai",
		Action:      service.PurposeAdjustSetUsed,
		AmountText:  "1.00000000",
		Now:         now,
	})
	require.Equal(t, "PURPOSE_WINDOW_EXPIRED", infraerrors.Reason(err))
	require.Equal(t, 409, infraerrors.Code(err))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPurposeAdjustEqualAmountSkipsAudit(t *testing.T) {
	ctx := context.Background()
	db, mock := newAdjustMock(t)
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	window := now.Add(-time.Hour)
	mock.ExpectBegin()
	expectLiveUser(mock, 12)
	mock.ExpectQuery(purposeAdjustLockSQL).WithArgs(int64(12), "openai").
		WillReturnRows(walletRows("100.00000000", "1.25000000", "week", window))
	mock.ExpectCommit()

	result, err := NewUsageMonitorRepository(db).AdjustPurposeWallet(ctx, service.PurposeWalletAdjust{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     "openai",
		Action:      service.PurposeAdjustSetLimit,
		AmountText:  "100",
		Now:         now,
	})
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.Nil(t, result.Audit)
	require.Equal(t, "100.00000000", result.Row.LimitAmount)
	require.Equal(t, "1.25000000", result.Row.UsedAmount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPurposeAdjustMissingUser(t *testing.T) {
	ctx := context.Background()
	db, mock := newAdjustMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(purposeAdjustUserSQL).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"ok"}))
	mock.ExpectRollback()

	_, err := NewUsageMonitorRepository(db).AdjustPurposeWallet(ctx, service.PurposeWalletAdjust{
		ActorUserID: 7,
		UserID:      99,
		Purpose:     "openai",
		Action:      service.PurposeAdjustSetLimit,
		AmountText:  "80.00000000",
		Now:         time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
	})
	require.Equal(t, "USER_NOT_FOUND", infraerrors.Reason(err))
	require.Equal(t, 404, infraerrors.Code(err))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPurposeAdjustEnsuresWalletWithoutWritingSettings(t *testing.T) {
	ctx := context.Background()
	db, mock := newAdjustMock(t)
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	window := now.Add(-time.Minute)
	require.NotContains(t, strings.ToLower(purposeAdjustEnsureSQL), "settings")
	require.Contains(t, purposeAdjustEnsureSQL, "ON CONFLICT (user_id, purpose) DO NOTHING")

	mock.ExpectBegin()
	expectLiveUser(mock, 12)
	mock.ExpectQuery(purposeAdjustLockSQL).WithArgs(int64(12), "openai").
		WillReturnRows(sqlmock.NewRows([]string{"limit_amount", "used_amount", "period", "window_start"}))
	mock.ExpectQuery("SELECT value FROM settings WHERE key = $1").
		WithArgs(service.SettingKeyPurposeAllowancePolicy).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(purposeAdjustEnsureSQL).
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectQuery(purposeAdjustLockSQL).WithArgs(int64(12), "openai").
		WillReturnRows(walletRows("100.00000000", "0.00000000", "week", window))
	mock.ExpectQuery(purposeAdjustLimitSQL).WithArgs(int64(12), "openai", "80.00000000").
		WillReturnRows(walletRows("80.00000000", "0.00000000", "week", window))
	mock.ExpectQuery(purposeAdjustAuditSQL).WithArgs(
		int64(7), int64(12), "openai", "limit_amount", "typed", "100.00000000", "80.00000000", "",
	).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(8), now))
	mock.ExpectCommit()

	result, err := NewUsageMonitorRepository(db).AdjustPurposeWallet(ctx, service.PurposeWalletAdjust{
		ActorUserID: 7,
		UserID:      12,
		Purpose:     "openai",
		Action:      service.PurposeAdjustSetLimit,
		AmountText:  "80.00000000",
		Now:         now,
	})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, "80.00000000", result.Row.LimitAmount)
	require.Equal(t, "0.00000000", result.Row.UsedAmount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPurposeAdjustListKeysetDoesNotInsert(t *testing.T) {
	ctx := context.Background()
	db, mock := newAdjustMock(t)
	userID := int64(12)
	beforeID := int64(40)
	statement, args := purposeAdjustmentListSQL(service.PurposeAdjustmentQuery{
		UserID: &userID, BeforeID: &beforeID, Limit: 10,
	})
	require.Contains(t, statement, "AND target_user_id = $1")
	require.Contains(t, statement, "AND id < $2")
	require.Contains(t, statement, "ORDER BY id DESC LIMIT $3")
	require.NotContains(t, strings.ToUpper(statement), "INSERT")
	require.NotContains(t, strings.ToUpper(statement), "DELETE")
	require.NotContains(t, statement, "user_purpose_allowances")

	created := time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC)
	mock.ExpectQuery(statement).WithArgs(args[0], args[1], args[2]).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "actor_user_id", "target_user_id", "purpose", "field", "origin",
			"old_value", "new_value", "note", "created_at",
		}).AddRow(int64(9), int64(7), int64(12), "openai", "limit_amount", "typed", "100.00000000", "80.00000000", "less", created))

	items, err := NewUsageMonitorRepository(db).ListPurposeAdjustments(ctx, service.PurposeAdjustmentQuery{
		UserID: &userID, BeforeID: &beforeID, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(9), items[0].ID)
	require.Equal(t, "less", items[0].Note)
	require.True(t, items[0].CreatedAt.Equal(created))
	require.NoError(t, mock.ExpectationsWereMet())
}

func newAdjustMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func expectLiveUser(mock sqlmock.Sqlmock, userID int64) {
	mock.ExpectQuery(purposeAdjustUserSQL).WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(1))
}

func walletRows(limit, used, period string, window time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"limit_amount", "used_amount", "period", "window_start"}).
		AddRow(limit, used, period, window)
}

func sqlSetClause(statement string) string {
	setAt := strings.Index(statement, "SET ")
	whereAt := strings.Index(statement, "WHERE ")
	if setAt < 0 || whereAt < 0 || whereAt < setAt {
		return statement
	}
	return statement[setAt:whereAt]
}
