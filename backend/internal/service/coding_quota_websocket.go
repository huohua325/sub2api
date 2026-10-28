package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	coderws "github.com/coder/websocket"
)

// Each turn owns its own handle. AfterTurn can overlap the next BeforeRequest;
// never store a turn's billing identity on the shared session context.
func (s *OpenAIGatewayService) codingWSTurnHooks(ctx context.Context, key *APIKey, account *Account, first []byte, original *OpenAIWSIngressHooks) (*OpenAIWSIngressHooks, func(), error) {
	noop := func() {}
	if s.codingPoints == nil || key == nil || key.Group == nil {
		return original, noop, nil
	}
	purpose := PurposeOfGroup(key.Group.Platform, key.Group.UsageKind)
	if (purpose != PurposeOpenAI && purpose != PurposeGrok) || CodingControlMode(purpose) == "off" {
		return original, noop, nil
	}
	hooks := OpenAIWSIngressHooks{}
	if original != nil {
		hooks = *original
	}
	var mu sync.Mutex
	turns := map[int]*CodingReservationHandle{}
	begin := func(turn int, body []byte) error {
		mu.Lock()
		defer mu.Unlock()
		if turns[turn] != nil {
			return nil
		}
		model, weight := codingRawEstimate(s.billingService, account, body)
		parent := account.ID
		if account.ParentAccountID != nil {
			parent = *account.ParentAccountID
		}
		h, err := s.codingPoints.BeginCodingReservation(ctx, CodingReservationInput{UserID: key.UserID, APIKeyID: key.ID, AccountID: parent, Purpose: purpose, Model: model, Weight: weight, Mode: CodingControlMode(purpose), StartedAt: time.Now().UTC()})
		if err != nil {
			if CodingControlMode(purpose) == "shadow" {
				slog.Error("coding_ws_shadow_failed", "error", err)
				return nil
			}
			return NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, err.Error(), err)
		}
		turns[turn] = h
		// Admission is immediately before the proxy executes this turn. A lost
		// result remains unresolved, including a failed/ambiguous network write.
		if err = h.MarkSent(ctx); err != nil {
			h.Finish()
			return err
		}
		return nil
	}
	cleanup := func() {
		mu.Lock()
		defer mu.Unlock()
		for _, h := range turns {
			h.Finish()
		}
		clear(turns)
	}
	hooks.BeforeRequest = func(turn int, body []byte, model string) error {
		if original != nil && original.BeforeRequest != nil {
			if err := original.BeforeRequest(turn, body, model); err != nil {
				return err
			}
		}
		return begin(turn, body)
	}
	hooks.AfterTurn = func(turn int, result *OpenAIForwardResult, err error) {
		mu.Lock()
		h := turns[turn]
		delete(turns, turn)
		mu.Unlock()
		defer h.Finish()
		if result != nil {
			result.CodingReservation = h
		}
		if original != nil && original.AfterTurn != nil {
			original.AfterTurn(turn, result, err)
		}
	}
	if err := begin(1, first); err != nil {
		cleanup()
		return nil, noop, err
	}
	return &hooks, cleanup, nil
}

func codingWSAdmissionErrorFrame(err error) []byte {
	code := ""
	switch {
	case errors.Is(err, ErrCodingQuotaBusy):
		code = "CODING_QUOTA_BUSY"
	case errors.Is(err, ErrCodingQuotaExhausted):
		code = "PURPOSE_QUOTA_EXHAUSTED"
	case errors.Is(err, ErrCodingQuotaUnavailable):
		code = "CODING_QUOTA_UNAVAILABLE"
	}
	if code == "" {
		return nil
	}
	frame, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]any{"type": code, "code": code, "message": "Quota capacity unavailable for this turn; retry later"}})
	return frame
}
