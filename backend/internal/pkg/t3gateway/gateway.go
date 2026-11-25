// Package t3gateway places a MILE administrator session in front of the native development workspace.
package t3gateway

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	enterPath      = "/_mile/enter"
	sessionName    = "__Host-mile_ws"
	nextPathCookie = "__Host-mile_ws_next"
	ticketTTL      = time.Minute
	sessionTTL     = 8 * time.Hour
	nextCookieTTL  = 2 * time.Minute
	checkTimeout   = 3 * time.Second
	maxTickets     = 256
	maxSessions    = 1024
	maxNextPathLen = 512
)

// Config describes the public origin, private workspace server and trusted MILE origin.
type Config struct {
	PublicURL      string
	UpstreamURL    string
	UpstreamSocket string
	AdminOrigin    string
	Store          Store
}

// Validate checks that the credential still belongs to an active MILE administrator.
type Validate func(ctx context.Context, credential, userAgent string) error

// Pair issues an official one-time workspace pairing credential.
type Pair func(ctx context.Context) (string, error)

// Ticket is the browser handoff form returned only to a MILE administrator.
type Ticket struct {
	URL    string `json:"url"`
	Ticket string `json:"ticket"`
}

// Record holds the gateway-side ticket or session state bound to a browser.
type Record struct {
	Credential string    `json:"credential"`
	UserAgent  string    `json:"user_agent"`
	NextPath   string    `json:"next_path,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// Store persists launch tickets and active workspace gateway sessions.
type Store interface {
	SaveTicket(ctx context.Context, id string, record Record, ttl time.Duration) error
	ConsumeTicket(ctx context.Context, id, userAgent string) (Record, bool, error)
	SaveSession(ctx context.Context, key string, record Record, ttl time.Duration) error
	GetSession(ctx context.Context, key string) (Record, bool, error)
	DeleteSession(ctx context.Context, key string) error
}

type memoryStore struct {
	now      func() time.Time
	mu       sync.Mutex
	tickets  map[string]Record
	sessions map[string]Record
}

func newMemoryStore(now func() time.Time) *memoryStore {
	return &memoryStore{
		now:      now,
		tickets:  make(map[string]Record),
		sessions: make(map[string]Record),
	}
}

func (m *memoryStore) SaveTicket(_ context.Context, id string, record Record, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prune(m.tickets, m.now())
	if len(m.tickets) >= maxTickets {
		return errors.New("workspace gateway ticket capacity exceeded")
	}
	m.tickets[id] = record
	return nil
}

func (m *memoryStore) ConsumeTicket(_ context.Context, id, userAgent string) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.tickets[id]
	if ok && record.UserAgent == userAgent {
		delete(m.tickets, id)
	}
	return record, ok, nil
}

func (m *memoryStore) SaveSession(_ context.Context, key string, record Record, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prune(m.sessions, m.now())
	if len(m.sessions) >= maxSessions {
		return errors.New("workspace gateway session capacity exceeded")
	}
	m.sessions[key] = record
	return nil
}

func (m *memoryStore) GetSession(_ context.Context, key string) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.sessions[key]
	return record, ok, nil
}

func (m *memoryStore) DeleteSession(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, key)
	return nil
}

// Gateway authorizes and proxies browser traffic for one private workspace server.
type Gateway struct {
	public        *url.URL
	adminOrigin   string
	validate      Validate
	pair          Pair
	proxy         *httputil.ReverseProxy
	now           func() time.Time
	checkInterval time.Duration
	store         Store
	defaultStore  *memoryStore
}

// New constructs a gateway for a single public HTTPS origin and private workspace endpoint.
func New(config Config, validate Validate, pair Pair) (*Gateway, error) {
	public, err := parseOrigin(config.PublicURL)
	if err != nil || public.Scheme != "https" {
		return nil, errors.New("workspace public URL must be an HTTPS origin")
	}
	admin, err := parseOrigin(config.AdminOrigin)
	if err != nil || admin.Scheme != "https" || strings.EqualFold(admin.Hostname(), public.Hostname()) {
		return nil, errors.New("MILE admin URL must be a different HTTPS host from the workspace")
	}
	upstream, err := parseOrigin(config.UpstreamURL)
	if err != nil || !isPrivateUpstream(upstream) || strings.EqualFold(public.Host, upstream.Host) {
		return nil, errors.New("workspace upstream must be a separate private origin")
	}
	dial := (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	if socket := config.UpstreamSocket; socket != "" {
		if !validSocket(socket, upstream) {
			return nil, errors.New("workspace Unix socket requires a canonical absolute path and an HTTP loopback origin")
		}
		unixDial := dial
		dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return unixDial(ctx, "unix", socket)
		}
	}
	if validate == nil || pair == nil {
		return nil, errors.New("workspace gateway requires administrator validation and pairing")
	}

	transport := &http.Transport{
		// Ignore proxy environment variables for this private connection.
		DialContext:           dial,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	g := &Gateway{
		public:        public,
		adminOrigin:   admin.String(),
		validate:      validate,
		pair:          pair,
		now:           time.Now,
		checkInterval: 5 * time.Second,
		proxy: &httputil.ReverseProxy{
			Transport: transport,
			Rewrite: func(req *httputil.ProxyRequest) {
				req.SetURL(upstream)
				req.Out.Host = public.Host
				req.Out.Header.Set("X-Forwarded-Host", public.Host)
				req.Out.Header.Set("X-Forwarded-Proto", public.Scheme)
				req.Out.Header.Del("Authorization")
				req.Out.Header.Del("X-Api-Key")
				req.Out.Header.Del("X-Admin-UI-Request")
				req.Out.Header.Del("Cookie")
				for _, cookie := range req.In.Cookies() {
					if !isGatewayCookie(cookie.Name) {
						req.Out.AddCookie(cookie)
					}
				}
			},
			ModifyResponse: func(resp *http.Response) error {
				noStore(resp.Header)
				resp.Header.Add("Content-Security-Policy", "frame-ancestors 'none'")
				cookies := resp.Cookies()
				resp.Header.Del("Set-Cookie")
				for _, cookie := range cookies {
					if !isGatewayCookie(cookie.Name) {
						cookie.Domain = ""
						cookie.Secure = true
						resp.Header.Add("Set-Cookie", cookie.String())
					}
				}
				if location := resp.Header.Get("Location"); location != "" {
					target, parseErr := url.Parse(location)
					if parseErr == nil && strings.EqualFold(target.Host, upstream.Host) {
						target.Scheme, target.Host = public.Scheme, public.Host
						resp.Header.Set("Location", target.String())
					}
				}
				return nil
			},
			ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
				noStore(w.Header())
				http.Error(w, "Development workspace is unavailable", http.StatusBadGateway)
			},
		},
	}
	if config.Store != nil {
		g.store = config.Store
	} else {
		g.defaultStore = newMemoryStore(func() time.Time { return g.now() })
		g.store = g.defaultStore
	}
	return g, nil
}

func isGatewayCookie(name string) bool {
	return name == sessionName || name == nextPathCookie
}

func parseOrigin(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawPath != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.Opaque != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid origin")
	}
	parsed.Path = ""
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed, nil
}

func isPrivateUpstream(upstream *url.URL) bool {
	ip, err := netip.ParseAddr(upstream.Hostname())
	if err == nil {
		ip = ip.Unmap()
		return ip.IsPrivate() || ip.IsLoopback()
	}
	return upstream.Scheme == "https"
}

func validSocket(socket string, upstream *url.URL) bool {
	ip, err := netip.ParseAddr(upstream.Hostname())
	return err == nil && ip.IsLoopback() && upstream.Scheme == "http" &&
		path.IsAbs(socket) && path.Clean(socket) == socket && socket != "/" &&
		len(socket) < 108 && !strings.ContainsAny(socket, "\\\x00")
}

// SanitizeNextPath validates an internal workspace deep-link return path.
func SanitizeNextPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "/" || len(trimmed) > maxNextPathLen ||
		!strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") ||
		strings.ContainsAny(trimmed, "\\\r\n\x00#") {
		return ""
	}
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.Opaque != "" {
		return ""
	}
	cleaned := path.Clean(parsed.Path)
	if cleaned == "/" || cleaned == "." || !strings.HasPrefix(cleaned, "/") ||
		cleaned == enterPath || strings.HasPrefix(cleaned, "/_mile/") ||
		cleaned == "/pair" || strings.HasPrefix(cleaned, "/pair/") ||
		cleaned == "/api" || strings.HasPrefix(cleaned, "/api/") ||
		cleaned == "/assets" || strings.HasPrefix(cleaned, "/assets/") ||
		privateT3Route(cleaned) {
		return ""
	}
	if parsed.RawQuery != "" {
		return cleaned + "?" + parsed.RawQuery
	}
	return cleaned
}

// Host is the dedicated public host matched by the outer MILE router.
func (g *Gateway) Host() string {
	return g.public.Host
}

// Issue creates a single-use ticket for an already-authorized MILE browser.
func (g *Gateway) Issue(credential, userAgent string) (Ticket, error) {
	return g.IssueWithNext(context.Background(), credential, userAgent, "")
}

// IssueWithNext creates a single-use ticket carrying an optional sanitized workspace deep link.
func (g *Gateway) IssueWithNext(ctx context.Context, credential, userAgent, nextPath string) (Ticket, error) {
	if len(credential) == 0 || len(credential) > 8192 || len(userAgent) == 0 || len(userAgent) > 1024 {
		return Ticket{}, errors.New("invalid MILE browser credential")
	}
	now := g.now()
	id := rand.Text()
	record := Record{
		Credential: credential,
		UserAgent:  userAgent,
		NextPath:   SanitizeNextPath(nextPath),
		ExpiresAt:  now.Add(ticketTTL),
	}
	if err := g.store.SaveTicket(ctx, id, record, ticketTTL); err != nil {
		return Ticket{}, err
	}
	return Ticket{URL: g.public.String() + enterPath, Ticket: id}, nil
}

// ServeHTTP enforces the MILE session on every native workspace route.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	noStore(w.Header())
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !strings.EqualFold(r.Host, g.public.Host) || r.URL.IsAbs() {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == enterPath {
		g.enter(w, r)
		return
	}
	if privateT3Route(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	if !g.sameOrigin(r) {
		http.Error(w, "Workspace origin required", http.StatusForbidden)
		return
	}
	cookie, err := r.Cookie(sessionName)
	if err != nil {
		if g.redirectBrowserToAdmin(w, r, false) {
			return
		}
		http.Error(w, "Open the development workspace from the MILE administrator panel", http.StatusUnauthorized)
		return
	}
	session, ok, storeErr := g.store.GetSession(r.Context(), cookie.Value)
	if storeErr != nil || !ok || session.UserAgent != r.UserAgent() || !g.authorized(r.Context(), session) {
		if storeErr == nil && ok && session.UserAgent == r.UserAgent() && r.Context().Err() == nil {
			_ = g.store.DeleteSession(r.Context(), cookie.Value)
		}
		if g.redirectBrowserToAdmin(w, r, true) {
			return
		}
		http.Error(w, "MILE administrator session expired", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/" {
		if nextCookie, nextErr := r.Cookie(nextPathCookie); nextErr == nil {
			clearCookie(w, nextPathCookie)
			if safeNext := SanitizeNextPath(nextCookie.Value); safeNext != "" {
				w.Header().Set("Location", safeNext)
				w.WriteHeader(http.StatusSeeOther)
				return
			}
		}
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go g.watch(ctx, cancel, cookie.Value, session)
	g.proxy.ServeHTTP(w, r.WithContext(ctx))
}

func (g *Gateway) redirectBrowserToAdmin(w http.ResponseWriter, r *http.Request, clearSession bool) bool {
	if r.Method != http.MethodGet || r.Header.Get("Upgrade") != "" {
		return false
	}
	cleaned := path.Clean(r.URL.Path)
	if strings.HasPrefix(cleaned, "/api/") || cleaned == "/api" ||
		strings.HasPrefix(cleaned, "/assets/") || cleaned == "/assets" ||
		cleaned == "/favicon.ico" || strings.HasPrefix(cleaned, "/favicon-") {
		return false
	}
	secFetchMode := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Mode")))
	accept := strings.ToLower(r.Header.Get("Accept"))
	if secFetchMode != "navigate" && !strings.Contains(accept, "text/html") {
		return false
	}
	if clearSession {
		clearCookie(w, sessionName)
	}
	target := g.adminOrigin + "/admin/development-workspace"
	if safeNext := SanitizeNextPath(r.URL.RequestURI()); safeNext != "" {
		target += "?" + url.Values{"next": {safeNext}}.Encode()
	}
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusSeeOther)
	return true
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

func (g *Gateway) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin != "" {
		return origin == g.public.String()
	}
	return r.Header.Get("Upgrade") == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead)
}

func privateT3Route(rawPath string) bool {
	resolved := path.Clean(rawPath)
	for _, prefix := range []string{
		"/mcp", "/oauth/mcp", "/.well-known/oauth-authorization-server",
		"/.well-known/oauth-protected-resource", "/api/auth/pairing-token",
		"/api/auth/pairing-links", "/api/auth/connect-devices",
	} {
		if resolved == prefix || strings.HasPrefix(resolved, prefix+"/") {
			return true
		}
	}
	return false
}

func (g *Gateway) enter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Origin") != g.adminOrigin {
		http.Error(w, "MILE admin origin required", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid workspace ticket form", http.StatusBadRequest)
		return
	}
	id := r.PostForm.Get("ticket")
	session, ok, err := g.store.ConsumeTicket(r.Context(), id, r.UserAgent())
	if err != nil || !ok || session.UserAgent != r.UserAgent() || !g.authorized(r.Context(), session) {
		http.Error(w, "Workspace ticket expired", http.StatusUnauthorized)
		return
	}

	grant, err := g.pair(r.Context())
	if err != nil || grant == "" || len(grant) > 256 || strings.ContainsAny(grant, " \t\r\n") {
		http.Error(w, "Development workspace pairing is unavailable", http.StatusBadGateway)
		return
	}
	key, err := g.storeSession(r.Context(), session)
	if err != nil {
		http.Error(w, "Development workspace gateway is busy", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionName, Value: key, Path: "/", MaxAge: int(sessionTTL / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	if safeNext := SanitizeNextPath(session.NextPath); safeNext != "" {
		http.SetCookie(w, &http.Cookie{
			Name: nextPathCookie, Value: safeNext, Path: "/", MaxAge: int(nextCookieTTL / time.Second),
			HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
		})
	}
	w.Header().Set("Location", "/pair#"+url.Values{"token": {grant}}.Encode())
	w.WriteHeader(http.StatusSeeOther)
}

func (g *Gateway) storeSession(ctx context.Context, session Record) (string, error) {
	now := g.now()
	key := rand.Text()
	session.NextPath = ""
	session.ExpiresAt = now.Add(sessionTTL)
	if err := g.store.SaveSession(ctx, key, session, sessionTTL); err != nil {
		return "", err
	}
	return key, nil
}

func (g *Gateway) authorized(ctx context.Context, session Record) bool {
	if !g.now().Before(session.ExpiresAt) {
		return false
	}
	check, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	return g.validate(check, session.Credential, session.UserAgent) == nil
}

func (g *Gateway) watch(ctx context.Context, closeConnection context.CancelFunc, key string, session Record) {
	ticker := time.NewTicker(g.checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !g.authorized(ctx, session) {
				// A closed browser request does not invalidate its other connections.
				if ctx.Err() == nil {
					_ = g.store.DeleteSession(context.Background(), key)
					closeConnection()
				}
				return
			}
		}
	}
}

func prune(items map[string]Record, now time.Time) {
	for id, item := range items {
		if !now.Before(item.ExpiresAt) {
			delete(items, id)
		}
	}
}

func noStore(headers http.Header) {
	headers.Set("Cache-Control", "private, no-store")
}

var _ http.Handler = (*Gateway)(nil)
