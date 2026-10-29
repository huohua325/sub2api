package service

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestdrain"
	"log/slog"
	"sync"
	"time"
)

// Native voice reports one aggregate duration bill for the session. Reserve
// every active audio turn independently, and correlate them to that ONE bill.
type codingRealtimeSession struct {
	mu        sync.Mutex
	points    *OfficialPointService
	key       *APIKey
	account   *Account
	model     string
	active    *CodingReservationHandle
	holds     []*CodingReservationHandle
	closed    bool
	executing bool
	endRound  func()
}

func newCodingRealtimeSession(points *OfficialPointService, key *APIKey, account *Account, model string) *codingRealtimeSession {
	return &codingRealtimeSession{points: points, key: key, account: account, model: model}
}

func (s *codingRealtimeSession) Begin(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("realtime session closed")
	}
	if s.active != nil || s.endRound != nil {
		return nil
	}
	endRound, err := requestdrain.BeginRound(ctx)
	if err != nil {
		return err
	}
	s.endRound = endRound
	if s.points == nil || s.key == nil || s.key.Group == nil {
		return nil
	}
	purpose := PurposeOfGroup(s.key.Group.Platform, s.key.Group.UsageKind)
	if purpose != PurposeGrok || CodingControlMode(purpose) == "off" {
		return nil
	}
	parent := s.account.ID
	if s.account.ParentAccountID != nil {
		parent = *s.account.ParentAccountID
	}
	h, err := s.points.BeginCodingReservation(ctx, CodingReservationInput{UserID: s.key.UserID, APIKeyID: s.key.ID, AccountID: parent, Purpose: purpose, Model: s.model, Mode: CodingControlMode(purpose), StartedAt: time.Now().UTC()})
	if err != nil {
		if CodingControlMode(purpose) == "shadow" {
			slog.Error("coding_realtime_shadow_failed", "error", err)
			return nil
		}
		s.endRound()
		s.endRound = nil
		return err
	}
	s.holds = append(s.holds, h)
	s.active = h
	if err := h.MarkSent(ctx); err != nil {
		h.Finish()
		s.active = nil
		s.endRound()
		s.endRound = nil
		return err
	}
	return nil
}

func (s *codingRealtimeSession) End() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active.Finish()
	s.active = nil
	s.executing = false
	if s.endRound != nil {
		s.endRound()
		s.endRound = nil
	}
}

func (s *codingRealtimeSession) Close() *CodingReservationHandle {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, h := range s.holds {
		h.Finish()
	}
	if s.endRound != nil {
		s.endRound()
		s.endRound = nil
	}
	if len(s.holds) == 0 {
		return nil
	}
	primary := s.holds[0]
	primary.mu.Lock()
	defer primary.mu.Unlock()
	primary.companions = nil
	for _, h := range s.holds[1:] {
		primary.companions = append(primary.companions, h.ID)
	}
	return primary
}

func (s *OpenAIGatewayService) BindGrokRealtimeCoding(upstream *GrokRealtimeUpstream, key *APIKey, account *Account, model string) {
	if upstream != nil {
		upstream.coding = newCodingRealtimeSession(s.codingPoints, key, account, model)
	}
}

func (u *GrokRealtimeUpstream) CodingBillingHandle() *CodingReservationHandle {
	if u == nil {
		return nil
	}
	return u.coding.Close()
}

// BeginEvent distinguishes input chunks from an explicit additional response.
func (s *codingRealtimeSession) BeginEvent(ctx context.Context, event string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	overlap := event == "response.create" && s.active != nil && s.executing
	s.mu.Unlock()
	if overlap && CodingControlMode(PurposeGrok) == "enforce" {
		return ErrCodingQuotaBusy
	}
	if err := s.Begin(ctx); err != nil {
		return err
	}
	if event == "response.create" {
		s.MarkExecuting()
	}
	return nil
}
func (s *codingRealtimeSession) MarkExecuting() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.executing = true
	s.mu.Unlock()
}
