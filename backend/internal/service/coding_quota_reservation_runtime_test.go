package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codingRuntimeStore struct {
	OfficialPointStore
	CodingBillingStore
	mu           sync.Mutex
	inputs       []CodingReservationInput
	finishes     []bool
	sent         int
	markError    error
	reserveError error
}

func (s *codingRuntimeStore) ReserveCodingQuota(_ context.Context, in CodingReservationInput) (CodingReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs = append(s.inputs, in)
	if s.reserveError != nil {
		return CodingReservation{}, s.reserveError
	}
	return CodingReservation{ID: in.ID, Mode: in.Mode, StartedAt: in.StartedAt}, nil
}
func (s *codingRuntimeStore) MarkCodingReservationSent(context.Context, string, time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent++
	return s.markError
}

func TestCodingReservationShadowSendFailureKeepsExistingAdmission(t *testing.T) {
	store := &codingRuntimeStore{markError: context.DeadlineExceeded}
	points := NewOfficialPointService(nil, store, nil)
	h, err := points.BeginCodingReservation(context.Background(), CodingReservationInput{UserID: 1, APIKeyID: 1, AccountID: 10, Purpose: "openai", Mode: "shadow"})
	require.NoError(t, err)
	defer h.Finish()
	require.NoError(t, h.MarkSent(context.Background()))
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

func TestCodingReservationRealtimeIdleAndPerTurn(t *testing.T) {
	store := &codingRuntimeStore{}
	points := NewOfficialPointService(nil, store, nil)
	session := newCodingRealtimeSession(points, &APIKey{ID: 1, UserID: 1, Group: &Group{Platform: PlatformGrok, UsageKind: UsageKindCoding}}, &Account{ID: 10}, "grok-voice-latest")
	require.Empty(t, store.inputs)
	require.NoError(t, session.Begin(context.Background()))
	require.NoError(t, session.Begin(context.Background()))
	require.Len(t, store.inputs, 1)
	session.End()
	require.Len(t, store.finishes, 1)
	require.NoError(t, session.Begin(context.Background()))
	require.Len(t, store.inputs, 2)
	billing := session.Close()
	require.NotNil(t, billing)
	require.Len(t, billing.companions, 1)
	require.Len(t, store.finishes, 2)
}

func TestCodingReservationMediaAndEmbeddingsCannotBypassAdmission(t *testing.T) {
	t.Setenv("SUB2API_CODING_QUOTA_OPENAI_MODE", "enforce")
	for _, endpoint := range []string{"images", "embeddings", "alpha-search"} {
		t.Run(endpoint, func(t *testing.T) {
			store := &codingRuntimeStore{reserveError: ErrCodingQuotaBusy}
			svc := &OpenAIGatewayService{codingPoints: NewOfficialPointService(nil, store, nil)}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/v1/"+endpoint, nil)
			c.Set("api_key", &APIKey{ID: 1, UserID: 1, Group: &Group{Platform: PlatformOpenAI, UsageKind: UsageKindCoding}})
			a := &Account{ID: 10, Type: AccountTypeAPIKey}
			var err error
			switch endpoint {
			case "images":
				_, err = svc.ForwardImages(c.Request.Context(), c, a, []byte(`{"model":"gpt-image-1"}`), &OpenAIImagesRequest{Model: "gpt-image-1"}, "")
			case "alpha-search":
				_, err = svc.ForwardAlphaSearch(c.Request.Context(), c, a, []byte(`{"model":"gpt-5"}`))
			default:
				_, err = svc.ForwardEmbeddings(c.Request.Context(), c, a, []byte(`{"model":"text-embedding-3-small"}`), "")
			}
			require.ErrorIs(t, err, ErrCodingQuotaBusy)
			require.Equal(t, 429, w.Code)
			require.Equal(t, "5", w.Header().Get("Retry-After"))
		})
	}
}

func TestCodingReservationWSBusyRejectsFrameWithoutClosingConnection(t *testing.T) {
	inner := &fakePassthroughFrameConn{reads: [][]byte{[]byte(`{"type":"response.create","id":"busy"}`), []byte(`{"type":"response.create","id":"allowed"}`)}}
	count := 0
	conn := &openAIWSPolicyEnforcingFrameConn{inner: inner, filter: func(_ coderws.MessageType, p []byte) ([]byte, *OpenAIFastBlockedError, error) {
		count++
		if count == 1 {
			return p, nil, ErrCodingQuotaBusy
		}
		return p, nil, nil
	}}
	_, payload, err := conn.ReadFrame(context.Background())
	require.NoError(t, err)
	require.Contains(t, string(payload), "allowed")
	require.False(t, inner.closeOnce)
	require.Len(t, inner.writes, 1)
	require.Contains(t, string(inner.writes[0]), "CODING_QUOTA_BUSY")
}

func TestCodingReservationShadowActivityDoesNotClaimBusy(t *testing.T) {
	s := CodingReservationSummary{Mode: "shadow", AdmissionMode: "single", Active: 1}
	require.Equal(t, "processing", s.ActivityStatus())
	s.Mode = "enforce"
	require.Equal(t, "busy", s.ActivityStatus())
}

func TestCodingReservationRealtimeRejectsOverlappingExplicitTurns(t *testing.T) {
	t.Setenv("SUB2API_CODING_QUOTA_GROK_MODE", "enforce")
	store := &codingRuntimeStore{}
	session := newCodingRealtimeSession(NewOfficialPointService(nil, store, nil), &APIKey{ID: 1, UserID: 1, Group: &Group{Platform: PlatformGrok, UsageKind: UsageKindCoding}}, &Account{ID: 10}, "grok-voice-latest")
	defer session.Close()
	require.NoError(t, session.BeginEvent(context.Background(), "input_audio_buffer.append"))
	require.NoError(t, session.BeginEvent(context.Background(), "response.create"))
	require.ErrorIs(t, session.BeginEvent(context.Background(), "response.create"), ErrCodingQuotaBusy)
	session.End()
	require.NoError(t, session.BeginEvent(context.Background(), "response.create"))
	require.Len(t, store.inputs, 2)
}

func TestCodingReservationForgedUpgradeHeaderCannotBypass(t *testing.T) {
	t.Setenv("SUB2API_CODING_QUOTA_OPENAI_MODE", "enforce")
	store := &codingRuntimeStore{reserveError: ErrCodingQuotaBusy}
	svc := &OpenAIGatewayService{codingPoints: NewOfficialPointService(nil, store, nil)}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Header.Set("Upgrade", "websocket")
	c.Set("api_key", &APIKey{ID: 1, UserID: 1, Group: &Group{Platform: PlatformOpenAI, UsageKind: UsageKindCoding}})
	_, err := svc.PrepareCodingRequest(c.Request.Context(), c, &Account{ID: 10}, []byte(`{"model":"gpt-5"}`))
	require.ErrorIs(t, err, ErrCodingQuotaBusy)
}
func TestCodingReservationCodexStrippedCapIsUnknown(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	billing.fallbackPrices["gpt-5"] = &ModelPricing{InputPricePerToken: 1e-6, OutputPricePerToken: 2e-6}
	body := []byte(`{"model":"gpt-5","max_output_tokens":1}`)
	_, known := codingRawEstimate(billing, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, body)
	require.NotEmpty(t, known, "control must be priceable")
	_, unknown := codingRawEstimate(billing, &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, body)
	require.Empty(t, unknown, "Codex deletes output caps before sending")
}
func TestCodingReservationVoiceRequestCarriesHandleToActualTransport(t *testing.T) {
	store := &codingRuntimeStore{}
	up := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"audio/wav"}}, Body: io.NopCloser(strings.NewReader("audio"))}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, codingPoints: NewOfficialPointService(nil, store, nil), httpUpstream: up}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/tts", nil)
	c.Set("api_key", &APIKey{ID: 1, UserID: 1, Group: &Group{Platform: PlatformGrok, UsageKind: UsageKindCoding}})
	defer FinishCodingRequest(c)
	_, _ = svc.ForwardGrokVoice(c.Request.Context(), c, &Account{ID: 10, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}}, "tts", []byte(`{"text":"hi"}`), "application/json")
	require.NotNil(t, up.lastReq)
	require.NotNil(t, CodingReservationFromContext(up.lastReq.Context()))
}

