package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	coderws "github.com/coder/websocket"
)

func TestExperimentProtocolGuardUsesSelectedSupplierAndPurpose(t *testing.T) {
	for _, test := range []struct {
		name, origin, usage, protocol string
		blocked                       bool
	}{
		{"GlobalAI websocket", "https://globalai.vip/v1", UsageKindExperiment, "websocket", true},
		{"GlobalAI batch", "https://globalai.vip/v1", UsageKindExperiment, "batch", true},
		{"GlobalAI async", "https://globalai.vip/v1", UsageKindExperiment, "asynchronous-image", true},
		{"GlobalAI ordinary HTTP", "https://globalai.vip/v1", UsageKindExperiment, "", false},
		{"other supplier websocket", "https://api.example.invalid/v1", UsageKindExperiment, "websocket", false},
		{"coding semantics", "https://globalai.vip/v1", UsageKindCoding, "websocket", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Credentials: map[string]any{"base_url": test.origin}}
			group := &Group{Platform: PlatformOpenAI, UsageKind: test.usage}
			_, c, w := experimentTestRequest(t, &experimentAdmissionSpy{})
			err := ValidateGlobalAIExperimentProtocol(context.Background(), c, account, group, test.protocol)
			if errors.Is(err, ErrExperimentProtocolUnsupported) != test.blocked {
				t.Fatalf("protocol result: %v", err)
			}
			if test.blocked && (!c.IsAborted() || w.Code != http.StatusBadRequest) {
				t.Fatal("unsupported protocol did not return its explicit local error")
			}
		})
	}
}

func TestExperimentDeferredProtocolIsCheckedAfterAccountSelection(t *testing.T) {
	ctx := WithExperimentDeferredProtocol(context.Background(), "asynchronous-image")
	group := &Group{Platform: PlatformOpenAI, UsageKind: UsageKindExperiment}
	if err := ValidateGlobalAIExperimentProtocol(ctx, nil, &Account{Credentials: map[string]any{"base_url": "https://api.example.invalid"}}, group, ""); err != nil {
		t.Fatal("another asynchronous supplier was changed")
	}
	if err := ValidateGlobalAIExperimentProtocol(ctx, nil, &Account{Credentials: map[string]any{"base_url": "https://globalai.vip/v1"}}, group, ""); !errors.Is(err, ErrExperimentProtocolUnsupported) {
		t.Fatal("GlobalAI deferred generation bypassed the guard")
	}
}

func TestExperimentTransportFailuresNeverRetryOrEvictAccounts(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic", "gemini"} {
		for _, sent := range []bool{false, true} {
			t.Run(protocol+map[bool]string{false: "/local-admission", true: "/unknown-network"}[sent], func(t *testing.T) {
				req, c, w := experimentTestRequest(t, &experimentAdmissionSpy{})
				original := error(ErrExperimentAlreadySent)
				if sent {
					if err := BeginExperimentHTTPAttempt(req, 8); err != nil {
						t.Fatal(err)
					}
					original = io.ErrUnexpectedEOF
				}
				account := &Account{ID: 8, Platform: PlatformOpenAI}
				var result error
				switch protocol {
				case "openai":
					result = (&OpenAIGatewayService{}).handleOpenAIUpstreamTransportError(req.Context(), c, account, original, false)
				case "anthropic":
					result = (&GatewayService{}).handleUpstreamTransportError(req.Context(), c, account, original, OpsUpstreamErrorEvent{})
				case "gemini":
					result = (&GeminiMessagesCompatService{}).handleUpstreamTransportError(req.Context(), c, account, original)
				}
				var failover *UpstreamFailoverError
				if result != original || errors.As(result, &failover) || w.Body.Len() != 0 {
					t.Fatalf("accounting failure became a retry, account fault or replacement response: %v", result)
				}
				if _, exists := c.Get(OpsUpstreamErrorsKey); exists {
					t.Fatal("local/unknown accounting failure was attributed as a supplier health fault")
				}
			})
		}
	}
}

func TestExperimentWebSocketStopsBeforeDialOrIdentityMutation(t *testing.T) {
	req, c, _ := experimentTestRequest(t, &experimentAdmissionSpy{})
	c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformOpenAI, UsageKind: UsageKindExperiment}})
	account := &Account{Platform: PlatformOpenAI, Credentials: map[string]any{"base_url": "https://globalai.vip/v1"}}
	// A zero connection cannot do network I/O; the selected supplier must be
	// rejected before the forwarder accesses it or prepares account identity.
	err := (&OpenAIGatewayService{}).ProxyResponsesWebSocketFromClient(req.Context(), c, &coderws.Conn{}, account, "not-used", nil, nil)
	if !errors.Is(err, ErrExperimentProtocolUnsupported) {
		t.Fatalf("unsupported websocket reached upstream preparation: %v", err)
	}
}
