package service

import (
	"context"
	"log/slog"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	PurposeAdjustSetLimit    = "set_limit"
	PurposeAdjustSetUsed     = "set_used"
	PurposeAdjustRebuildUsed = "rebuild_used"
	PurposeAdjustAlignWindow = "align_window"

	PurposeAdjustFieldLimit  = "limit_amount"
	PurposeAdjustFieldUsed   = "used_amount"
	PurposeAdjustFieldWindow = "window_start"

	PurposeAdjustOriginTyped   = "typed"
	PurposeAdjustOriginRebuild = "rebuild"
	PurposeAdjustOriginAlign   = "align"

	purposeAdjustNoteMax = 500
	purposeAdjustScale   = 8
	// NUMERIC(20, 8) keeps 12 digits before the decimal point.
	purposeAdjustIntegerDigits = 12
	purposeAdjustAmountMaxLen  = 80
	purposeAdjustWindowDays    = 366
)

var purposeAdjustWindowMin = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// PurposeAdjustInput is one confirmed edit of a single purpose wallet.
type PurposeAdjustInput struct {
	ActorUserID int64
	UserID      int64
	Purpose     string
	Action      string
	Value       string
	Note        string
	Confirm     bool
}

// PurposeWalletAdjust is the validated command executed inside the wallet lock.
type PurposeWalletAdjust struct {
	ActorUserID int64
	UserID      int64
	Purpose     string
	Action      string
	AmountText  string
	Window      *time.Time
	Note        string
	Now         time.Time
}

// PurposeAdjustmentAudit is one row of purpose_allowance_adjustments.
type PurposeAdjustmentAudit struct {
	ID           int64     `json:"id"`
	ActorUserID  int64     `json:"actor_user_id"`
	TargetUserID int64     `json:"target_user_id"`
	Purpose      string    `json:"purpose"`
	Field        string    `json:"field"`
	Origin       string    `json:"origin"`
	OldValue     string    `json:"old_value"`
	NewValue     string    `json:"new_value"`
	Note         string    `json:"note"`
	CreatedAt    time.Time `json:"created_at"`
}

// PurposeAdjustResult is the POST adjustments payload inside response.Success.
type PurposeAdjustResult struct {
	Changed bool                    `json:"changed"`
	Wallet  UsageMonitorWallet      `json:"wallet"`
	Audit   *PurposeAdjustmentAudit `json:"audit"`
}

// PurposeWalletAdjustResult is the locked wallet after the transaction.
type PurposeWalletAdjustResult struct {
	Changed bool
	Field   string
	Origin  string
	Row     UsageMonitorUserRow
	Audit   *PurposeAdjustmentAudit
}

// PurposeWalletEffect is the single-column change decided under the row lock.
type PurposeWalletEffect struct {
	Changed  bool
	Field    string
	Origin   string
	OldValue string
	NewValue string
	Amount   string
	Window   time.Time
}

// PurposeAdjustmentQuery is the keyset for GET adjustments.
type PurposeAdjustmentQuery struct {
	UserID   *int64
	BeforeID *int64
	Limit    int
}

// PurposeAdjustmentList is the GET adjustments payload inside response.Success.
type PurposeAdjustmentList struct {
	Items []PurposeAdjustmentAudit `json:"items"`
}

// Adjust changes one user's purpose wallet and records an audit row when the
// stored value actually moves.
func (s *UsageMonitorService) Adjust(ctx context.Context, in PurposeAdjustInput) (*PurposeAdjustResult, error) {
	if s == nil || s.store == nil {
		return nil, infraerrors.ServiceUnavailable("USAGE_MONITOR_UNAVAILABLE", "用量监控尚未就绪")
	}
	nowFn := s.now
	if nowFn == nil {
		nowFn = time.Now
	}
	cmd, err := validatePurposeAdjust(in, nowFn().UTC())
	if err != nil {
		logPurposeAdjustFailure(in, err)
		return nil, err
	}
	stored, err := s.store.AdjustPurposeWallet(ctx, cmd)
	if err != nil {
		logPurposeAdjustFailure(in, err)
		return nil, err
	}
	wallet, err := s.adjustmentWallet(ctx, stored.Row, cmd.Now)
	if err != nil {
		logPurposeAdjustFailure(in, err)
		return nil, err
	}
	slog.Info("usage_monitor_adjust",
		"actor_user_id", cmd.ActorUserID,
		"target_user_id", cmd.UserID,
		"purpose", cmd.Purpose,
		"field", stored.Field,
		"origin", stored.Origin,
		"changed", stored.Changed,
	)
	return &PurposeAdjustResult{
		Changed: stored.Changed,
		Wallet:  wallet,
		Audit:   stored.Audit,
	}, nil
}

