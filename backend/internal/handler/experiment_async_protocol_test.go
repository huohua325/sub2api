package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestExperimentAsyncImageKeepsSelectedSupplierProtocolBoundary(t *testing.T) {
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", nil)
	task, result, cancel, err := newAsyncImageContext(c, []byte(`{"model":"gpt-image-1","prompt":"review"}`), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	group := &service.Group{Platform: service.PlatformOpenAI, UsageKind: service.UsageKindExperiment}
	other := &service.Account{Credentials: map[string]any{"base_url": "https://api.example.invalid/v1"}}
	if err := service.ValidateGlobalAIExperimentProtocol(task.Request.Context(), task, other, group, ""); err != nil {
		t.Fatal("another selected asynchronous supplier was affected")
	}
	global := &service.Account{Credentials: map[string]any{"base_url": "https://globalai.vip/v1"}}
	err = service.ValidateGlobalAIExperimentProtocol(task.Request.Context(), task, global, group, "")
	if !errors.Is(err, service.ErrExperimentProtocolUnsupported) || result.Code != http.StatusBadRequest {
		t.Fatalf("GlobalAI async generation bypassed the selected-account guard: %v", err)
	}
	if writer.Body.Len() != 0 {
		t.Fatal("detached task wrote to the original completed ingress response")
	}
}
