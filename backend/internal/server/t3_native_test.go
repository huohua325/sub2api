package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/t3gateway"
	"github.com/Wei-Shaw/sub2api/internal/service"
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

func TestNativeT3PairTokenLoaderHotReloadsRotatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pair-token")
	require.NoError(t, os.WriteFile(path, []byte("initial-token\n"), 0o600))

	loader, err := newPairTokenLoader(path)
	require.NoError(t, err)
	tok, err := loader.get()
	require.NoError(t, err)
	require.Equal(t, "initial-token", tok)

	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.WriteFile(path, []byte("rotated-token-v2\n"), 0o600))
	require.NoError(t, os.Chtimes(path, future, future))

	tok, err = loader.get()
	require.NoError(t, err)
	require.Equal(t, "rotated-token-v2", tok)
}

func TestNativeT3ProbeRunnerReportsReachability(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "pair-token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("issuer-token"), 0o600))

	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()

	ws, err := newNativeT3Workspace(
		t3gateway.Config{
			PublicURL:   "https://studio.example.test",
			AdminOrigin: "https://admin.example.test",
			UpstreamURL: upstream.URL,
		},
		tokenFile,
		func(context.Context, string) (service.T3BrowserSession, error) {
			return service.T3BrowserSession{UserID: 1, SessionID: "s1", TokenVersion: 1}, nil
		},
		func(context.Context, service.T3BrowserSession) error { return nil },
	)
	require.NoError(t, err)
	require.True(t, ws.probeRunner(context.Background()))

	upstream.Close()
	require.False(t, ws.probeRunner(context.Background()))
}
