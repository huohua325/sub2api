package requestdrain

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDrainWaitsForHTTPBillingAndRejectsNewRequests(t *testing.T) {
	d := New()
	entered := make(chan struct{})
	finish := make(chan struct{})
	returned := make(chan struct{})
	h := d.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-finish
		_, _ = io.WriteString(w, "billed")
	}))
	go func() {
		defer close(returned)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", nil))
	}()
	<-entered
	d.Begin()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/v1/responses", nil))
	require.Equal(t, 503, rr.Code)
	require.Equal(t, "5", rr.Header().Get("Retry-After"))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, d.Wait(ctx), context.DeadlineExceeded)
	close(finish)
	<-returned
	require.NoError(t, d.Wait(context.Background()))
}

type hijackWriter struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (w hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

func TestDrainLetsActiveWebSocketTurnBillThenClosesIdleConnection(t *testing.T) {
	d := New()
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()
	entered := make(chan struct{})
	finish := make(chan struct{})
	billed := make(chan struct{})
	returned := make(chan struct{})
	h := d.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done, err := BeginRound(r.Context())
		require.NoError(t, err)
		hijacker, ok := w.(http.Hijacker)
		require.True(t, ok)
		_, _, err = hijacker.Hijack()
		require.NoError(t, err)
		close(entered)
		<-finish
		_, err = BeginRound(r.Context())
		require.ErrorIs(t, err, ErrDraining)
		close(billed)
		done()
		done()
	}))
	go func() {
		defer close(returned)
		h.ServeHTTP(hijackWriter{httptest.NewRecorder(), server}, httptest.NewRequest("GET", "/v1/responses", nil))
	}()
	<-entered
	d.Begin()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, d.Wait(ctx), context.DeadlineExceeded)
	close(finish)
	<-billed
	<-returned
	require.NoError(t, d.Wait(context.Background()))
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	_, err := client.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
}

func TestDrainClosesManagedIdleWebSocketWithoutWaitingForGrace(t *testing.T) {
	d := New()
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()
	entered := make(chan struct{})
	returned := make(chan struct{})
	h := d.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done, err := BeginRound(r.Context())
		require.NoError(t, err)
		hijacker, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, err := hijacker.Hijack()
		require.NoError(t, err)
		done()
		close(entered)
		_, _ = conn.Read(make([]byte, 1))
	}))
	go func() {
		defer close(returned)
		h.ServeHTTP(hijackWriter{httptest.NewRecorder(), server}, httptest.NewRequest("GET", "/v1/responses", nil))
	}()
	<-entered
	d.Begin()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, d.Wait(ctx))
	<-returned
}

func TestDrainAbortCancelsHTTPAndStillWaitsForDetachedHandoff(t *testing.T) {
	d := New()
	entered := make(chan struct{})
	canceled := make(chan struct{})
	handoff := make(chan struct{})
	returned := make(chan struct{})
	h := d.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
		<-handoff
	}))
	go func() {
		defer close(returned)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", nil))
	}()
	<-entered
	d.Begin()
	d.Abort()
	<-canceled
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, d.Wait(ctx), context.DeadlineExceeded)
	close(handoff)
	<-returned
	require.NoError(t, d.Wait(context.Background()))
}

func TestShutdownServerWaitsForHandoffAfterGraceExpires(t *testing.T) {
	d := New()
	entered := make(chan struct{})
	canceled := make(chan struct{})
	handoff := make(chan struct{})
	srv := httptest.NewServer(d.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
		<-handoff
	})))
	defer srv.Close()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		resp, err := http.Get(srv.URL)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- d.ShutdownServer(ctx, srv.Config, time.Second) }()
	<-canceled
	select {
	case err := <-shutdownDone:
		t.Fatalf("dependencies stopped before billing handoff: %v", err)
	default:
	}
	close(handoff)
	require.ErrorIs(t, <-shutdownDone, context.DeadlineExceeded)
	<-clientDone
}

func TestDrainWriterPreservesCloseNotificationAndStreaming(t *testing.T) {
	d := New()
	entered := make(chan struct{})
	notified := make(chan struct{})
	h := d.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		notifier, ok := w.(interface{ CloseNotify() <-chan bool })
		require.True(t, ok)
		ch := notifier.CloseNotify()
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		flusher.Flush()
		close(entered)
		<-ch
		close(notified)
	}))
	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/stream", nil))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("streaming handler did not start")
	}
	d.Begin()
	d.Abort()
	select {
	case <-notified:
	case <-time.After(time.Second):
		t.Fatal("disconnect notification missing")
	}
	require.NoError(t, d.Wait(context.Background()))
}

func TestShutdownServerServesRetryResponseWhileExistingRequestDrains(t *testing.T) {
	d := New()
	entered := make(chan struct{})
	finish := make(chan struct{})
	firstDone := make(chan struct{})
	srv := httptest.NewServer(d.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-finish })))
	defer srv.Close()
	go func() {
		defer close(firstDone)
		resp, err := http.Get(srv.URL)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered
	d.Begin()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- d.ShutdownServer(ctx, srv.Config, time.Second) }()
	// A second connection must receive an actionable response throughout drain.
	time.Sleep(20 * time.Millisecond)
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Errorf("drain must return retry guidance: %v", err)
	} else {
		require.Equal(t, 503, resp.StatusCode)
		require.Equal(t, "5", resp.Header.Get("Retry-After"))
		_ = resp.Body.Close()
	}
	close(finish)
	<-firstDone
	require.NoError(t, <-shutdownDone)
}
