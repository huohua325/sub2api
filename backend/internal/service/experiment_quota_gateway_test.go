package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type experimentAdmissionSpy struct {
	reserveErr error
	sendErr    error
	reserved   int
	sent       int
	released   int
	receipts   int
	requestID  string
	upstreamID string
	status     int
	completed  int
}

func (s *experimentAdmissionSpy) ReserveExperimentRequest(_ context.Context, in ExperimentAdmissionInput) error {
	s.reserved++
	s.requestID = in.RequestID
	return s.reserveErr
}
func (s *experimentAdmissionSpy) MarkExperimentRequestSent(context.Context, string, int64) error {
	s.sent++
	return s.sendErr
}
func (s *experimentAdmissionSpy) ReleaseExperimentRequest(context.Context, string, int64, string) error {
	s.released++
	return nil
}
func (s *experimentAdmissionSpy) RecordExperimentResponse(_ context.Context, _ string, _ int64, id string, status int) error {
	s.receipts++
	s.upstreamID, s.status = id, status
	return nil
}

func (s *experimentAdmissionSpy) CompleteExperimentRequest(context.Context, string, int64) error {
	s.completed++
	return nil
}

func experimentTestRequest(t *testing.T, store ExperimentAdmissionStore) (*http.Request, *gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "https://globalai.vip/v1/chat/completions", nil)
	h := &ExperimentRequestHandle{gin: c, store: store, input: ExperimentAdmissionInput{RequestID: "local:request", APIKeyID: 3, UserID: 3}}
	c.Set(experimentRequestHandleKey, h)
	c.Request = req.WithContext(context.WithValue(req.Context(), experimentRequestContextKey{}, h))
	return c.Request, c, w
}

func TestExperimentHTTPAdmissionStopsUnknownSpendBeforeSend(t *testing.T) {
	for _, reason := range []error{ErrExperimentAccountingPending, ErrExperimentRequestInFlight, ErrExperimentQuotaExhausted} {
		t.Run(reason.Error(), func(t *testing.T) {
			spy := &experimentAdmissionSpy{reserveErr: reason}
			req, c, w := experimentTestRequest(t, spy)
			if err := BeginExperimentHTTPAttempt(req, 8); !errors.Is(err, reason) {
				t.Fatalf("admission: %v", err)
			}
			if spy.sent != 0 || !c.IsAborted() || w.Code < 400 {
				t.Fatal("unknown or exhausted charges reached the send checkpoint")
			}
			FinishCodingRequest(c)
			if spy.released != 0 {
				t.Fatal("failed admission released an unrelated reservation")
			}
		})
	}
}

func TestExperimentSentAttemptCannotBeReplayedOrReleased(t *testing.T) {
	spy := &experimentAdmissionSpy{}
	req, c, _ := experimentTestRequest(t, spy)
	if err := BeginExperimentHTTPAttempt(req, 8); err != nil {
		t.Fatal(err)
	}
	resp := &http.Response{StatusCode: 403, Header: http.Header{globalAIRequestIDHeader: []string{"supplier-receipt"}}}
	RecordExperimentHTTPResponse(req, resp)
	if spy.receipts != 1 || spy.upstreamID != "supplier-receipt" || spy.status != 403 {
		t.Fatal("a refused or partial response lost its receipt identity")
	}
	if err := BeginExperimentHTTPAttempt(req, 13); !errors.Is(err, ErrExperimentAccountingPending) {
		t.Fatalf("sent attempt was retried on a different account: %v", err)
	}
	req.URL.Host = "another-provider.example"
	if err := BeginExperimentHTTPAttempt(req, 13); !errors.Is(err, ErrExperimentAccountingPending) {
		t.Fatalf("a mixed group replayed unknown spend through another provider: %v", err)
	}
	FinishCodingRequest(c)
	if spy.sent != 1 || spy.released != 0 || spy.completed != 1 {
		t.Fatal("a sent request was resent or its unknown cost was released")
	}
	if got := resolveUsageBillingRequestID(req.Context(), "supplier-response"); got != "local:request" {
		t.Fatalf("billing no longer uses the durable admission identity: %s", got)
	}
}

func TestExperimentUnsentAttemptCanRelease(t *testing.T) {
	spy := &experimentAdmissionSpy{sendErr: ErrExperimentAccountingUnavailable}
	req, c, _ := experimentTestRequest(t, spy)
	if err := BeginExperimentHTTPAttempt(req, 8); err == nil {
		t.Fatal("sent checkpoint failure was ignored")
	}
	if err := BeginExperimentHTTPAttempt(req, 13); !errors.Is(err, ErrExperimentAccountingUnavailable) || spy.sent != 1 {
		t.Fatal("an unsent reservation changed its supplier account on retry")
	}
	FinishCodingRequest(c)
	if spy.released != 1 {
		t.Fatal("unsent reservation was not released")
	}
}

func TestExperimentGuardLeavesOtherTrafficAlone(t *testing.T) {
	spy := &experimentAdmissionSpy{reserveErr: ErrExperimentAccountingUnavailable}
	req, c, _ := experimentTestRequest(t, spy)
	req.URL.Host = "api.example.test"
	if err := BeginExperimentHTTPAttempt(req, 8); err != nil {
		t.Fatal(err)
	}
	req.URL.Host = "globalai.vip"
	req.Method = http.MethodGet
	if err := BeginExperimentHTTPAttempt(req, 8); err != nil || spy.reserved != 0 || c.IsAborted() {
		t.Fatal("supplier discovery or another provider was blocked")
	}
	req.Method = http.MethodPost
	for _, path := range []string{"/v1/messages/count_tokens", "/v1beta/models/gemini:countTokens"} {
		req.URL.Path = path
		if err := BeginExperimentHTTPAttempt(req, 8); err != nil || spy.reserved != 0 {
			t.Fatal("a free token count acquired an unfinishable paid reservation")
		}
	}
}

func TestExperimentIndependentMoneyEventsKeepTheirIdentity(t *testing.T) {
	spy := &experimentAdmissionSpy{}
	req, _, _ := experimentTestRequest(t, spy)
	if err := BeginExperimentHTTPAttempt(req, 8); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"web_search:separate", "grok-video:separate", "grok_audio:separate"} {
		if got := resolveUsageBillingRequestID(req.Context(), id); got != id {
			t.Fatalf("collapsed a separate money event %s into %s", id, got)
		}
	}
}

func TestExperimentUnsupportedMoneyEventsStopBeforeReserve(t *testing.T) {
	for _, path := range []string{"/v1/alpha/search", "/v1/audio/transcriptions", "/v1/videos", "/v1beta/cachedContents"} {
		t.Run(path, func(t *testing.T) {
			spy := &experimentAdmissionSpy{}
			req, _, w := experimentTestRequest(t, spy)
			req.URL.Path = path
			if err := BeginExperimentHTTPAttempt(req, 8); !errors.Is(err, ErrExperimentProtocolUnsupported) || spy.reserved != 0 || spy.sent != 0 || w.Code != http.StatusBadRequest {
				t.Fatalf("untracked money event was admitted: %v", err)
			}
		})
	}
}
