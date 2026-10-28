package service

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codingRuntimeStore struct {
	OfficialPointStore
	CodingBillingStore
	mu       sync.Mutex
	inputs   []CodingReservationInput
	finishes []bool
	sent     int
}

func (s *codingRuntimeStore) ReserveCodingQuota(_ context.Context, in CodingReservationInput) (CodingReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs = append(s.inputs, in)
	return CodingReservation{ID: in.ID, Mode: in.Mode, StartedAt: in.StartedAt}, nil
}
func (s *codingRuntimeStore) MarkCodingReservationSent(context.Context, string, time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent++
	return nil
}
func (s *codingRuntimeStore) FinishCodingReservation(_ context.Context, _ string, noUsage bool, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishes = append(s.finishes, noUsage)
	return nil
}
func (s *codingRuntimeStore) RenewCodingReservation(context.Context, string, time.Time) error {
	return nil
}

func TestCodingReservationRuntimeUnsentReleaseAndUnknownSent(t *testing.T) {
	store := &codingRuntimeStore{}
	points := NewOfficialPointService(nil, store, nil)
	for _, sent := range []bool{false, true} {
		h, err := points.BeginCodingReservation(context.Background(), CodingReservationInput{UserID: 1, APIKeyID: 2, AccountID: 10, Purpose: "openai", Model: "gpt-5", StartedAt: time.Now()})
		require.NoError(t, err)
		require.NotNil(t, h)
		require.Same(t, h, CodingReservationFromContext(WithCodingReservation(context.Background(), h)))
		if sent {
			require.NoError(t, h.MarkSent(context.Background()))
		}
		h.Finish()
		h.Finish()
	}
	require.Equal(t, []bool{true, false}, store.finishes)
	require.Len(t, store.inputs, 2)
	require.NotEqual(t, store.inputs[0].ID, store.inputs[1].ID)
	require.Equal(t, "shadow", store.inputs[0].Mode)
	require.Equal(t, "50", store.inputs[0].Limit)
}

func TestCodingReservationReplayContextPreservesInternalHandle(t *testing.T) {
	h := &CodingReservationHandle{}
	ctx := WithCodingReservation(context.Background(), h)
	require.Same(t, h, CodingReservationFromContext(context.WithoutCancel(ctx)))
}

func TestCodingReservationHTTPRetriesShareIdentity(t *testing.T) {
	store := &codingRuntimeStore{}
	points := NewOfficialPointService(nil, store, nil)
	svc := &OpenAIGatewayService{codingPoints: points}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("api_key", &APIKey{ID: 2, UserID: 1, Group: &Group{Platform: PlatformOpenAI, UsageKind: UsageKindCoding}})
	body := []byte(`{"model":"gpt-5","input":"hi","max_output_tokens":64}`)
	ctx, err := svc.PrepareCodingRequest(c.Request.Context(), c, &Account{ID: 10}, body)
	require.NoError(t, err)
	first := CodingReservationFromContext(ctx)
	require.NotNil(t, first)
	ctx, err = svc.PrepareCodingRequest(ctx, c, &Account{ID: 11}, body)
	require.NoError(t, err)
	require.Same(t, first, CodingReservationFromContext(ctx))
	require.Len(t, store.inputs, 2)
	require.Equal(t, store.inputs[0].ID, store.inputs[1].ID)
	require.Equal(t, int64(11), store.inputs[1].AccountID)
	FinishCodingRequest(c)
	require.Equal(t, []bool{true}, store.finishes)
}

func TestCodingReservationWSTurnsIndependentAndIdleDoesNotHold(t *testing.T) {
	store := &codingRuntimeStore{}
	svc := &OpenAIGatewayService{codingPoints: NewOfficialPointService(nil, store, nil)}
	key := &APIKey{ID: 2, UserID: 1, Group: &Group{Platform: PlatformOpenAI, UsageKind: UsageKindCoding}}
	var got []*CodingReservationHandle
	hooks, cleanup, err := svc.codingWSTurnHooks(context.Background(), key, &Account{ID: 10}, []byte(`{"model":"gpt-5"}`), &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, _ error) { got = append(got, r.CodingReservation) }})
	require.NoError(t, err)
	defer cleanup()
	require.Len(t, store.inputs, 1)
	hooks.AfterTurn(1, &OpenAIForwardResult{}, nil)
	require.Len(t, store.finishes, 1)
	require.NoError(t, hooks.BeforeRequest(2, []byte(`{"model":"gpt-5"}`), "gpt-5"))
	hooks.AfterTurn(2, &OpenAIForwardResult{}, nil)
	require.Len(t, store.inputs, 2)
	require.Len(t, store.finishes, 2)
	require.NotEqual(t, got[0].ID, got[1].ID)
}

func TestCodingReservationEnforceEstimatedCapacityIsNotConfirmedExhaustion(t *testing.T) {
	t.Setenv("SUB2API_CODING_QUOTA_OPENAI_MODE", "enforce")
	card := codingQuotaResult(CodingQuotaReading{Purpose: PurposeOpenAI, Used: "2", Pending: "60", PendingCount: 1}, "50")
	require.False(t, card.Blocked)
	require.NotNil(t, card.Percent)
	require.Equal(t, 100, *card.Percent)
	card = codingQuotaResult(CodingQuotaReading{Purpose: PurposeOpenAI, Used: "50", Pending: "0"}, "50")
	require.True(t, card.Blocked)
}
