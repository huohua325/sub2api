package t3gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	testPublic = "https://t3.example.test"
	testAdmin  = "https://mile.example.test"
	testAgent  = "test-browser"
	testBearer = "mile-fixture-credential"
)

func fixture(t *testing.T, forward http.Handler, validate Validate, pair Pair) *Gateway {
	t.Helper()
	upstream := httptest.NewServer(forward)
	t.Cleanup(upstream.Close)
	if validate == nil {
		validate = func(_ context.Context, bearer, agent string) error {
			if bearer != testBearer || agent != testAgent {
				return errors.New("invalid MILE credential")
			}
			return nil
		}
	}
	if pair == nil {
		pair = func(context.Context) (string, error) { return "native-pair-code", nil }
	}
	gateway, err := New(Config{PublicURL: testPublic, AdminOrigin: testAdmin, UpstreamURL: upstream.URL}, validate, pair)
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func request(t *testing.T, g *Gateway, method, path, origin, agent string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Host = g.Host()
	req.Header.Set("Origin", origin)
	req.Header.Set("User-Agent", agent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	g.ServeHTTP(res, req)
	return res
}

func issue(t *testing.T, g *Gateway, browser string) url.Values {
	t.Helper()
	ticket, err := g.Issue(testBearer, browser)
	if err != nil || ticket.URL != testPublic+enterPath || ticket.Ticket == "" {
		t.Fatalf("unexpected ticket result: %v", err)
	}
	return url.Values{"ticket": {ticket.Ticket}}
}

func enter(t *testing.T, g *Gateway) *http.Cookie {
	t.Helper()
	result := request(t, g, http.MethodPost, enterPath, testAdmin, testAgent, issue(t, g, testAgent))
	if result.Code != http.StatusSeeOther {
		t.Fatalf("enter status %d: %s", result.Code, result.Body.String())
	}
	cookies := result.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d gateway cookies, want 1", len(cookies))
	}
	return cookies[0]
}

func TestGatewayRejectsAnonymousRequests(t *testing.T) {
	var called atomic.Bool
	g := fixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) }), nil, nil)
	for _, test := range []struct {
		name   string
		method string
		path   string
		origin string
		status int
	}{
		{"root", http.MethodGet, "/", "", http.StatusUnauthorized},
		{"asset", http.MethodGet, "/assets/workspace.js", "", http.StatusUnauthorized},
		{"pair-page", http.MethodGet, "/pair", "", http.StatusUnauthorized},
		{"api", http.MethodPost, "/api/tasks", testPublic, http.StatusUnauthorized},
		{"cross-origin", http.MethodPost, "/api/tasks", testAdmin, http.StatusForbidden},
		{"private-tool-api", http.MethodPost, "/mcp", testPublic, http.StatusNotFound},
		{"private-pairing-api", http.MethodPost, "/api/auth/pairing-token", testPublic, http.StatusNotFound},
		{"enter-origin", http.MethodPost, enterPath, testPublic, http.StatusForbidden},
		{"enter-method", http.MethodGet, enterPath, testAdmin, http.StatusMethodNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := request(t, g, test.method, test.path, test.origin, testAgent, nil)
			if result.Code != test.status {
				t.Fatalf("got %d, want %d", result.Code, test.status)
			}
		})
	}
	if called.Load() {
		t.Fatal("an unauthorized request reached T3")
	}
}

