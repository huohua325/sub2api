package service

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrExperimentAccountingPending bounds unverified billing exposure.
	ErrExperimentAccountingPending = errors.New("experiment accounting has unresolved charges")
	// ErrExperimentRequestInFlight limits active execution across a user's keys.
	ErrExperimentRequestInFlight = errors.New("experiment execution capacity is busy")
	// ErrExperimentQuotaExhausted preserves the existing personal allowance limit.
	ErrExperimentQuotaExhausted = errors.New("experiment quota exhausted")
	// ErrExperimentAccountingUnavailable fails closed when durable admission cannot be checked.
	ErrExperimentAccountingUnavailable = errors.New("experiment accounting unavailable")
)

// ExperimentAdmissionInput identifies a request before any chargeable upstream send.
// It does not invent an estimated cost; actual charges come from supplier logs.
type ExperimentAdmissionInput struct {
	RequestID string
	UserID    int64
	APIKeyID  int64
	AccountID int64
	StartedAt time.Time
}

// ExperimentAdmissionStore coordinates durable admission across keys and processes.
type ExperimentAdmissionStore interface {
	ReserveExperimentRequest(context.Context, ExperimentAdmissionInput) error
	MarkExperimentRequestSent(context.Context, string, int64) error
	ReleaseExperimentRequest(context.Context, string, int64, string) error
}

// ExperimentCompletionStore separates finished execution from unknown billing.
type ExperimentCompletionStore interface {
	CompleteExperimentRequest(context.Context, string, int64) error
}

// ExperimentChargeEvidence identifies the exact upstream evidence used for a settlement.
// SHA256 describes the retained evidence artifact or canonical consume-log row.
type ExperimentChargeEvidence struct {
	Source            string
	SHA256            string
	UpstreamRequestID string
	AccountID         int64
}

// ExperimentEvidenceReconciler settles an existing charge exactly once with an audit record.
type ExperimentEvidenceReconciler interface {
	CompleteExperimentChargeWithEvidence(context.Context, int64, float64, ExperimentChargeEvidence) error
}

// ExperimentRecentChargeMatcher matches one cached supplier page in one database query.
type ExperimentRecentChargeMatcher interface {
	MatchPendingExperimentCharges(context.Context, int64, []string) ([]PendingExperimentCharge, error)
}

// ExperimentResponseRecorder retains receipt identity even when no usage can be billed.
type ExperimentResponseRecorder interface {
	RecordExperimentResponse(context.Context, string, int64, string, int) error
}

// ExperimentAccountingStatus describes unknown costs separately from settled usage.
// Counts are disjoint and include liabilities from earlier periods. A finished
// request awaiting its supplier bill does not occupy execution capacity.
type ExperimentAccountingStatus struct {
	PendingRequests    int64
	UnresolvedRequests int64
	InFlightRequests   int64
	UpdatedAt          *time.Time
	OldestPendingAt    *time.Time
	ActiveLimit        int64
	OutstandingLimit   int64
	AccountingBlocked  bool
}

// ExperimentAccountingStatusReader supplies a durable summary for user-facing quota cards.
type ExperimentAccountingStatusReader interface {
	ReadExperimentAccountingStatus(context.Context, int64, time.Time) (ExperimentAccountingStatus, error)
}

// ExperimentUnsentRecoveryStore releases only stale reservations that never
// obtained the durable send checkpoint. Sent or unknown work is never expired.
type ExperimentUnsentRecoveryStore interface {
	RecoverExperimentUnsentRequests(context.Context, time.Time) (int64, error)
}
