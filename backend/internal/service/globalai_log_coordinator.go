package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type globalAILogDeferred struct{ until time.Time }

func (e *globalAILogDeferred) Error() string {
	return "GlobalAI billing logs are awaiting the next rate-limited lookup"
}

type globalAILogHTTPError struct {
	status     int
	retryAfter time.Duration
}

func (e *globalAILogHTTPError) Error() string {
	return fmt.Sprintf("globalai log api returned %d", e.status)
}

type globalAILogPage struct {
	logs []globalAIConsumeLog
	at   time.Time
}

// globalAILogCoordinator bounds supplier billing traffic across all keys on
// this process's shared egress. Unknown charges remain durable while deferred.
type globalAILogCoordinator struct {
	mu       sync.Mutex
	pages    map[string]globalAILogPage
	next     map[string]time.Time
	now      func() time.Time
	interval time.Duration
	cacheTTL time.Duration
	backoff  time.Duration
}

func newGlobalAILogCoordinator() *globalAILogCoordinator {
	return &globalAILogCoordinator{
		pages: map[string]globalAILogPage{}, next: map[string]time.Time{}, now: time.Now,
		interval: 10 * time.Second, cacheTTL: 10 * time.Second, backoff: 5 * time.Minute,
	}
}

var globalAILogs = newGlobalAILogCoordinator()

func (c *globalAILogCoordinator) load(ctx context.Context, apiKey, logURL string) ([]globalAIConsumeLog, error) {
	digest := sha256.Sum256([]byte(logURL + "\x00" + apiKey))
	key := hex.EncodeToString(digest[:])
	u, err := url.Parse(logURL)
	if err != nil {
		return nil, err
	}
	origin := strings.ToLower(u.Scheme + "://" + u.Host)
	c.mu.Lock()
	now := c.now()
	page, exists := c.pages[key]
	if exists && now.Sub(page.at) < c.cacheTTL {
		c.mu.Unlock()
		return page.logs, nil
	}
	if next := c.next[origin]; now.Before(next) {
		c.mu.Unlock()
		return nil, &globalAILogDeferred{until: next}
	}
	// Reserve before doing I/O so concurrent requests cannot stampede the API.
	c.next[origin] = now.Add(c.interval)
	c.mu.Unlock()
	logs, err := globalAILogFetch(ctx, apiKey, logURL)
	c.mu.Lock()
	defer c.mu.Unlock()
	now = c.now()
	if err != nil {
		var failure *globalAILogHTTPError
		if errors.As(err, &failure) && failure.status == http.StatusTooManyRequests {
			delay := c.backoff
			if failure.retryAfter > delay {
				delay = failure.retryAfter
			}
			c.next[origin] = now.Add(delay)
		}
		return nil, err
	}
	if len(c.pages) >= 256 {
		// Cache eviction never removes durable charges or settlement evidence.
		for id, old := range c.pages {
			if now.Sub(old.at) >= c.cacheTTL {
				delete(c.pages, id)
			}
		}
	}
	c.pages[key] = globalAILogPage{logs: logs, at: now}
	return logs, nil
}

func globalAIRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && seconds > 0 && seconds <= 86400 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
