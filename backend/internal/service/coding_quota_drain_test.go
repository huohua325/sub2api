package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requestdrain"
	"github.com/stretchr/testify/require"
)

func TestCodingWebSocketDrainRejectsNewTurnsEvenWithQuotaOffOrShadow(t *testing.T) {
	for _, mode := range []string{"off", "shadow", "enforce"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SUB2API_CODING_QUOTA_OPENAI_MODE", mode)
			d := requestdrain.New()
			store := &codingRuntimeStore{}
			points := NewOfficialPointService(nil, store, nil)
			svc := &OpenAIGatewayService{codingPoints: points}
			key := &APIKey{ID: 1, UserID: 1, Group: &Group{Platform: "openai", UsageKind: "coding"}}
			handler := d.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hooks, cleanup, err := svc.codingWSTurnHooks(r.Context(), key, &Account{ID: 10}, []byte(`{"model":"gpt-5"}`), nil)
				require.NoError(t, err)
				defer cleanup()
				d.Begin()
				err = hooks.BeforeRequest(2, []byte(`{"model":"gpt-5"}`), "gpt-5")
				require.ErrorIs(t, err, requestdrain.ErrDraining)
				require.Contains(t, string(codingWSAdmissionErrorFrame(err)), "SERVER_DRAINING")
				hooks.AfterTurn(1, &OpenAIForwardResult{}, nil)
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/responses", nil))
			require.NoError(t, d.Wait(context.Background()))
		})
	}
}

func TestCodingRealtimeDrainRejectsNewTurnWhenQuotaDisabled(t *testing.T) {
	t.Setenv("SUB2API_CODING_QUOTA_GROK_MODE", "off")
	d := requestdrain.New()
	session := newCodingRealtimeSession(nil, nil, nil, "grok")
	handler := d.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, session.Begin(r.Context()))
		session.End()
		d.Begin()
		require.ErrorIs(t, session.Begin(r.Context()), requestdrain.ErrDraining)
		_ = session.Close()
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/realtime", nil))
}
