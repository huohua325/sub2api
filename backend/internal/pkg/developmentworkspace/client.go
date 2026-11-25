// Package developmentworkspace adapts a private T3 Code environment to the admin task API.
package developmentworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	// ErrDisabled means the operator has not configured a workspace.
	ErrDisabled = errors.New("development workspace is disabled")
	// ErrUnavailable means the configured T3 environment cannot complete a request.
	ErrUnavailable = errors.New("development workspace is unavailable")
	// ErrNotFound means there is no such task in the configured project.
	ErrNotFound = errors.New("development workspace task was not found")
	// ErrInvalid means the request or operator configuration is invalid.
	ErrInvalid = errors.New("invalid development workspace request")
	// ErrConflict means the current task state does not permit the requested action.
	ErrConflict = errors.New("development workspace task cannot be changed")
)

const (
	maxPromptBytes = 24000
	maxRequestID   = 128
	fullAccess     = "full-access"
	latestPosition = 9007199254740991
)

var (
	requestKey = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	t3ID       = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:-]{0,127}$`)
)

// Config selects one T3 project and a Codex provider instance in that environment.
// Token is an OAuth access token issued to an outside MCP client with Full access.
type Config struct {
	URL           string
	Token         string
	ProjectID     string
	DefaultBranch string
	Provider      string
	Model         string
}

// Status describes the configured development workspace.
type Status struct {
	Enabled       bool   `json:"enabled"`
	Project       string `json:"project,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	Model         string `json:"model,omitempty"`
}

// Task is the stable task reference and latest known turn state from T3.
type Task struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Branch    string `json:"branch,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// Message is a persisted user prompt or assistant reply.
type Message struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

// Activity summarizes an execution, workspace change, or provider error.
type Activity struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

// Change is a file in the latest checkpoint that records changes.
type Change struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// Detail contains the latest messages, activity, and checkpoint summary.
type Detail struct {
	Task
	Messages []Message  `json:"messages"`
	Activity []Activity `json:"activity"`
	Changes  []Change   `json:"changes,omitempty"`
}

// CreateInput starts a new worktree-backed task. RequestID is stable across retries.
type CreateInput struct {
	Message   string `json:"message"`
	Branch    string `json:"branch,omitempty"`
	RequestID string `json:"request_id"`
}

// SendInput adds a prompt to an existing task. RequestID is stable across retries.
type SendInput struct {
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// Client is a concurrency-safe adapter for the fixed T3 environment.
type Client struct {
	config Config
	http   *http.Client
}

// New constructs a T3 adapter. An empty config leaves the workspace disabled.
func New(config Config) (*Client, error) {
	if config == (Config{}) {
		return &Client{}, nil
	}
	endpoint, err := url.Parse(config.URL)
	if err != nil || endpoint == nil || !validEndpoint(endpoint) {
		return nil, fmt.Errorf("%w: T3 URL must be an HTTPS /mcp endpoint or loopback HTTP /mcp", ErrInvalid)
	}
	if config.Token == "" || strings.ContainsAny(config.Token, " \r\n\t") ||
		!t3ID.MatchString(config.ProjectID) || !t3ID.MatchString(config.Provider) ||
		strings.TrimSpace(config.Model) != config.Model || config.Model == "" || len(config.Model) > 256 ||
		!validBranch(config.DefaultBranch) {
		return nil, fmt.Errorf("%w: T3 token, project, branch, provider, and model are required", ErrInvalid)
	}
	return &Client{config: config, http: &http.Client{
		Timeout: 25 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}, nil
}

func validEndpoint(endpoint *url.URL) bool {
	if endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Opaque != "" ||
		endpoint.Path != "/mcp" || endpoint.RawPath != "" || endpoint.RawQuery != "" ||
		endpoint.Fragment != "" || endpoint.ForceQuery {
		return false
	}
	if endpoint.Scheme == "https" {
		return true
	}
	return endpoint.Scheme == "http" && (endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "::1")
}

func validBranch(branch string) bool {
	if branch == "" || len(branch) > 200 || strings.Contains(branch, "..") ||
		strings.ContainsAny(branch, " \\~^:?*[\x7f") || strings.HasPrefix(branch, "-") {
		return false
	}
	for _, ch := range branch {
		if ch < ' ' {
			return false
		}
	}
	for part := range strings.SplitSeq(branch, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return branch != "@" && !strings.Contains(branch, "@{")
}

func validPrompt(message string, requestID string) bool {
	return len(message) > 0 && len(message) <= maxPromptBytes && utf8.ValidString(message) &&
		len(requestID) <= maxRequestID && requestKey.MatchString(requestID)
}

// Status checks the configured project and the selected Codex provider instance.
func (c *Client) Status(ctx context.Context) (Status, error) {
	if c.http == nil {
		return Status{}, nil
	}
	status := Status{Enabled: true, DefaultBranch: c.config.DefaultBranch, Model: c.config.Model}
	var project struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := c.call(ctx, "t3_project_read", values{"projectId": c.config.ProjectID}, &project); err != nil {
		return status, err
	}
	if project.ID != c.config.ProjectID || strings.TrimSpace(project.Title) == "" {
		return status, fmt.Errorf("%w: T3 project configuration", ErrUnavailable)
	}
	status.Project = project.Title
	return status, c.checkCodex(ctx)
}

func (c *Client) checkCodex(ctx context.Context) error {
	var result struct {
		RuntimeMode string `json:"runtimeMode"`
		Providers   []struct {
			InstanceID string `json:"providerInstanceId"`
			Driver     string `json:"driverKind"`
			CanRun     bool   `json:"canRunChildTask"`
			Models     []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := c.call(ctx, "orchestrator_capabilities", values{"search": c.config.Model}, &result); err != nil {
		return err
	}
	if result.RuntimeMode != fullAccess {
		return fmt.Errorf("%w: T3 MCP client requires Full access", ErrUnavailable)
	}
	for _, provider := range result.Providers {
		if provider.InstanceID == c.config.Provider && provider.Driver == "codex" && provider.CanRun {
			for _, model := range provider.Models {
				if model.ID == c.config.Model {
					return nil
				}
			}
			return fmt.Errorf("%w: configured Codex model is unavailable", ErrUnavailable)
		}
	}
	return fmt.Errorf("%w: configured Codex provider is unavailable", ErrUnavailable)
}

type t3Thread struct {
	ID        string `json:"threadId"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Provider  string `json:"providerInstanceId"`
	Mode      string `json:"runtimeMode"`
	Branch    string `json:"branch"`
	Worktree  string `json:"worktreePath"`
	ParentID  string `json:"parentThreadId"`
	UpdatedAt string `json:"updatedAt"`
	Source    string `json:"creationSource"`
}

