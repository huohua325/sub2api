package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGlobalAIYuanUsesQuotaPerUnit(t *testing.T) {
	if got := globalAIYuan(10247); got != 10247.0/globalAIQuotaPerYuan {
		t.Fatalf("yuan = %v", got)
	}
	if globalAIYuan(0) != 0 || globalAIYuan(-1) != 0 {
		t.Fatal("non-positive quota must be free")
	}
}

func TestMatchGlobalAIConsumeLogByRequestID(t *testing.T) {
	logs := []globalAIConsumeLog{
		{Type: 1, Quota: 99, RequestID: "topup"},
		{Type: 2, Quota: 10247, RequestID: "req-terra", ModelName: "gpt-5.6-terra"},
		{Type: 5, Quota: 50, RequestID: "req-terra"},
	}
	got, ok := matchGlobalAIConsumeLog(logs, " req-terra ")
	if !ok || got.Quota != 10247 {
		t.Fatalf("match = %+v ok=%v", got, ok)
	}
	if _, ok := matchGlobalAIConsumeLog(logs, "missing"); ok {
		t.Fatal("missing request id matched")
	}
}

func TestParseGlobalAILogs(t *testing.T) {
	body := []byte(`{"success":true,"data":[{"type":2,"quota":14,"request_id":"abc","model_name":"glm-5.3-flash"}]}`)
	logs, err := parseGlobalAILogs(body)
	if err != nil || len(logs) != 1 || logs[0].Quota != 14 {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
}

func TestApplyGlobalAIExperimentChargeReplacesLocalPrice(t *testing.T) {
	orig := globalAILogFetch
	origWait := globalAIRetryWait
	t.Cleanup(func() {
		globalAILogFetch = orig
		globalAIRetryWait = origWait
	})
	globalAIRetryWait = []time.Duration{0}
	globalAILogFetch = func(context.Context, string, string) ([]globalAIConsumeLog, error) {
		return []globalAIConsumeLog{{Type: 2, Quota: 10247, RequestID: "req-1"}}, nil
	}
	account := &Account{ID: 11, Credentials: map[string]any{
		"base_url": "https://globalai.vip/v1",
		"api_key":  "sk-test",
	}}
	group := &Group{Platform: PlatformOpenAI, UsageKind: UsageKindExperiment}
	headers := http.Header{}
	headers.Set(globalAIRequestIDHeader, "req-1")
	local := &CostBreakdown{InputCost: 0.08, OutputCost: 0.02, TotalCost: 0.1, ActualCost: 0.1}
	got := applyGlobalAIExperimentCharge(context.Background(), account, group, headers, local)
	if got.ActualCost != 10247.0/globalAIQuotaPerYuan || got.TotalCost != got.ActualCost || got.OutputCost != 0 {
		t.Fatalf("cost = %+v", got)
	}
	if !globalAIQuotaApplied(got) {
		t.Fatal("billing mode was not marked")
	}
}

func TestApplyGlobalAIExperimentChargeLeavesCodingGroups(t *testing.T) {
	account := &Account{Credentials: map[string]any{"base_url": "https://globalai.vip/v1", "api_key": "sk-test"}}
	group := &Group{Platform: PlatformOpenAI, UsageKind: UsageKindCoding}
	local := &CostBreakdown{ActualCost: 1.25, TotalCost: 1.25}
	got := applyGlobalAIExperimentCharge(context.Background(), account, group, nil, local)
	if got.ActualCost != 1.25 {
		t.Fatalf("coding cost changed: %+v", got)
	}
}

func TestApplyGlobalAIExperimentChargeZerosWhenLogIsMissing(t *testing.T) {
	orig := globalAILogFetch
	origWait := globalAIRetryWait
	t.Cleanup(func() {
		globalAILogFetch = orig
		globalAIRetryWait = origWait
	})
	globalAIRetryWait = []time.Duration{0}
	globalAILogFetch = func(context.Context, string, string) ([]globalAIConsumeLog, error) {
		return nil, errors.New("upstream down")
	}
	account := &Account{Credentials: map[string]any{"base_url": "https://globalai.vip", "api_key": "sk-test"}}
	group := &Group{Platform: PlatformAnthropic, UsageKind: UsageKindExperiment}
	headers := http.Header{}
	headers.Set(globalAIRequestIDHeader, "req-2")
	got := applyGlobalAIExperimentCharge(context.Background(), account, group, headers, &CostBreakdown{ActualCost: 3})
	if got.ActualCost != 0 || got.TotalCost != 0 {
		t.Fatalf("missing log kept local cost: %+v", got)
	}
}

func TestFetchGlobalAILogsHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"type":2,"quota":14,"request_id":"abc"}]}`))
	}))
	defer srv.Close()
	logs, err := fetchGlobalAILogsHTTP(context.Background(), "sk-test", srv.URL)
	if err != nil || len(logs) != 1 || logs[0].RequestID != "abc" {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
}
