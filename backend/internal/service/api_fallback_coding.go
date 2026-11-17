package service

import "time"

// APIFallbackCodingCharge is a fixed, already priced charge. Its immutable
// pricing and supplier evidence live in the associated API fallback attempt.
// It never carries a subscription observation or participates in calibration.
type APIFallbackCodingCharge struct {
	RequestID   string
	APIKeyID    int64
	UserID      int64
	AccountID   int64
	Purpose     string
	Points      string
	StartedAt   time.Time
	CompletedAt time.Time
}

// APIFallbackAdmissionUsage is the current personal period and existing coding
// usage under the same person lock used by ordinary reservations. Reserved
// includes subscription reservations only; API attempts are added by their
// owning repository in that same transaction, never counted twice here.
type APIFallbackAdmissionUsage struct {
	Used        string
	Pending     string
	Reserved    string
	Uncertain   bool
	PeriodStart time.Time
	PeriodEnd   time.Time
}