func (t t3Thread) task() Task {
	return Task{ID: t.ID, Title: t.Title, Status: t.Status, Branch: t.Branch, UpdatedAt: t.UpdatedAt}
}

func (c *Client) belongs(t t3Thread) bool {
	return t.ProjectID == c.config.ProjectID && t.Provider == c.config.Provider && t.ParentID == "" &&
		t.Mode == fullAccess && t.Source == "mcp" && t3ID.MatchString(t.ID)
}

func (t t3Thread) visibleWithoutWorktree() bool {
	if !strings.HasPrefix(t.ID, "mcp:launch:") {
		return false
	}
	switch t.Status {
	case "preparing", "queued", "starting", "interrupted", "cancelled", "failed", "rolled_back":
		return true
	default:
		return false
	}
}

// List returns the latest top-level Codex tasks for the configured project.
func (c *Client) List(ctx context.Context) ([]Task, error) {
	var result struct {
		Threads []t3Thread `json:"threads"`
	}
	if err := c.call(ctx, "t3_thread_list", values{"projectId": c.config.ProjectID, "limit": 100}, &result); err != nil {
		return nil, err
	}
	if result.Threads == nil {
		return nil, fmt.Errorf("%w: malformed T3 thread list", ErrUnavailable)
	}
	tasks := make([]Task, 0, len(result.Threads))
	for _, thread := range result.Threads {
		if c.belongs(thread) {
			tasks = append(tasks, thread.task())
		}
	}
	return tasks, nil
}

// Create launches Codex in a newly created worktree for the selected base branch.
func (c *Client) Create(ctx context.Context, input CreateInput) (Task, error) {
	message := strings.TrimSpace(input.Message)
	branch := input.Branch
	if branch == "" {
		branch = c.config.DefaultBranch
	}
	if c.http == nil {
		return Task{}, ErrDisabled
	}
	if !validPrompt(message, input.RequestID) || !validBranch(branch) {
		return Task{}, ErrInvalid
	}
	if err := c.checkCodex(ctx); err != nil {
		return Task{}, err
	}
	title := taskTitle(message)
	var launched struct {
		ThreadID  string `json:"threadId"`
		ProjectID string `json:"projectId"`
		Status    string `json:"status"`
	}
	err := c.call(ctx, "t3_thread_launch", values{
		"projectId": c.config.ProjectID, "title": title, "message": message,
		"modelSelection":    values{"instanceId": c.config.Provider, "model": c.config.Model},
		"runtimeMode":       fullAccess,
		"interactionMode":   "default",
		"workspaceStrategy": values{"type": "worktree", "baseRef": branch},
		"clientRequestId":   input.RequestID,
	}, &launched)
	if err != nil {
		return Task{}, err
	}
	if !t3ID.MatchString(launched.ThreadID) || launched.ProjectID != c.config.ProjectID || launched.Status == "" {
		return Task{}, fmt.Errorf("%w: malformed T3 launch result", ErrUnavailable)
	}
	return Task{ID: launched.ThreadID, Title: title, Status: launched.Status}, nil
}

