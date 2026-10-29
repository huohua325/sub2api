// Package requestdrain coordinates HTTP handlers, hijacked connections and
// logical WebSocket turns before billing workers and database clients stop.
package requestdrain

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

var ErrDraining = errors.New("SERVER_DRAINING")

type contextKey struct{}
type request struct {
	owner   *Controller
	cancel  context.CancelFunc
	ctx     context.Context
	conn    net.Conn
	rounds  int
	managed bool
}

type Controller struct {
	mu       sync.Mutex
	draining bool
	requests map[*request]struct{}
	done     chan struct{}
	once     sync.Once
}

func New() *Controller {
	return &Controller{requests: make(map[*request]struct{}), done: make(chan struct{})}
}

func (d *Controller) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		s := &request{owner: d, cancel: cancel, ctx: ctx}
		d.mu.Lock()
		if d.draining {
			d.mu.Unlock()
			cancel()
			w.Header().Set("Retry-After", "5")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"type":"SERVER_DRAINING","code":"SERVER_DRAINING","message":"Service restarting; retry shortly"}}`))
			return
		}
		d.requests[s] = struct{}{}
		d.mu.Unlock()
		defer func() { cancel(); d.mu.Lock(); delete(d.requests, s); d.signalLocked(); d.mu.Unlock() }()
		next.ServeHTTP(&writer{ResponseWriter: w, request: s}, r.WithContext(context.WithValue(ctx, contextKey{}, s)))
	})
}

func (d *Controller) signalLocked() {
	if d.draining && len(d.requests) == 0 {
		d.once.Do(func() { close(d.done) })
	}
}

// Begin rejects new HTTP requests and new logical turns. Unmanaged upgraded
// protocols retain the grace period; managed idle connections close immediately.
func (d *Controller) Begin() {
	d.mu.Lock()
	d.draining = true
	var idle []*request
	for r := range d.requests {
		if r.conn != nil && r.managed && r.rounds == 0 {
			idle = append(idle, r)
		}
	}
	d.signalLocked()
	d.mu.Unlock()
	for _, r := range idle {
		r.cancel()
		_ = r.conn.Close()
	}
}
func (d *Controller) Wait(ctx context.Context) error {
	select {
	case <-d.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ShutdownServer keeps billing dependencies alive through both the normal
// grace period and a bounded handoff period after forced cancellation.
func (d *Controller) ShutdownServer(ctx context.Context, srv *http.Server, handoff time.Duration) error {
	d.Begin()
	// Keep the listener open during drain so clients receive 503/Retry-After
	// instead of an unexplained proxy connection failure.
	waitErr := d.Wait(ctx)
	if waitErr != nil {
		d.Abort()
		_ = srv.Close()
	}
	httpErr := srv.Shutdown(ctx)
	handoffCtx, cancel := context.WithTimeout(context.Background(), handoff)
	defer cancel()
	return errors.Join(waitErr, httpErr, d.Wait(handoffCtx))
}

// Abort ends upstream execution at the grace deadline. Handlers remain tracked
// until their detached, bounded billing handoff and deferred cleanup finish.
func (d *Controller) Abort() {
	type stop struct {
		cancel context.CancelFunc
		conn   net.Conn
	}
	d.mu.Lock()
	pending := make([]stop, 0, len(d.requests))
	for r := range d.requests {
		pending = append(pending, stop{r.cancel, r.conn})
	}
	d.mu.Unlock()
	for _, r := range pending {
		r.cancel()
		if r.conn != nil {
			_ = r.conn.Close()
		}
	}
}

// BeginRound is intentionally independent of quota mode, provider and store.
// Call its idempotent completion only AFTER usage handoff and turn cleanup.
func BeginRound(ctx context.Context) (func(), error) {
	noop := func() {}
	s, _ := ctx.Value(contextKey{}).(*request)
	if s == nil {
		return noop, nil
	}
	d := s.owner
	d.mu.Lock()
	if d.draining || ctx.Err() != nil {
		d.mu.Unlock()
		return noop, ErrDraining
	}
	s.managed = true
	s.rounds++
	d.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			d.mu.Lock()
			s.rounds--
			closeIdle := d.draining && s.rounds == 0 && s.conn != nil
			conn := s.conn
			d.mu.Unlock()
			if closeIdle {
				s.cancel()
				_ = conn.Close()
			}
		})
	}, nil
}

type writer struct {
	http.ResponseWriter
	request *request
}

func (w *writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *writer) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }

// Gin retains the legacy CloseNotifier contract for wrapped stream writers.
func (w *writer) CloseNotify() <-chan bool {
	if notifier, ok := w.ResponseWriter.(interface{ CloseNotify() <-chan bool }); ok {
		return notifier.CloseNotify()
	}
	ch := make(chan bool, 1)
	go func() { <-w.request.ctx.Done(); ch <- true }()
	return ch
}
func (w *writer) Push(target string, options *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}
func (w *writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	s := w.request
	d := s.owner
	d.mu.Lock()
	s.conn = conn
	closeIdle := d.draining && s.managed && s.rounds == 0
	d.mu.Unlock()
	if closeIdle {
		s.cancel()
		_ = conn.Close()
	}
	return conn, rw, nil
}