type codingVideoClaimStore struct {
	codingRuntimeStore
	handed bool
}

func (s *codingVideoClaimStore) StoreCodingAsyncJob(context.Context, string, int64, int64, GrokVideoPendingBilling) error {
	return nil
}
func (s *codingVideoClaimStore) LoadCodingAsyncJob(context.Context, string, int64, int64) (*GrokVideoPendingBilling, error) {
	return &GrokVideoPendingBilling{CodingReservationID: "durable-video"}, nil
}
func (s *codingVideoClaimStore) CodingAsyncJobHasBilling(context.Context, string, int64, int64) (bool, error) {
	return s.handed, nil
}

type codingStaleVideoClaim struct{ GatewayCache }

func (codingStaleVideoClaim) ClaimGrokVideoBilled(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}
func TestCodingReservationVideoStaleRedisClaimCannotHideMissingHandoff(t *testing.T) {
	store := &codingVideoClaimStore{}
	svc := &OpenAIGatewayService{codingPoints: NewOfficialPointService(nil, store, nil), cache: codingStaleVideoClaim{}}
	claimed, err := svc.ClaimGrokVideoBilling(context.Background(), "job", 1, 1)
	require.NoError(t, err)
	require.True(t, claimed, "a stale Redis claim cannot suppress a missing durable bill")
	store.handed = true
	claimed, err = svc.ClaimGrokVideoBilling(context.Background(), "job", 1, 1)
	require.NoError(t, err)
	require.False(t, claimed, "existing durable command remains authoritative")
}

func TestCodingReservationSendStorageFailureIs503WithoutFailover(t *testing.T) {
	for _, kind := range []string{"openai", "generic"} {
		t.Run(kind, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			account := &Account{ID: 10, Platform: PlatformOpenAI}
			var err error
			if kind == "openai" {
				err = (&OpenAIGatewayService{}).handleOpenAIUpstreamTransportError(c.Request.Context(), c, account, ErrCodingQuotaUnavailable, false)
			} else {
				err = (&GatewayService{}).handleUpstreamTransportError(c.Request.Context(), c, account, ErrCodingQuotaUnavailable, OpsUpstreamErrorEvent{})
			}
			require.ErrorIs(t, err, ErrCodingQuotaUnavailable)
			require.Equal(t, http.StatusServiceUnavailable, w.Code)
			require.Contains(t, w.Body.String(), "CODING_QUOTA_UNAVAILABLE")
		})
	}
}