func TestGatewayIssuesOneTimeOriginAndBrowserBoundTickets(t *testing.T) {
	g := fixture(t, http.NotFoundHandler(), nil, nil)
	now := time.Now()
	g.now = func() time.Time { return now }
	ticket := issue(t, g, testAgent)
	wrongAgent := request(t, g, http.MethodPost, enterPath, testAdmin, "other browser", ticket)
	if wrongAgent.Code != http.StatusUnauthorized {
		t.Fatalf("wrong browser status %d", wrongAgent.Code)
	}
	result := request(t, g, http.MethodPost, enterPath, testAdmin, testAgent, ticket)
	if result.Code != http.StatusSeeOther || result.Header().Get("Location") != "/pair#token=native-pair-code" || result.Body.String() != "" {
		t.Fatalf("pair response: status %d, path %q", result.Code, result.Header().Get("Location"))
	}
	cookies := result.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionName || cookies[0].Domain != "" || cookies[0].Path != "/" ||
		!cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("missing a host-only Secure HttpOnly SameSite cookie")
	}
	replay := request(t, g, http.MethodPost, enterPath, testAdmin, testAgent, ticket)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replay status %d", replay.Code)
	}
	expired := issue(t, g, testAgent)
	now = now.Add(ticketTTL)
	if expiredResult := request(t, g, http.MethodPost, enterPath, testAdmin, testAgent, expired); expiredResult.Code != http.StatusUnauthorized {
		t.Fatalf("expired status %d", expiredResult.Code)
	}
}

func TestGatewayKeepsMILECredentialsOutOfT3(t *testing.T) {
	g := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s|%s|%s|%s|%s", r.Host, r.Header.Get("X-Forwarded-Host"),
			r.Header.Get("X-Forwarded-Proto"), r.Header.Get("Cookie"), r.Header.Get("Authorization"))
	}), nil, nil)
	session := enter(t, g)
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	req.Host = g.Host()
	req.Header.Set("User-Agent", testAgent)
	req.Header.Set("Authorization", "Bearer "+testBearer)
	req.Header.Set("X-Forwarded-Host", "forged.example.test")
	req.AddCookie(session)
	req.AddCookie(&http.Cookie{Name: "native_session", Value: "t3-fixture-cookie"})
	res := httptest.NewRecorder()
	g.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "t3.example.test|t3.example.test|https|native_session=t3-fixture-cookie|" {
		t.Fatalf("unexpected proxied request: %d, %q", res.Code, res.Body.String())
	}
	if res.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("private T3 response is cacheable")
	}
}

func TestGatewayClosesWebSocketWhenAdministratorLosesAccess(t *testing.T) {
	var revoked atomic.Bool
	validate := func(_ context.Context, _, _ string) error {
		if revoked.Load() {
			return errors.New("session revoked")
		}
		return nil
	}
	upgrader := websocket.Upgrader{}
	g := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() {
			if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
		}()
		if err := conn.WriteMessage(websocket.TextMessage, []byte("ready")); err != nil {
			t.Error(err)
			return
		}
		// Go's reverse proxy should close the upstream socket on revocation.
		_, _, _ = conn.ReadMessage()
	}), validate, nil)
	g.checkInterval = 10 * time.Millisecond
	session := enter(t, g)
	server := httptest.NewServer(g)
	defer server.Close()
	dialer := websocket.Dialer{}
	headers := http.Header{
		"Host":       {g.Host()},
		"Origin":     {testPublic},
		"User-Agent": {testAgent},
		"Cookie":     {session.Name + "=" + session.Value},
	}
	conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", headers)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
	}()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, payload, err := conn.ReadMessage(); err != nil || string(payload) != "ready" {
		t.Fatalf("expected working websocket, got %v", err)
	}
	revoked.Store(true)
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("revoked websocket remained connected")
	} else {
		var netErr interface{ Timeout() bool }
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatal("revoked websocket timed out instead of being closed")
		}
	}
	res := request(t, g, http.MethodGet, "/", "", testAgent, nil, session)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session was able to reconnect: %d", res.Code)
	}
}

