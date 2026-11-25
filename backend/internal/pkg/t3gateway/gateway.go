// Package t3gateway places a MILE administrator session in front of native T3 Code.
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
	enterPath    = "/_mile/enter"
	sessionName  = "__Host-mile_t3"
	ticketTTL    = time.Minute
	sessionTTL   = 8 * time.Hour
	checkTimeout = 3 * time.Second
	maxTickets   = 256
	maxSessions  = 1024
)

// Config describes the public origin, private T3 server and trusted MILE origin.
type Config struct {
	PublicURL      string
	UpstreamURL    string
	UpstreamSocket string
	AdminOrigin    string
}

// Validate checks that the credential still belongs to an active MILE administrator.
type Validate func(ctx context.Context, bearer, userAgent string) error

// Pair issues an official one-time T3 Code pairing credential.
type Pair func(ctx context.Context) (string, error)

// Ticket is the browser handoff form returned only to a MILE administrator.
type Ticket struct {
	URL    string `json:"url"`
	Ticket string `json:"ticket"`
}

type credential struct {
	bearer    string
	userAgent string
	expiresAt time.Time
}

// Gateway authorizes and proxies browser traffic for one private T3 Code server.
type Gateway struct {
	public        *url.URL
	adminOrigin   string
	validate      Validate
	pair          Pair
	proxy         *httputil.ReverseProxy
	now           func() time.Time
	checkInterval time.Duration

	mu       sync.Mutex
	tickets  map[string]credential
	sessions map[string]credential
}

// New constructs a gateway for a single public HTTPS origin and private T3 endpoint.
func New(config Config, validate Validate, pair Pair) (*Gateway, error) {
	public, err := parseOrigin(config.PublicURL)
	if err != nil || public.Scheme != "https" {
		return nil, errors.New("T3 public URL must be an HTTPS origin")
	}
	admin, err := parseOrigin(config.AdminOrigin)
	if err != nil || admin.Scheme != "https" || strings.EqualFold(admin.Hostname(), public.Hostname()) {
		return nil, errors.New("MILE admin URL must be a different HTTPS host from T3")
	}
	upstream, err := parseOrigin(config.UpstreamURL)
	if err != nil || !isPrivateUpstream(upstream) || strings.EqualFold(public.Host, upstream.Host) {
		return nil, errors.New("T3 upstream must be a separate private origin")
	}
	dial := (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	if socket := config.UpstreamSocket; socket != "" {
		if !validSocket(socket, upstream) {
			return nil, errors.New("T3 Unix socket requires a canonical absolute path and an HTTP loopback origin")
		}
		unixDial := dial
		dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return unixDial(ctx, "unix", socket)
		}
	}
	if validate == nil || pair == nil {
		return nil, errors.New("T3 gateway requires administrator validation and pairing")
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

	return &Gateway{
		public:        public,
		adminOrigin:   admin.String(),
		validate:      validate,
		pair:          pair,
		now:           time.Now,
		checkInterval: 5 * time.Second,
		tickets:       make(map[string]credential),
		sessions:      make(map[string]credential),
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
					if cookie.Name != sessionName {
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
					if cookie.Name != sessionName {
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
				http.Error(w, "T3 Code is unavailable", http.StatusBadGateway)
			},
		},
	}, nil
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

// Host is the dedicated public host matched by the outer MILE router.
func (g *Gateway) Host() string {
	return g.public.Host
}

// Issue creates a single-use ticket for an already-authorized MILE browser.
func (g *Gateway) Issue(bearer, userAgent string) (Ticket, error) {
	if len(bearer) == 0 || len(bearer) > 8192 || len(userAgent) == 0 || len(userAgent) > 1024 {
		return Ticket{}, errors.New("invalid MILE browser credential")
	}
	now := g.now()
	id := rand.Text()
	g.mu.Lock()
	defer g.mu.Unlock()
	prune(g.tickets, now)
	if len(g.tickets) >= maxTickets {
		return Ticket{}, errors.New("T3 gateway ticket capacity exceeded")
	}
	g.tickets[id] = credential{bearer: bearer, userAgent: userAgent, expiresAt: now.Add(ticketTTL)}
	return Ticket{URL: g.public.String() + enterPath, Ticket: id}, nil
}

// ServeHTTP enforces the MILE session on every native T3 route.
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
		http.Error(w, "T3 origin required", http.StatusForbidden)
		return
	}
	cookie, err := r.Cookie(sessionName)
	if err != nil {
		http.Error(w, "Open T3 Code from the MILE administrator panel", http.StatusUnauthorized)
		return
	}
	g.mu.Lock()
	session, ok := g.sessions[cookie.Value]
	g.mu.Unlock()
	if !ok || session.userAgent != r.UserAgent() || !g.authorized(r.Context(), session) {
		if ok && session.userAgent == r.UserAgent() && r.Context().Err() == nil {
			g.removeSession(cookie.Value)
		}
		http.Error(w, "MILE administrator session expired", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go g.watch(ctx, cancel, cookie.Value, session)
	g.proxy.ServeHTTP(w, r.WithContext(ctx))
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
		http.Error(w, "Invalid T3 ticket form", http.StatusBadRequest)
		return
	}
	id := r.PostForm.Get("ticket")
	g.mu.Lock()
	session, ok := g.tickets[id]
	if ok && session.userAgent == r.UserAgent() {
		delete(g.tickets, id)
	}
	g.mu.Unlock()
	if !ok || session.userAgent != r.UserAgent() || !g.authorized(r.Context(), session) {
		http.Error(w, "T3 ticket expired", http.StatusUnauthorized)
		return
	}

	grant, err := g.pair(r.Context())
	if err != nil || grant == "" || len(grant) > 256 || strings.ContainsAny(grant, " \t\r\n") {
		http.Error(w, "T3 Code pairing is unavailable", http.StatusBadGateway)
		return
	}
	key, err := g.storeSession(session)
	if err != nil {
		http.Error(w, "T3 gateway is busy", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionName, Value: key, Path: "/", MaxAge: int(sessionTTL / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Location", "/pair#"+url.Values{"token": {grant}}.Encode())
	w.WriteHeader(http.StatusSeeOther)
}

func (g *Gateway) storeSession(session credential) (string, error) {
	now := g.now()
	key := rand.Text()
	g.mu.Lock()
	defer g.mu.Unlock()
	prune(g.sessions, now)
	if len(g.sessions) >= maxSessions {
		return "", errors.New("T3 gateway session capacity exceeded")
	}
	session.expiresAt = now.Add(sessionTTL)
	g.sessions[key] = session
	return key, nil
}

func (g *Gateway) removeSession(key string) {
	g.mu.Lock()
	delete(g.sessions, key)
	g.mu.Unlock()
}

func (g *Gateway) authorized(ctx context.Context, session credential) bool {
	if !g.now().Before(session.expiresAt) {
		return false
	}
	check, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	return g.validate(check, session.bearer, session.userAgent) == nil
}

func (g *Gateway) watch(ctx context.Context, closeConnection context.CancelFunc, key string, session credential) {
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
					g.removeSession(key)
					closeConnection()
				}
				return
			}
		}
	}
}

func prune(items map[string]credential, now time.Time) {
	for id, item := range items {
		if !now.Before(item.expiresAt) {
			delete(items, id)
		}
	}
}

func noStore(headers http.Header) {
	headers.Set("Cache-Control", "private, no-store")
}

var _ http.Handler = (*Gateway)(nil)
