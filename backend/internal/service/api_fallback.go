package service

import (
	"context"
	"encoding/json"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// APIFallbackPolicy is an administrator-approved, exact-model standby policy.
// Prices are official USD per million tokens. Procurement and personal points
// deliberately use separate multipliers. No key material is stored here.
type APIFallbackPolicy struct {
	ID                         string                       `json:"id"`
	Version                    int64                        `json:"version"`
	GroupID                    int64                        `json:"group_id"`
	Platform                   string                       `json:"platform"`
	Model                      string                       `json:"model"`
	PrimaryAccountIDs          []int64                      `json:"primary_account_ids"`
	PrimaryEvidence            []APIFallbackPrimaryEvidence `json:"primary_evidence"`
	BackupAccountIDs           []int64                      `json:"backup_account_ids"`
	ExperimentKeyID            int64                        `json:"experiment_key_id,omitempty"`
	ExperimentGroupID          int64                        `json:"experiment_group_id,omitempty"`
	Enabled                    bool                         `json:"enabled"`
	CostCircuitOpen            bool                         `json:"cost_circuit_open"`
	CapabilitiesVerified       bool                         `json:"capabilities_verified"`
	ExpiresAt                  *time.Time                   `json:"expires_at"`
	PointsPerUSD               string                       `json:"points_per_usd"`
	SupplierMultiplier         string                       `json:"supplier_multiplier"`
	InputPricePerMillion       string                       `json:"input_price_per_million"`
	CachedInputPricePerMillion string                       `json:"cached_input_price_per_million"`
	OutputPricePerMillion      string                       `json:"output_price_per_million"`
	MaxInputTokens             int64                        `json:"max_input_tokens"`
	MaxOutputTokens            int64                        `json:"max_output_tokens"`
	PerRequestBudgetUSD        string                       `json:"per_request_budget_usd"`
	DailyBudgetUSD             string                       `json:"daily_budget_usd"`
	TotalBudgetUSD             string                       `json:"total_budget_usd"`
	Note                       string                       `json:"note"`
	ApprovedBy                 int64                        `json:"approved_by"`
	CreatedAt                  time.Time                    `json:"created_at"`
	UpdatedAt                  time.Time                    `json:"updated_at"`
}

// APIFallbackPrimaryEvidence is an administrator-confirmed, time-bounded reason.
// A current healthy subscription snapshot always overrides historical evidence.
type APIFallbackPrimaryEvidence struct {
	AccountID  int64     `json:"account_id"`
	Reason     string    `json:"reason"`
	ObservedAt time.Time `json:"observed_at"`
	ValidUntil time.Time `json:"valid_until"`
	Note       string    `json:"note"`
}

// APIFallbackPolicyInput requires explicit confirmation and optimistic versioning.
type APIFallbackPolicyInput struct {
	APIFallbackPolicy
	ExpectedVersion int64 `json:"expected_version"`
	Confirm         bool  `json:"confirm"`
	ActorUserID     int64 `json:"-"`
}

// APIFallbackAttempt retains costs of every real upstream attempt.
type APIFallbackAttempt struct {
	ID             string             `json:"id"`
	RequestID      string             `json:"request_id"`
	PolicyID       string             `json:"policy_id"`
	PolicyVersion  int64              `json:"policy_version"`
	UserID         int64              `json:"user_id"`
	APIKeyID       int64              `json:"api_key_id"`
	AccountID      int64              `json:"account_id"`
	Model          string             `json:"model"`
	Platform       string             `json:"platform"`
	State          string             `json:"state"`
	Reason         string             `json:"reason"`
	ReservedUSD    string             `json:"reserved_usd"`
	ReservedPoints string             `json:"reserved_points"`
	OfficialUSD    string             `json:"official_usd"`
	SupplierUSD    string             `json:"supplier_usd"`
	Points         string             `json:"points"`
	StartedAt      time.Time          `json:"started_at"`
	CompletedAt    *time.Time         `json:"completed_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
	Receipt        json.RawMessage    `json:"receipt,omitempty"`
	Events         []APIFallbackEvent `json:"events"`
}

// APIFallbackEvent is immutable administrator-visible transition evidence.
type APIFallbackEvent struct {
	Type        string    `json:"type"`
	ActorUserID *int64    `json:"actor_user_id"`
	Evidence    string    `json:"evidence"`
	CreatedAt   time.Time `json:"created_at"`
}

// APIFallbackSummary contains procurement costs, never user sale prices.
type APIFallbackSummary struct {
	SettledUSD      string `json:"settled_usd"`
	ReservedUSD     string `json:"reserved_usd"`
	UnresolvedUSD   string `json:"unresolved_usd"`
	TodayUSD        string `json:"today_usd"`
	SettledCount    int64  `json:"settled_count"`
	UnresolvedCount int64  `json:"unresolved_count"`
}

// APIFallbackView is exposed only through administrator APIs.
type APIFallbackView struct {
	Policies       []APIFallbackPolicy        `json:"policies"`
	Attempts       []APIFallbackAttempt       `json:"attempts"`
	Summary        APIFallbackSummary         `json:"summary"`
	ExperimentKeys []APIFallbackExperimentKey `json:"experiment_keys,omitempty"`
}

// APIFallbackExperimentKey exposes a selectable Key without its credential.
type APIFallbackExperimentKey struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	GroupName string `json:"group_name"`
	Platform  string `json:"platform"`
}

// APIFallbackPreview is a read-only decision; it never reserves money or calls a model.
type APIFallbackPreview struct {
	Eligible             bool               `json:"eligible"`
	Reason               string             `json:"reason"`
	Checks               []APIFallbackCheck `json:"checks"`
	RequestChecksPending bool               `json:"request_checks_pending"`
}

type APIFallbackCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

func (s *UsageMonitorService) SetAPIFallbackPreview(fn func(context.Context, *APIFallbackPolicy) (*APIFallbackPreview, error)) {
	s.apiFallbackPreview = fn
}

func (s *UsageMonitorService) PreviewAPIFallback(ctx context.Context, actor int64, id string) (*APIFallbackPreview, error) {
	view, err := s.APIFallback(ctx, actor)
	if err != nil {
		return nil, err
	}
	for i := range view.Policies {
		if view.Policies[i].ID == id {
			if s.apiFallbackPreview == nil {
				return nil, infraerrors.ServiceUnavailable("API_FALLBACK_UNAVAILABLE", "preview is unavailable")
			}
			preview, e := s.apiFallbackPreview(ctx, &view.Policies[i])
			if e != nil {
				return nil, e
			}
			if preview == nil {
				return nil, infraerrors.ServiceUnavailable("API_FALLBACK_UNAVAILABLE", "preview is unavailable")
			}
			return applyAPIFallbackPolicyPreview(preview, &view.Policies[i], time.Now().UTC()), nil
		}
	}
	return nil, infraerrors.NotFound("API_FALLBACK_NOT_FOUND", "standby policy not found")
}

func applyAPIFallbackPolicyPreview(preview *APIFallbackPreview, p *APIFallbackPolicy, now time.Time) *APIFallbackPreview {
	preview.RequestChecksPending = true
	expires := p.ActiveAt(now)
	preview.Checks = append(preview.Checks, APIFallbackCheck{Name: "policy_enabled", Passed: p.Enabled}, APIFallbackCheck{Name: "policy_validity", Passed: expires}, APIFallbackCheck{Name: "capabilities_verified", Passed: p.CapabilitiesVerified})
	preview.Checks = append(preview.Checks, APIFallbackCheck{Name: "cost_circuit_closed", Passed: !p.CostCircuitOpen})
	switch {
	case !p.Enabled:
		preview.Eligible = false
		preview.Reason = "policy_disabled"
	case !expires:
		preview.Eligible = false
		preview.Reason = "policy_expired"
	case !p.CapabilitiesVerified:
		preview.Eligible = false
		preview.Reason = "capabilities_unverified"
	case p.CostCircuitOpen:
		preview.Eligible = false
		preview.Reason = "cost_bound_exceeded"
	}
	return preview
}

// ActiveAt applies the optional expiration of experiment standby policies.
func (p *APIFallbackPolicy) ActiveAt(now time.Time) bool {
	return p != nil && p.Enabled && (p.ExpiresAt != nil && p.ExpiresAt.After(now) || p.ExpiresAt == nil && p.ExperimentKeyID > 0)
}

// APIFallbackReservationInput is supplied by the server after capability checks.
// EstimatedInputTokens must be a verified upper bound, not a typical token ratio.
type APIFallbackReservationInput struct {
	ID, RequestID, PolicyID                    string
	PolicyVersion, UserID, APIKeyID, AccountID int64
	GroupID                                    int64
	Platform, Model                            string
	EstimatedInputTokens, MaxOutputTokens      int64
	PersonalLimit                              string
	StartedAt, Now                             time.Time
}

// APIFallbackSettlementInput uses native Responses token counts. InputTokens
// includes CachedInputTokens. Usage is retained as original evidence.
type APIFallbackSettlementInput struct {
	ID                                           string
	InputTokens, CachedInputTokens, OutputTokens int64
	Usage                                        json.RawMessage
	Now                                          time.Time
}

// APIFallbackResolveInput appends explicit administrator reconciliation evidence.
// Only a known token receipt or confirmed zero-billed outcome may resolve unknowns.
type APIFallbackResolveInput struct {
	ActorUserID       int64  `json:"-"`
	ID                string `json:"-"`
	Confirm           bool   `json:"confirm"`
	Evidence          string `json:"evidence"`
	Outcome           string `json:"outcome"`
	InputTokens       int64  `json:"input_tokens"`
	CachedInputTokens int64  `json:"cached_input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
}

// APIFallbackStore preserves policy versions, uncertain spend and fixed charges.
type APIFallbackStore interface {
	ReadAPIFallback(context.Context, int64) (*APIFallbackView, error)
	SaveAPIFallbackPolicy(context.Context, APIFallbackPolicyInput) (*APIFallbackPolicy, error)
	DisableAPIFallbackPolicy(context.Context, string, int64, int64, string) (*APIFallbackPolicy, error)
	FindAPIFallbackPolicy(context.Context, int64, string, string) (*APIFallbackPolicy, error)
	ReserveAPIFallback(context.Context, APIFallbackReservationInput) (*APIFallbackAttempt, error)
	MarkAPIFallbackSent(context.Context, string) error
	SettleAPIFallback(context.Context, APIFallbackSettlementInput) (*APIFallbackAttempt, error)
	CancelAPIFallbackUnsent(context.Context, string, string) error
	MarkAPIFallbackUnknown(context.Context, string, string) error
	ResolveAPIFallback(context.Context, APIFallbackResolveInput) (*APIFallbackAttempt, error)
	ResolveAPIFallbackLineage(context.Context, int64, int64, int64, string, []string) (int64, error)
}

func (s *UsageMonitorService) apiFallbackStore() (APIFallbackStore, error) {
	if s != nil {
		if store, ok := s.store.(APIFallbackStore); ok {
			return store, nil
		}
	}
	return nil, infraerrors.ServiceUnavailable("API_FALLBACK_UNAVAILABLE", "API standby is unavailable")
}

func (s *UsageMonitorService) APIFallback(ctx context.Context, actor int64) (*APIFallbackView, error) {
	store, e := s.apiFallbackStore()
	if e != nil {
		return nil, e
	}
	return store.ReadAPIFallback(ctx, actor)
}
func (s *UsageMonitorService) SaveAPIFallbackPolicy(ctx context.Context, in APIFallbackPolicyInput) (*APIFallbackPolicy, error) {
	store, e := s.apiFallbackStore()
	if e != nil {
		return nil, e
	}
	return store.SaveAPIFallbackPolicy(ctx, in)
}
func (s *UsageMonitorService) DisableAPIFallbackPolicy(ctx context.Context, id string, version, actor int64, note string) (*APIFallbackPolicy, error) {
	store, e := s.apiFallbackStore()
	if e != nil {
		return nil, e
	}
	return store.DisableAPIFallbackPolicy(ctx, id, version, actor, note)
}
func (s *UsageMonitorService) FindAPIFallbackPolicy(ctx context.Context, group int64, platform, model string) (*APIFallbackPolicy, error) {
	store, e := s.apiFallbackStore()
	if e != nil {
		return nil, e
	}
	return store.FindAPIFallbackPolicy(ctx, group, platform, model)
}
func (s *UsageMonitorService) ReserveAPIFallback(ctx context.Context, in APIFallbackReservationInput) (*APIFallbackAttempt, error) {
	store, e := s.apiFallbackStore()
	if e != nil {
		return nil, e
	}
	return store.ReserveAPIFallback(ctx, in)
}
func (s *UsageMonitorService) MarkAPIFallbackSent(ctx context.Context, id string) error {
	store, e := s.apiFallbackStore()
	if e != nil {
		return e
	}
	return store.MarkAPIFallbackSent(ctx, id)
}
func (s *UsageMonitorService) SettleAPIFallback(ctx context.Context, in APIFallbackSettlementInput) (*APIFallbackAttempt, error) {
	store, e := s.apiFallbackStore()
	if e != nil {
		return nil, e
	}
	return store.SettleAPIFallback(ctx, in)
}
func (s *UsageMonitorService) CancelAPIFallbackUnsent(ctx context.Context, id, reason string) error {
	store, e := s.apiFallbackStore()
	if e != nil {
		return e
	}
	return store.CancelAPIFallbackUnsent(ctx, id, reason)
}
func (s *UsageMonitorService) MarkAPIFallbackUnknown(ctx context.Context, id, reason string) error {
	store, e := s.apiFallbackStore()
	if e != nil {
		return e
	}
	return store.MarkAPIFallbackUnknown(ctx, id, reason)
}
func (s *UsageMonitorService) ResolveAPIFallback(ctx context.Context, in APIFallbackResolveInput) (*APIFallbackAttempt, error) {
	store, e := s.apiFallbackStore()
	if e != nil {
		return nil, e
	}
	return store.ResolveAPIFallback(ctx, in)
}

// ResolveAPIFallbackLineage identifies only prior receipts for this exact user,
// key, group and model. It returns zero if all hashes cannot share one account.
func (s *UsageMonitorService) ResolveAPIFallbackLineage(ctx context.Context, user, key, group int64, model string, hashes []string) (int64, error) {
	store, e := s.apiFallbackStore()
	if e != nil {
		return 0, e
	}
	return store.ResolveAPIFallbackLineage(ctx, user, key, group, model, hashes)
}
