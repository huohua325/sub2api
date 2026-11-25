//go:build unix

package t3gateway

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func TestGatewayProxiesThroughUnixSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "t3.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	upstream := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host != "t3.example.test" || r.URL.Path != "/assets/t3.js" {
				t.Errorf("wrong T3 request %s %s", r.Host, r.URL.Path)
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	go func() {
		if err := upstream.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Error(err)
		}
	}()
	t.Cleanup(func() {
		if err := upstream.Close(); err != nil {
			t.Error(err)
		}
	})
	gateway, err := New(Config{
		PublicURL: testPublic, AdminOrigin: testAdmin,
		UpstreamURL: "http://127.0.0.1:9", UpstreamSocket: socket,
	}, func(context.Context, string, string) error { return nil },
		func(context.Context) (string, error) { return "native-pair-code", nil })
	if err != nil {
		t.Fatal(err)
	}
	session := enter(t, gateway)
	res := request(t, gateway, http.MethodGet, "/assets/t3.js", "", testAgent, nil, session)
	if res.Code != http.StatusNoContent {
		t.Fatalf("got %d, want %d", res.Code, http.StatusNoContent)
	}
}