// ListAdjustments returns audit rows newest id first. It does not create wallets.
func (s *UsageMonitorService) ListAdjustments(ctx context.Context, query PurposeAdjustmentQuery) (*PurposeAdjustmentList, error) {
	if s == nil || s.store == nil {
		return nil, infraerrors.ServiceUnavailable("USAGE_MONITOR_UNAVAILABLE", "用量监控尚未就绪")
	}
	normalized, err := normalizePurposeAdjustmentQuery(query)
	if err != nil {
		return nil, err
	}
	items, err := s.store.ListPurposeAdjustments(ctx, normalized)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []PurposeAdjustmentAudit{}
	}
	return &PurposeAdjustmentList{Items: items}, nil
}

func (s *UsageMonitorService) adjustmentWallet(ctx context.Context, row UsageMonitorUserRow, now time.Time) (UsageMonitorWallet, error) {
	var counts []UsageMonitorRequestCount
	if row.Purpose == PurposeExperiment {
		loaded, err := s.experimentCountsForUser(ctx, row.ID, now)
		if err != nil {
			// The wallet write already committed. Counts are display-only.
			slog.Warn("usage_monitor_adjust_counts", "target_user_id", row.ID, "reason", infraerrors.Reason(err))
		} else {
			counts = loaded
		}
	}
	wallet, _, err := monitorWallet(row, counts, now)
	return wallet, err
}

func (s *UsageMonitorService) experimentCountsForUser(ctx context.Context, userID int64, now time.Time) ([]UsageMonitorRequestCount, error) {
	groupIDs, err := s.store.ListExperimentGroupIDs(ctx)
	if err != nil {
		return nil, err
	}
	all, err := s.store.ListExperimentRequestCounts(ctx, groupIDs, ShanghaiMonthStart(now), nextShanghaiMonthStart(now))
	if err != nil {
		return nil, err
	}
	out := make([]UsageMonitorRequestCount, 0)
	for _, item := range all {
		if item.UserID != userID {
			continue
		}
		out = append(out, UsageMonitorRequestCount{Platform: item.Platform, Count: item.Count})
	}
	return out, nil
}

func validatePurposeAdjust(in PurposeAdjustInput, now time.Time) (PurposeWalletAdjust, error) {
	if !in.Confirm {
		return PurposeWalletAdjust{}, infraerrors.BadRequest("CONFIRMATION_REQUIRED", "confirmation required")
	}
	if in.ActorUserID <= 0 {
		return PurposeWalletAdjust{}, infraerrors.Unauthorized("ADJUSTMENT_ACTOR_REQUIRED", "authentication required")
	}
	if in.UserID <= 0 {
		return PurposeWalletAdjust{}, infraerrors.NotFound("USER_NOT_FOUND", "user not found")
	}
	purpose := strings.TrimSpace(in.Purpose)
	if purpose != PurposeExperiment || in.Action != PurposeAdjustSetLimit {
		return PurposeWalletAdjust{}, infraerrors.BadRequest("PURPOSE_YUAN_RETIRED", "only the experiment yuan cap can be changed")
	}
	if utf8.RuneCountInString(in.Note) > purposeAdjustNoteMax {
		return PurposeWalletAdjust{}, infraerrors.BadRequest("INVALID_ADJUSTMENT_NOTE", "note is too long")
	}
	cmd := PurposeWalletAdjust{
		ActorUserID: in.ActorUserID,
		UserID:      in.UserID,
		Purpose:     purpose,
		Action:      in.Action,
		Note:        in.Note,
		Now:         now.UTC(),
	}
	switch in.Action {
	case PurposeAdjustSetLimit, PurposeAdjustSetUsed:
		text, err := submittedAmountText(in.Value)
		if err != nil {
			return PurposeWalletAdjust{}, err
		}
		cmd.AmountText = text
	case PurposeAdjustRebuildUsed:
	case PurposeAdjustAlignWindow:
		window, err := parseAdjustmentWindow(in.Value, now.UTC())
		if err != nil {
			return PurposeWalletAdjust{}, err
		}
		cmd.Window = &window
	default:
		return PurposeWalletAdjust{}, infraerrors.BadRequest("INVALID_ADJUSTMENT_ACTION", "action must be set_limit, set_used, rebuild_used, or align_window")
	}
	return cmd, nil
}

