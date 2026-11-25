package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeT3PairingUsesScopedOfficialAPI(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/pairing-token" || r.Method != http.MethodPost ||
			r.Header.Get("Authorization") != "Bearer local-test-issuer" {
			t.Error("unexpected upstream pairing request")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var payload struct {
			Scopes []string `json:"scopes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		require.Contains(t, payload.Scopes, "orchestration:operate")
		require.Contains(t, payload.Scopes, "terminal:operate")
		require.NotContains(t, payload.Scopes, "access:write")
		_, _ = w.Write([]byte(`{"credential":"test-pair-code"}`))
	}))
	defer upstream.Close()
	code, err := createNativeT3Pairing(context.Background(), upstream.Client(), upstream.URL, "local-test-issuer")
	require.NoError(t, err)
	require.Equal(t, "test-pair-code", code)
}