func TestGatewayDeepLinkRedirectAndPostPairRestore(t *testing.T) {
	g := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("proxied:" + r.URL.RequestURI()))
	}), nil, nil)

	// Browser HTML navigation without a cookie redirects smoothly to MILE admin with ?next=...
	navReq := httptest.NewRequest(http.MethodGet, "/threads/abc?tab=diff", nil)
	navReq.Host = g.Host()
	navReq.Header.Set("User-Agent", testAgent)
	navReq.Header.Set("Accept", "text/html,application/xhtml+xml")
	navRes := httptest.NewRecorder()
	g.ServeHTTP(navRes, navReq)
	if navRes.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect for HTML navigation, got %d", navRes.Code)
	}
	wantRedirect := testAdmin + "/admin/development-workspace?next=%2Fthreads%2Fabc%3Ftab%3Ddiff"
	if navRes.Header().Get("Location") != wantRedirect {
		t.Fatalf("redirect location = %q, want %q", navRes.Header().Get("Location"), wantRedirect)
	}

	// API requests with Accept: text/html still get 401 (never redirected).
	apiReq := httptest.NewRequest(http.MethodGet, "/api/threads", nil)
	apiReq.Host = g.Host()
	apiReq.Header.Set("User-Agent", testAgent)
	apiReq.Header.Set("Accept", "text/html")
	apiRes := httptest.NewRecorder()
	g.ServeHTTP(apiRes, apiReq)
	if apiRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for API path, got %d", apiRes.Code)
	}

	// Issue a ticket carrying the deep link, enter, and confirm `/` restores the deep link once.
	ticket, err := g.IssueWithNext(context.Background(), testBearer, testAgent, "/threads/abc?tab=diff")
	if err != nil {
		t.Fatal(err)
	}
	enterRes := request(t, g, http.MethodPost, enterPath, testAdmin, testAgent, url.Values{"ticket": {ticket.Ticket}})
	if enterRes.Code != http.StatusSeeOther || enterRes.Header().Get("Location") != "/pair#token=native-pair-code" {
		t.Fatalf("unexpected enter response: %d %q", enterRes.Code, enterRes.Header().Get("Location"))
	}
	cookies := enterRes.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected session + next cookies, got %d", len(cookies))
	}
	rootRes := request(t, g, http.MethodGet, "/", "", testAgent, nil, cookies...)
	if rootRes.Code != http.StatusSeeOther || rootRes.Header().Get("Location") != "/threads/abc?tab=diff" {
		t.Fatalf("expected deep-link restore redirect, got %d %q", rootRes.Code, rootRes.Header().Get("Location"))
	}

	// Subsequent visit to `/` without the one-time next cookie proxies normally.
	plainRes := request(t, g, http.MethodGet, "/", "", testAgent, nil, cookies[0])
	if plainRes.Code != http.StatusOK || plainRes.Body.String() != "proxied:/" {
		t.Fatalf("expected normal proxy after next consumed, got %d %q", plainRes.Code, plainRes.Body.String())
	}
}

func TestGatewaySanitizeNextPathRejectsUnsafeTargets(t *testing.T) {
	for _, bad := range []string{
		"", "/", "//evil.test/x", "https://evil.test/x", "/\\evil", "/_mile/enter",
		"/pair", "/pair#token=x", "/api/auth/pairing-token", "/mcp", "/assets/app.js",
		"/threads/a\r\nb",
	} {
		if got := SanitizeNextPath(bad); got != "" {
			t.Fatalf("SanitizeNextPath(%q) = %q, want empty", bad, got)
		}
	}
	if got := SanitizeNextPath("/threads/123?view=files"); got != "/threads/123?view=files" {
		t.Fatalf("unexpected sanitized deep link: %q", got)
	}
}

func TestGatewaySharedStoreSurvivesRestart(t *testing.T) {
	shared := newMemoryStore(time.Now)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	validate := func(_ context.Context, cred, agent string) error {
		if cred != testBearer || agent != testAgent {
			return errors.New("denied")
		}
		return nil
	}
	pair := func(context.Context) (string, error) { return "pair-code", nil }
	cfg := Config{PublicURL: testPublic, AdminOrigin: testAdmin, UpstreamURL: upstream.URL, Store: shared}

	gw1, err := New(cfg, validate, pair)
	if err != nil {
		t.Fatal(err)
	}
	sessionCookie := enter(t, gw1)

	// New Gateway instance sharing the same Store simulates a Sub2API process restart.
	gw2, err := New(cfg, validate, pair)
	if err != nil {
		t.Fatal(err)
	}
	res := request(t, gw2, http.MethodGet, "/threads/1", "", testAgent, nil, sessionCookie)
	if res.Code != http.StatusOK || res.Body.String() != "ok" {
		t.Fatalf("session did not survive gateway restart: %d %q", res.Code, res.Body.String())
	}
}