func normalizePurposeAdjustmentQuery(query PurposeAdjustmentQuery) (PurposeAdjustmentQuery, error) {
	if query.Limit == 0 {
		query.Limit = 50
	}
	if query.Limit < 0 {
		return PurposeAdjustmentQuery{}, infraerrors.BadRequest("INVALID_ADJUSTMENT_QUERY", "invalid limit")
	}
	if query.Limit > 200 {
		query.Limit = 200
	}
	if query.UserID != nil && *query.UserID <= 0 {
		return PurposeAdjustmentQuery{}, infraerrors.BadRequest("INVALID_ADJUSTMENT_QUERY", "invalid user_id")
	}
	if query.BeforeID != nil && *query.BeforeID <= 0 {
		return PurposeAdjustmentQuery{}, infraerrors.BadRequest("INVALID_ADJUSTMENT_QUERY", "invalid before_id")
	}
	return query, nil
}

// PurposeAdjustExpired reports the 409 for writing used_amount on a finished window.
// Aligning window_start stays allowed: it does not clear the stored amount.
func PurposeAdjustExpired(action, purpose, period string, windowStart, now time.Time) error {
	if action != PurposeAdjustSetUsed && action != PurposeAdjustRebuildUsed {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	if PurposeWindowExpired(AllowancePeriod(purpose, period), windowStart, now) {
		return infraerrors.Conflict("PURPOSE_WINDOW_EXPIRED", "purpose window expired")
	}
	return nil
}

// PurposeAdjustmentEffect decides the column write after the wallet row is locked.
// rebuiltUsed is the gateway sum for rebuild_used and is ignored otherwise.
// Equal 8-decimal amounts, or the same timestamp, set Changed false.
func PurposeAdjustmentEffect(row UsageMonitorUserRow, cmd PurposeWalletAdjust, rebuiltUsed string) (PurposeWalletEffect, error) {
	now := cmd.Now
	if now.IsZero() {
		now = time.Now()
	}
	if err := PurposeAdjustExpired(cmd.Action, row.Purpose, row.Period, row.WindowStart, now); err != nil {
		return PurposeWalletEffect{}, err
	}
	effect := PurposeWalletEffect{}
	switch cmd.Action {
	case PurposeAdjustSetLimit:
		effect.Field = PurposeAdjustFieldLimit
		effect.Origin = PurposeAdjustOriginTyped
		effect.OldValue = row.LimitAmount
		effect.Amount = cmd.AmountText
		effect.NewValue = cmd.AmountText
		effect.Changed = !amountTextEqual(row.LimitAmount, cmd.AmountText)
	case PurposeAdjustSetUsed:
		effect.Field = PurposeAdjustFieldUsed
		effect.Origin = PurposeAdjustOriginTyped
		effect.OldValue = row.UsedAmount
		effect.Amount = cmd.AmountText
		effect.NewValue = cmd.AmountText
		effect.Changed = !amountTextEqual(row.UsedAmount, cmd.AmountText)
	case PurposeAdjustRebuildUsed:
		canonical, err := canonicalAmountText(rebuiltUsed)
		if err != nil {
			return PurposeWalletEffect{}, err
		}
		effect.Field = PurposeAdjustFieldUsed
		effect.Origin = PurposeAdjustOriginRebuild
		effect.OldValue = row.UsedAmount
		effect.Amount = canonical
		effect.NewValue = canonical
		effect.Changed = !amountTextEqual(row.UsedAmount, canonical)
	case PurposeAdjustAlignWindow:
		effect.Field = PurposeAdjustFieldWindow
		effect.Origin = PurposeAdjustOriginAlign
		if cmd.Window == nil {
			return PurposeWalletEffect{}, errInvalidAdjustmentValue()
		}
		effect.Window = cmd.Window.UTC().Truncate(time.Microsecond)
		effect.OldValue = formatAdjustmentWindow(row.WindowStart)
		effect.NewValue = formatAdjustmentWindow(effect.Window)
		effect.Changed = !row.WindowStart.UTC().Truncate(time.Microsecond).Equal(effect.Window)
	default:
		return PurposeWalletEffect{}, infraerrors.BadRequest("INVALID_ADJUSTMENT_ACTION", "action must be set_limit, set_used, rebuild_used, or align_window")
	}
	return effect, nil
}

func submittedAmountText(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", errInvalidAdjustmentValue()
	}
	return canonicalAmountText(text)
}

