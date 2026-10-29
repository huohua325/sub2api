package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

type codingReservationContextKey struct{}

// CodingReservationHandle belongs to a logical request, including internal retries.
// Finishing a sent request without a durable bill preserves an unresolved record.
type CodingReservationHandle struct {
	ID         string
	mode       string
	store      CodingReservationStore
	mu         sync.Mutex
	sent       bool
	closed     bool
	stop       chan struct{}
	companions []string
}

func WithCodingReservation(ctx context.Context, h *CodingReservationHandle) context.Context {
	return context.WithValue(ctx, codingReservationContextKey{}, h)
}

func CodingReservationFromContext(ctx context.Context) *CodingReservationHandle {
	if ctx == nil {
		return nil
	}
	h, _ := ctx.Value(codingReservationContextKey{}).(*CodingReservationHandle)
	return h
}

func (s *OfficialPointService) BeginCodingReservation(ctx context.Context, in CodingReservationInput) (*CodingReservationHandle, error) {
	store, ok := s.store.(CodingReservationStore)
	if !ok {
		return nil, fmt.Errorf("%w: reservation store missing", ErrCodingQuotaUnavailable)
	}
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	if in.Mode == "" {
		in.Mode = "shadow"
	}
	if in.Now.IsZero() {
		in.Now = time.Now().UTC()
	}
	if in.StartedAt.IsZero() {
		in.StartedAt = in.Now
	}
	minSamples, maxAge := CodingCalibrationLimits()
	if in.MinSamples <= 0 {
		in.MinSamples = minSamples
	}
	if in.MaxAge <= 0 {
		in.MaxAge = maxAge
	}
	if in.Limit == "" {
		in.Limit = s.Budget(ctx).ForPurpose(in.Purpose)
	}
	if _, err := store.ReserveCodingQuota(ctx, in); err != nil {
		return nil, err
	}
	h := &CodingReservationHandle{ID: in.ID, mode: in.Mode, store: store, stop: make(chan struct{})}
	go h.heartbeat()
	return h, nil
}

func (h *CodingReservationHandle) heartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := h.store.RenewCodingReservation(ctx, h.ID, time.Now().UTC())
			cancel()
			if err != nil {
				slog.Error("coding_reservation_heartbeat_failed", "reservation", h.ID, "error", err)
			}
		}
	}
}

func (h *CodingReservationHandle) MarkSent(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("%w: reservation closed", ErrCodingQuotaUnavailable)
	}
	if err := h.store.MarkCodingReservationSent(ctx, h.ID, time.Now().UTC()); err != nil {
		if h.mode == "shadow" {
			h.sent = true
			slog.Error("coding_reservation_shadow_send_record_failed", "reservation", h.ID, "error", err)
			return nil
		}
		return fmt.Errorf("%w: %v", ErrCodingQuotaUnavailable, err)
	}
	h.sent = true
	return nil
}

func (h *CodingReservationHandle) Finish() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.stop)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.store.FinishCodingReservation(ctx, h.ID, !h.sent, time.Now().UTC()); err != nil {
		slog.Error("coding_reservation_finish_failed", "reservation", h.ID, "error", err)
	}
}

// Handoff persists the exact normalized command before an immediate apply attempt.
func (h *CodingReservationHandle) Handoff(ctx context.Context, cmd *UsageBillingCommand) error {
	if h == nil {
		return nil
	}
	store, ok := h.store.(CodingBillingStore)
	if !ok {
		return fmt.Errorf("%w: billing handoff store missing", ErrCodingQuotaUnavailable)
	}
	cmd.CodingReservationID = h.ID
	h.mu.Lock()
	cmd.CodingReservationIDs = append([]string(nil), h.companions...)
	h.mu.Unlock()
	return store.EnqueueCodingBilling(ctx, h.ID, cmd)
}

// ResumeCodingBilling only restores internal correlation from durable media
// evidence; EnqueueCodingBilling revalidates ownership before capture.
func (s *OpenAIGatewayService) ResumeCodingBilling(id string) *CodingReservationHandle {
	if s.codingPoints == nil || id == "" {
		return nil
	}
	store, ok := s.codingPoints.store.(CodingReservationStore)
	if !ok {
		return nil
	}
	return &CodingReservationHandle{ID: id, store: store, sent: true, stop: make(chan struct{})}
}
