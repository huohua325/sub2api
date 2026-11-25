package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/t3gateway"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	redisTicketKeyPrefix  = "mile:devws:ticket:"
	redisSessionKeyPrefix = "mile:devws:session:"
)

var consumeTicketScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if not raw then
	return ""
end
local ok, parsed = pcall(cjson.decode, raw)
if ok and parsed and parsed["user_agent"] == ARGV[1] then
	redis.call("DEL", KEYS[1])
end
return raw
`)

type pairTokenLoader struct {
	path string

	mu      sync.RWMutex
	token   string
	modTime time.Time
	size    int64
}

func newPairTokenLoader(path string) (*pairTokenLoader, error) {
	loader := &pairTokenLoader{path: path}
	if _, err := loader.get(); err != nil {
		return nil, err
	}
	return loader, nil
}

func (l *pairTokenLoader) get() (string, error) {
	info, err := os.Stat(l.path)
	if err != nil {
		return "", err
	}
	l.mu.RLock()
	if l.token != "" && info.ModTime().Equal(l.modTime) && info.Size() == l.size {
		tok := l.token
		l.mu.RUnlock()
		return tok, nil
	}
	l.mu.RUnlock()

	l.mu.Lock()
	defer l.mu.Unlock()
	info, err = os.Stat(l.path)
	if err != nil {
		return "", err
	}
	if l.token != "" && info.ModTime().Equal(l.modTime) && info.Size() == l.size {
		return l.token, nil
	}
	token, err := readNativeT3PairToken(l.path)
	if err != nil {
		return "", err
	}
	l.token = token
	l.modTime = info.ModTime()
	l.size = info.Size()
	return token, nil
}

type redisGatewayStore struct {
	client *redis.Client
}

func (s *redisGatewayStore) SaveTicket(ctx context.Context, id string, record t3gateway.Record, ttl time.Duration) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, redisTicketKeyPrefix+id, raw, ttl).Err()
}

func (s *redisGatewayStore) ConsumeTicket(ctx context.Context, id, userAgent string) (t3gateway.Record, bool, error) {
	raw, err := consumeTicketScript.Run(ctx, s.client, []string{redisTicketKeyPrefix + id}, userAgent).Text()
	if errors.Is(err, redis.Nil) || raw == "" {
		return t3gateway.Record{}, false, nil
	}
	if err != nil {
		return t3gateway.Record{}, false, err
	}
	var record t3gateway.Record
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return t3gateway.Record{}, false, err
	}
	return record, true, nil
}

func (s *redisGatewayStore) SaveSession(ctx context.Context, key string, record t3gateway.Record, ttl time.Duration) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, redisSessionKeyPrefix+key, raw, ttl).Err()
}

func (s *redisGatewayStore) GetSession(ctx context.Context, key string) (t3gateway.Record, bool, error) {
	raw, err := s.client.Get(ctx, redisSessionKeyPrefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return t3gateway.Record{}, false, nil
	}
	if err != nil {
		return t3gateway.Record{}, false, err
	}
	var record t3gateway.Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return t3gateway.Record{}, false, err
	}
	return record, true, nil
}

func (s *redisGatewayStore) DeleteSession(ctx context.Context, key string) error {
	return s.client.Del(ctx, redisSessionKeyPrefix+key).Err()
}

type nativeT3Workspace struct {
	gateway        *t3gateway.Gateway
	public         *url.URL
	admin          *url.URL
	upstream       *url.URL
	upstreamSocket string
	tokens         *pairTokenLoader
	inspect        func(context.Context, string) (service.T3BrowserSession, error)
}

func configureNativeT3(auth *service.AuthService, redisClient *redis.Client) *nativeT3Workspace {
	if auth == nil || strings.TrimSpace(os.Getenv("DEV_T3_NATIVE_PUBLIC_URL")) == "" {
		return nil
	}
	cfg := t3gateway.Config{
		PublicURL:      strings.TrimSpace(os.Getenv("DEV_T3_NATIVE_PUBLIC_URL")),
		UpstreamURL:    strings.TrimSpace(os.Getenv("DEV_T3_NATIVE_UPSTREAM_URL")),
		AdminOrigin:    strings.TrimSpace(os.Getenv("DEV_T3_NATIVE_ADMIN_ORIGIN")),
		UpstreamSocket: strings.TrimSpace(os.Getenv("DEV_T3_NATIVE_UPSTREAM_SOCKET_FILE")),
	}
	if redisClient != nil {
		cfg.Store = &redisGatewayStore{client: redisClient}
	}
	native, err := newNativeT3Workspace(cfg, os.Getenv("DEV_T3_NATIVE_PAIR_TOKEN_FILE"),
		auth.InspectT3BrowserSession, auth.ValidateT3SessionIdentity)
	if err != nil {
		log.Print("Native development workspace disabled: invalid gateway or pairing configuration")
		return nil
	}
	return native
}

func newNativeT3Workspace(
	cfg t3gateway.Config,
	tokenFile string,
	inspect func(context.Context, string) (service.T3BrowserSession, error),
	validateIdentity func(context.Context, service.T3BrowserSession) error,
) (*nativeT3Workspace, error) {
	public, err := url.Parse(cfg.PublicURL)
	if err != nil {
		return nil, err
	}
	admin, err := url.Parse(cfg.AdminOrigin)
	if err != nil {
		return nil, err
	}
	upstream, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return nil, err
	}
	tokens, err := newPairTokenLoader(tokenFile)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if cfg.UpstreamSocket != "" {
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", cfg.UpstreamSocket)
		}
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	gateway, err := t3gateway.New(cfg, func(ctx context.Context, encoded, _ string) error {
		var identity service.T3BrowserSession
		if err := json.Unmarshal([]byte(encoded), &identity); err != nil {
			return service.ErrInvalidToken
		}
		return validateIdentity(ctx, identity)
	}, func(ctx context.Context) (string, error) {
		token, err := tokens.get()
		if err != nil {
			return "", errors.New("workspace pairing token is unavailable")
		}
		return createNativeT3Pairing(ctx, client, cfg.UpstreamURL, token)
	})
	if err != nil {
		return nil, err
	}
	return &nativeT3Workspace{
		gateway:        gateway,
		public:         public,
		admin:          admin,
		upstream:       upstream,
		upstreamSocket: cfg.UpstreamSocket,
		tokens:         tokens,
		inspect:        inspect,
	}, nil
}

func readNativeT3PairToken(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	const maxTokenBytes = 16 << 10
	raw, err := io.ReadAll(io.LimitReader(file, maxTokenBytes+1))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if len(raw) > maxTokenBytes || token == "" || strings.ContainsAny(token, " \r\n\t") {
		return "", errors.New("invalid workspace pairing service token")
	}
	return token, nil
}

func createNativeT3Pairing(ctx context.Context, client *http.Client, upstream, token string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"label": "MILE admin gateway",
		"scopes": []string{
			"orchestration:read", "orchestration:operate", "settings:write", "providers:manage", "preview:operate",
			"diagnostics:read", "terminal:read", "terminal:operate", "source-control:write", "filesystem:read", "filesystem:write",
		},
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(upstream, "/")+"/api/auth/pairing-token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	res, err := client.Do(request)
	if err != nil {
		return "", errors.New("workspace pairing service is unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", errors.New("workspace pairing service rejected the request")
	}
	var result struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&result); err != nil || result.Credential == "" {
		return "", errors.New("invalid workspace pairing response")
	}
	return result.Credential, nil
}

func (n *nativeT3Workspace) serveOrigin(c *gin.Context) {
	if strings.EqualFold(c.Request.Host, n.public.Host) {
		c.Abort()
		n.gateway.ServeHTTP(c.Writer, c.Request)
		return
	}
	c.Next()
}

func (n *nativeT3Workspace) formAction() string {
	if n == nil {
		return ""
	}
	return n.public.Scheme + "://" + n.public.Host
}

func (n *nativeT3Workspace) registerTicket(r *gin.Engine, adminAuth middleware.AdminAuthMiddleware, auditLog middleware.AuditLogMiddleware, settings *service.SettingService, redisClient *redis.Client) {
	limiter := middleware.NewPanelRateLimiter(redisClient, settings)
	r.POST("/api/v1/admin/development-workspace/native-ticket", gin.HandlerFunc(adminAuth), limiter.Global(),
		gin.HandlerFunc(auditLog), middleware.AdminComplianceGuard(settings), n.issueTicket)
	r.GET("/api/v1/admin/development-workspace/native-status", gin.HandlerFunc(adminAuth), limiter.Global(),
		middleware.AdminComplianceGuard(settings), n.nativeStatus)
}

func (n *nativeT3Workspace) nativeStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.GetString("auth_method") != service.AuditAuthMethodJWT {
		response.Forbidden(c, "An administrator browser session is required")
		return
	}
	_, tokenErr := n.tokens.get()
	response.Success(c, gin.H{
		"configured":       true,
		"token_loaded":     tokenErr == nil,
		"runner_reachable": n.probeRunner(c.Request.Context()),
	})
}

func (n *nativeT3Workspace) probeRunner(ctx context.Context) bool {
	dialer := &net.Dialer{Timeout: 1500 * time.Millisecond}
	var (
		conn net.Conn
		err  error
	)
	if n.upstreamSocket != "" {
		conn, err = dialer.DialContext(ctx, "unix", n.upstreamSocket)
	} else {
		host := n.upstream.Host
		if !strings.Contains(host, ":") {
			if n.upstream.Scheme == "https" {
				host += ":443"
			} else {
				host += ":80"
			}
		}
		conn, err = dialer.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (n *nativeT3Workspace) issueTicket(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.GetString("auth_method") != service.AuditAuthMethodJWT {
		response.Forbidden(c, "An administrator browser session is required")
		return
	}
	if !strings.EqualFold(c.Request.Host, n.admin.Host) || c.GetHeader("Origin") != n.admin.Scheme+"://"+n.admin.Host {
		response.Forbidden(c, "Request must come from the MILE administrator origin")
		return
	}
	authorization := strings.Fields(c.GetHeader("Authorization"))
	if len(authorization) != 2 || !strings.EqualFold(authorization[0], "Bearer") {
		response.Unauthorized(c, "Sign in again to open the workspace")
		return
	}
	identity, err := n.inspect(c.Request.Context(), authorization[1])
	if err != nil {
		response.Unauthorized(c, "Sign in again to open the workspace")
		return
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "Development workspace unavailable")
		return
	}
	var nextPath string
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		var payload struct {
			Next string `json:"next"`
		}
		if err := json.NewDecoder(io.LimitReader(c.Request.Body, 2048)).Decode(&payload); err == nil {
			nextPath = t3gateway.SanitizeNextPath(payload.Next)
		}
	}
	ticket, err := n.gateway.IssueWithNext(c.Request.Context(), string(encoded), c.Request.UserAgent(), nextPath)
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "Development workspace unavailable")
		return
	}
	response.Success(c, ticket)
}
