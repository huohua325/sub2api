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
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/t3gateway"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type nativeT3Workspace struct {
	gateway  *t3gateway.Gateway
	public   *url.URL
	admin    *url.URL
	validate func(context.Context, string) error
}

func configureNativeT3(auth *service.AuthService) *nativeT3Workspace {
	if auth == nil || strings.TrimSpace(os.Getenv("DEV_T3_NATIVE_PUBLIC_URL")) == "" {
		return nil
	}
	cfg := t3gateway.Config{
		PublicURL:      strings.TrimSpace(os.Getenv("DEV_T3_NATIVE_PUBLIC_URL")),
		UpstreamURL:    strings.TrimSpace(os.Getenv("DEV_T3_NATIVE_UPSTREAM_URL")),
		AdminOrigin:    strings.TrimSpace(os.Getenv("DEV_T3_NATIVE_ADMIN_ORIGIN")),
		UpstreamSocket: strings.TrimSpace(os.Getenv("DEV_T3_NATIVE_UPSTREAM_SOCKET_FILE")),
	}
	native, err := newNativeT3Workspace(cfg, os.Getenv("DEV_T3_NATIVE_PAIR_TOKEN_FILE"), auth.ValidateT3BrowserSession)
	if err != nil {
		log.Print("Native T3 workspace disabled: invalid gateway or pairing configuration")
		return nil
	}
	return native
}

func newNativeT3Workspace(cfg t3gateway.Config, tokenFile string, validate func(context.Context, string) error) (*nativeT3Workspace, error) {
	public, err := url.Parse(cfg.PublicURL)
	if err != nil {
		return nil, err
	}
	admin, err := url.Parse(cfg.AdminOrigin)
	if err != nil {
		return nil, err
	}
	token, err := readNativeT3PairToken(tokenFile)
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
	gateway, err := t3gateway.New(cfg, func(ctx context.Context, bearer, _ string) error {
		return validate(ctx, bearer)
	}, func(ctx context.Context) (string, error) {
		return createNativeT3Pairing(ctx, client, cfg.UpstreamURL, token)
	})
	if err != nil {
		return nil, err
	}
	return &nativeT3Workspace{gateway: gateway, public: public, admin: admin, validate: validate}, nil
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
		return "", errors.New("invalid T3 pairing service token")
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
		return "", errors.New("T3 pairing service is unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", errors.New("T3 pairing service rejected the request")
	}
	var result struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&result); err != nil || result.Credential == "" {
		return "", errors.New("invalid T3 pairing response")
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
	if len(authorization) != 2 || !strings.EqualFold(authorization[0], "Bearer") ||
		n.validate(c.Request.Context(), authorization[1]) != nil {
		response.Unauthorized(c, "Sign in again to open the workspace")
		return
	}
	ticket, err := n.gateway.Issue(authorization[1], c.Request.UserAgent())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "T3 workspace unavailable")
		return
	}
	response.Success(c, ticket)
}
