package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGlobalAICoordinatorSharesConcurrentFetchAndPages(t *testing.T) {
	original := globalAILogFetch
	t.Cleanup(func() { globalAILogFetch = original })
	var calls atomic.Int32
	globalAILogFetch = func(context.Context, string, string) ([]globalAIConsumeLog, error) {
		calls.Add(1)
		return []globalAIConsumeLog{{Type: 2, Quota: 50, RequestID: "receipt"}}, nil
	}
	c := newGlobalAILogCoordinator()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.load(context.Background(), "private-key", "https://globalai.vip/api/log/token")
			var deferred *globalAILogDeferred
			if err != nil && !errors.As(err, &deferred) {
				t.Errorf("lookup: %v", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent lookups made %d supplier calls", calls.Load())
	}
	logs, err := c.load(context.Background(), "private-key", "https://globalai.vip/api/log/token")
	if err != nil || len(logs) != 1 || logs[0].Quota != 50 || calls.Load() != 1 {
		t.Fatalf("cached page: logs=%v err=%v calls=%d", logs, err, calls.Load())
	}
	if _, err := c.load(context.Background(), "another-key", "https://globalai.vip/api/log/token"); err == nil || calls.Load() != 1 {
		t.Fatal("a second key bypassed the shared egress rate gate")
	}
}

func TestGlobalAICoordinatorHonors429AcrossKeys(t *testing.T) {
	original := globalAILogFetch
	t.Cleanup(func() { globalAILogFetch = original })
	c := newGlobalAILogCoordinator()
	now := time.Now()
	c.now = func() time.Time { return now }
	calls := 0
	globalAILogFetch = func(context.Context, string, string) ([]globalAIConsumeLog, error) {
		calls++
		return nil, &globalAILogHTTPError{status: 429, retryAfter: 10 * time.Minute}
	}
	_, _ = c.load(context.Background(), "first-key", "https://globalai.vip/api/log/token")
	now = now.Add(6 * time.Minute)
	_, err := c.load(context.Background(), "second-key", "https://globalai.vip/api/log/token")
	var deferred *globalAILogDeferred
	if !errors.As(err, &deferred) || calls != 1 {
		t.Fatalf("429 was retried before Retry-After: %v, calls=%d", err, calls)
	}
	now = now.Add(5 * time.Minute)
	_, _ = c.load(context.Background(), "second-key", "https://globalai.vip/api/log/token")
	if calls != 2 {
		t.Fatal("supplier lookup did not resume after backoff")
	}
}

func TestGlobalAIHTTP429RetainsRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "900")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := fetchGlobalAILogsHTTP(context.Background(), "private-key", srv.URL)
	var failure *globalAILogHTTPError
	if !errors.As(err, &failure) || failure.status != 429 || failure.retryAfter != 15*time.Minute {
		t.Fatalf("lost supplier retry instruction: %v", err)
	}
}

func TestGlobalAIRejectsConflictingAndNegativeAmounts(t *testing.T) {
	for _, input := range []string{
		`{"data":[{"type":2,"quota":-1,"request_id":"receipt"}]}`,
		`{"data":[{"type":2,"quota":10,"request_id":"receipt"},{"type":2,"quota":20,"request_id":"receipt"}]}`,
	} {
		if _, err := parseGlobalAILogs([]byte(input)); err == nil {
			t.Fatalf("accepted ambiguous billing evidence: %s", input)
		}
	}
}