func taskTitle(message string) string {
	first, _, _ := strings.Cut(message, "\n")
	first = strings.TrimSpace(first)
	characters := []rune(first)
	if len(characters) > 100 {
		return strings.TrimSpace(string(characters[:100])) + "..."
	}
	return first
}

type t3Item struct {
	Position      int    `json:"position"`
	ID            string `json:"itemId"`
	Type          string `json:"type"`
	Title         string `json:"title"`
	Text          string `json:"text"`
	TextTruncated bool   `json:"textTruncated"`
	UpdatedAt     string `json:"updatedAt"`
}

type t3Read struct {
	Thread t3Thread `json:"thread"`
	Items  []t3Item `json:"items"`
}

func (c *Client) read(ctx context.Context, id, view string, limit int, allowWithoutWorktree bool) (t3Read, error) {
	var result t3Read
	if !t3ID.MatchString(id) {
		return result, ErrNotFound
	}
	if err := c.call(ctx, "t3_thread_read", values{
		"threadId": id, "view": view, "limit": limit, "runLimit": 1,
		"beforePosition": latestPosition, "maxCharsPerItem": 16000,
	}, &result); err != nil {
		return result, err
	}
	if result.Thread.ID != id || !c.belongs(result.Thread) {
		return t3Read{}, ErrNotFound
	}
	if result.Thread.Worktree == "" && !(allowWithoutWorktree && result.Thread.visibleWithoutWorktree()) {
		return t3Read{}, ErrNotFound
	}
	return result, nil
}

// Get reads the latest messages, activity, and checkpoint summary for a scoped task.
func (c *Client) Get(ctx context.Context, id string) (Detail, error) {
	messages, err := c.read(ctx, id, "messages", 60, true)
	if err != nil {
		return Detail{}, err
	}
	activity, err := c.read(ctx, id, "activity", 60, true)
	if err != nil {
		return Detail{}, err
	}
	sort.Slice(messages.Items, func(i, j int) bool { return messages.Items[i].Position < messages.Items[j].Position })
	sort.Slice(activity.Items, func(i, j int) bool { return activity.Items[i].Position < activity.Items[j].Position })
	detail := Detail{Task: activity.Thread.task(), Messages: []Message{}, Activity: []Activity{}}
	for _, item := range messages.Items {
		if item.Type == "user_message" || item.Type == "assistant_message" {
			role := strings.TrimSuffix(item.Type, "_message")
			detail.Messages = append(detail.Messages, Message{ID: item.ID, Role: role, Text: item.Text, CreatedAt: item.UpdatedAt})
		}
	}
	for _, item := range activity.Items {
		switch item.Type {
		case "checkpoint":
			// A read-only turn creates an empty checkpoint. Keep the last recorded file changes.
			var changes []Change
			if !item.TextTruncated && json.Unmarshal([]byte(item.Text), &changes) == nil && len(changes) > 0 {
				detail.Changes = changes
			}
		case "command_execution", "file_change", "error", "system_notice", "approval_request", "user_input_request":
			label := item.Text
			if label == "" {
				label = item.Title
			}
			if label != "" {
				detail.Activity = append(detail.Activity, Activity{ID: item.ID, Text: label, CreatedAt: item.UpdatedAt})
			}
		}
	}
	return detail, nil
}

// Send forwards a follow-up while preserving T3's normal delivery behavior.
func (c *Client) Send(ctx context.Context, id string, input SendInput) error {
	message := strings.TrimSpace(input.Message)
	if !validPrompt(message, input.RequestID) {
		return ErrInvalid
	}
	if _, err := c.read(ctx, id, "messages", 1, false); err != nil {
		return err
	}
	return c.call(ctx, "t3_thread_send", values{
		"threadId": id, "message": message, "mode": "auto", "clientRequestId": input.RequestID,
	}, nil)
}

// Stop requests cancellation of the current task and holds queued messages.
func (c *Client) Stop(ctx context.Context, id string) error {
	return c.StopWithRequestID(ctx, id, "")
}

// StopWithRequestID allows a caller to retry the same stop request after losing its response.
func (c *Client) StopWithRequestID(ctx context.Context, id string, requestID string) error {
	if requestID != "" && !requestKey.MatchString(requestID) {
		return ErrInvalid
	}
	if _, err := c.read(ctx, id, "messages", 1, true); err != nil {
		return err
	}
	params := values{"threadId": id, "stop": true}
	if requestID != "" {
		params["clientRequestId"] = requestID
	}
	return c.call(ctx, "t3_thread_interrupt", params, nil)
}