func canonicalAmountText(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		text = "0"
	}
	parsed, err := parseAdjustmentAmount(text)
	if err != nil {
		return "", err
	}
	return amountText(parsed), nil
}

func parseAdjustmentAmount(text string) (*big.Rat, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > purposeAdjustAmountMaxLen {
		return nil, errInvalidAdjustmentValue()
	}
	if text[0] == '+' {
		text = text[1:]
		if text == "" {
			return nil, errInvalidAdjustmentValue()
		}
	}
	if strings.ContainsAny(text, "eE/") || strings.HasPrefix(text, "-") {
		return nil, errInvalidAdjustmentValue()
	}
	dot := false
	for _, r := range text {
		if r == '.' {
			if dot {
				return nil, errInvalidAdjustmentValue()
			}
			dot = true
			continue
		}
		if r < '0' || r > '9' {
			return nil, errInvalidAdjustmentValue()
		}
	}
	if text[0] == '.' || text[len(text)-1] == '.' {
		return nil, errInvalidAdjustmentValue()
	}
	parsed, ok := new(big.Rat).SetString(text)
	if !ok || parsed.Sign() < 0 {
		return nil, errInvalidAdjustmentValue()
	}
	rounded := roundHalfAway(parsed, purposeAdjustScale)
	if !fitsNumeric208(rounded) {
		return nil, errInvalidAdjustmentValue()
	}
	return rounded, nil
}

func fitsNumeric208(value *big.Rat) bool {
	if value == nil || value.Sign() < 0 {
		return false
	}
	whole := new(big.Int).Quo(value.Num(), value.Denom())
	if whole.Sign() == 0 {
		return true
	}
	return len(whole.String()) <= purposeAdjustIntegerDigits
}

func amountText(value *big.Rat) string {
	return roundHalfAway(value, purposeAdjustScale).FloatString(purposeAdjustScale)
}

func amountTextEqual(stored, next string) bool {
	left, okLeft := parseDecimal(stored)
	right, okRight := parseDecimal(next)
	if !okLeft || !okRight {
		return false
	}
	return roundHalfAway(left, purposeAdjustScale).Cmp(roundHalfAway(right, purposeAdjustScale)) == 0
}

func parseAdjustmentWindow(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errInvalidAdjustmentValue()
	}
	parsed = parsed.UTC().Truncate(time.Microsecond)
	latest := now.UTC().Add(purposeAdjustWindowDays * 24 * time.Hour)
	if parsed.Before(purposeAdjustWindowMin) || parsed.After(latest) {
		return time.Time{}, errInvalidAdjustmentValue()
	}
	return parsed, nil
}

func formatAdjustmentWindow(value time.Time) string {
	return value.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
}

func errInvalidAdjustmentValue() error {
	return infraerrors.BadRequest("INVALID_ADJUSTMENT_VALUE", "invalid adjustment value")
}

func logPurposeAdjustFailure(in PurposeAdjustInput, err error) {
	slog.Warn("usage_monitor_adjust_failed",
		"actor_user_id", in.ActorUserID,
		"target_user_id", in.UserID,
		"purpose", in.Purpose,
		"action", in.Action,
		"code", infraerrors.Code(err),
		"reason", infraerrors.Reason(err),
	)
}
