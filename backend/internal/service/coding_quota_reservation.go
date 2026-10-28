package service

import (
	"context"
	"errors"
	"time"
)

var (
	ErrCodingQuotaBusy        = errors.New("CODING_QUOTA_BUSY")
	ErrCodingQuotaExhausted   = errors.New("PURPOSE_QUOTA_EXHAUSTED")
	ErrCodingQuotaUnavailable = errors.New("CODING_QUOTA_UNAVAILABLE")
)

// CodingReservationInput is server-owned admission evidence; Weight excludes user selling multipliers.
type CodingReservationInput struct {
	ID                                  string
	UserID, APIKeyID, AccountID         int64
	Purpose, Model, Mode, Limit, Weight string
	StartedAt, Now                      time.Time
	MinSamples                          int
	MaxAge                              time.Duration
}

// CodingReservation exposes safe diagnostics, never request bodies or keys.
type CodingReservation struct {
	Billing         []CodingBillingDiagnostic `json:"billing,omitempty"`
	Events          []CodingReservationEvent  `json:"events,omitempty"`
	ID              string                    `json:"id"`
	UserID          int64                     `json:"user_id"`
	APIKeyID        int64                     `json:"-"`
	AccountID       int64                     `json:"account_id"`
	Purpose         string                    `json:"purpose"`
	Model           string                    `json:"model"`
	Mode            string                    `json:"mode"`
	State           string                    `json:"state"`
	AdmissionMode   string                    `json:"admission_mode"`
	Reason          string                    `json:"reason"`
	EstimatedPoints string                    `json:"estimated_points"`
	Rate            string                    `json:"rate"`
	SampleCount     int                       `json:"sample_count"`
	Reliable        bool                      `json:"reliable"`
	WouldAllow      bool                      `json:"would_allow"`
	StartedAt       time.Time                 `json:"started_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
	ObservedAt      *time.Time                `json:"observed_at"`
	WindowEnd       *time.Time                `json:"window_end"`
}

type CodingBillingDiagnostic struct {
	RequestID   string     `json:"request_id"`
	State       string     `json:"state"`
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"last_error"`
	LedgerID    *int64     `json:"ledger_id"`
	LedgerState string     `json:"ledger_state"`
	Points      string     `json:"points"`
	Correction  string     `json:"correction"`
	AuditEvent  string     `json:"audit_event"`
	StartedAt   *time.Time `json:"started_at"`
}

type CodingReservationEvent struct {
	State     string    `json:"state"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// CodingReservationQuery defines bounded administrator-only cursor pagination.
type CodingReservationQuery struct {
	UserID         int64
	Purpose, State string
	Before         string
	Limit          int
}
type CodingReservationPage struct {
	Items      []CodingReservation `json:"items"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

// CodingReservationStore coordinates reservations across processes using PostgreSQL.
type CodingReservationStore interface {
	ReserveCodingQuota(context.Context, CodingReservationInput) (CodingReservation, error)
	MarkCodingReservationSent(context.Context, string, time.Time) error
	RenewCodingReservation(context.Context, string, time.Time) error
	FinishCodingReservation(context.Context, string, bool, time.Time) error
	RecoverCodingReservations(context.Context, time.Time) error
	ListCodingReservations(context.Context, CodingReservationQuery) (CodingReservationPage, error)
}

// CodingBillingStore durably hands off normalized billing commands before execution.
type CodingBillingStore interface {
	CodingReservationStore
	EnqueueCodingBilling(context.Context, string, *UsageBillingCommand) error
	ReplayCodingBilling(context.Context, time.Time, int) (int, error)
}

type CodingAsyncStore interface {
	StoreCodingAsyncJob(context.Context, string, int64, int64, GrokVideoPendingBilling) error
	LoadCodingAsyncJob(context.Context, string, int64, int64) (*GrokVideoPendingBilling, error)
}
