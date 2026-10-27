// Experiment calls on a globalai.vip account are billed from GlobalAI's own
// consume log. Do not price those calls from local cards, the USD catalog, or
// a group rate. One yuan is 500000 quota points. Match the row by
// X-Oneapi-Request-Id. If that row cannot be read, record zero instead of a
// locally computed price. Codex and Grok Build (usage_kind coding) are unchanged.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	globalAIHost             = "globalai.vip"
	globalAIQuotaPerYuan     = 500000
	globalAIRequestIDHeader  = "X-Oneapi-Request-Id"
	globalAIQuotaBillingMode = "globalai_quota"
	globalAILogPath          = "/api/log/token"
	globalAIConsumeLogType   = 2
)

// globalAILogFetch loads the recent consume logs for one GlobalAI API key.
// Tests replace it. Production calls GlobalAI's token log API.
var globalAILogFetch = fetchGlobalAILogsHTTP

// globalAIRetryWait pauses between log lookups. The consume row is written
// as the upstream response finishes, so the first read usually hits.
var globalAIRetryWait = []time.Duration{0, 150 * time.Millisecond, 300 * time.Millisecond, 600 * time.Millisecond}

type globalAIConsumeLog struct {
	Type             int    `json:"type"`
	Quota            int    `json:"quota"`
	ModelName        string `json:"model_name"`
	CompletionTokens int    `json:"completion_tokens"`
	RequestID        string `json:"request_id"`
	Other            string `json:"other"`
}

func accountUsesGlobalAI(account *Account) bool {
	return globalAIOrigin(account) != ""
}

func globalAIExperimentBill(account *Account, group *Group) bool {
	if !accountUsesGlobalAI(account) || group == nil {
		return false
	}
	kind, err := NormalizeUsageKind(group.Platform, group.UsageKind)
	return err == nil && kind == UsageKindExperiment
}

func globalAIOrigin(account *Account) string {
	if account == nil {
		return ""
	}
	raw := strings.TrimSpace(account.GetCredential("base_url"))
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Hostname(), globalAIHost) {
		return ""
	}
	scheme := parsed.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + parsed.Host
}

func globalAIRequestID(headers http.Header) string {
	if len(headers) == 0 {
		return ""
	}
	return strings.TrimSpace(headers.Get(globalAIRequestIDHeader))
}

func globalAIYuan(quota int) float64 {
	if quota <= 0 {
		return 0
	}
	return float64(quota) / globalAIQuotaPerYuan
}

func matchGlobalAIConsumeLog(logs []globalAIConsumeLog, requestID string) (globalAIConsumeLog, bool) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return globalAIConsumeLog{}, false
	}
	for _, log := range logs {
		if log.Type != 0 && log.Type != globalAIConsumeLogType {
			continue
		}
		if strings.TrimSpace(log.RequestID) == requestID {
			return log, true
		}
	}
	return globalAIConsumeLog{}, false
}

func applyGlobalAIExperimentCharge(
	ctx context.Context,
	account *Account,
	group *Group,
	headers http.Header,
	cost *CostBreakdown,
) *CostBreakdown {
	if !globalAIExperimentBill(account, group) {
		return cost
	}
	if cost == nil {
		cost = &CostBreakdown{}
	}
	requestID := globalAIRequestID(headers)
	quota, found, err := lookupGlobalAIQuota(ctx, account, requestID)
	if err != nil || !found {
		slog.Warn("globalai_quota.missing",
			"account_id", accountID(account),
			"request_id", requestID,
			"error", errString(err),
		)
		cost = clearExperimentCost(cost)
		cost.GlobalAIChargePending = true
		cost.GlobalAIRequestID = requestID
		return cost
	}
	yuan := globalAIYuan(quota)
	slog.Info("globalai_quota.applied",
		"account_id", accountID(account),
		"request_id", requestID,
		"quota", quota,
		"yuan", yuan,
	)
	cost.InputCost = yuan
	cost.ImageInputCost = 0
	cost.OutputCost = 0
	cost.ImageOutputCost = 0
	cost.CacheCreationCost = 0
	cost.CacheReadCost = 0
	cost.TotalCost = yuan
	cost.ActualCost = yuan
	cost.LongContextBillingApplied = false
	cost.BillingMode = globalAIQuotaBillingMode
	cost.GlobalAIChargePending = false
	cost.GlobalAIRequestID = requestID
	return cost
}

func clearExperimentCost(cost *CostBreakdown) *CostBreakdown {
	if cost == nil {
		cost = &CostBreakdown{}
	}
	cost.InputCost = 0
	cost.ImageInputCost = 0
	cost.OutputCost = 0
	cost.ImageOutputCost = 0
	cost.CacheCreationCost = 0
	cost.CacheReadCost = 0
	cost.TotalCost = 0
	cost.ActualCost = 0
	cost.LongContextBillingApplied = false
	cost.BillingMode = globalAIQuotaBillingMode
	return cost
}

func globalAIQuotaApplied(cost *CostBreakdown) bool {
	return cost != nil && cost.BillingMode == globalAIQuotaBillingMode
}

func lookupGlobalAIQuota(ctx context.Context, account *Account, requestID string) (int, bool, error) {
	origin := globalAIOrigin(account)
	apiKey := ""
	if account != nil {
		apiKey = strings.TrimSpace(account.GetCredential("api_key"))
	}
	if origin == "" || apiKey == "" || strings.TrimSpace(requestID) == "" {
		return 0, false, fmt.Errorf("globalai quota lookup is missing the origin, key, or request id")
	}
	logURL := origin + globalAILogPath
	lookupCtx := ctx
	if lookupCtx == nil {
		lookupCtx = context.Background()
	}
	lookupCtx, cancel := context.WithTimeout(lookupCtx, 4*time.Second)
	defer cancel()

	var lastErr error
	for i, wait := range globalAIRetryWait {
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-lookupCtx.Done():
				timer.Stop()
				return 0, false, lookupCtx.Err()
			case <-timer.C:
			}
		}
		logs, err := globalAILogFetch(lookupCtx, apiKey, logURL)
		if err != nil {
			lastErr = err
			slog.Warn("globalai_quota.fetch_failed", "attempt", i+1, "error", err.Error())
			continue
		}
		if log, ok := matchGlobalAIConsumeLog(logs, requestID); ok {
			return log.Quota, true, nil
		}
		lastErr = fmt.Errorf("consume log %s was not in the recent GlobalAI logs", requestID)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("consume log %s was not in the recent GlobalAI logs", requestID)
	}
	return 0, false, lastErr
}

func fetchGlobalAILogsHTTP(ctx context.Context, apiKey, logURL string) ([]globalAIConsumeLog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, logURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("globalai log api returned %d", resp.StatusCode)
	}
	return parseGlobalAILogs(body)
}

func parseGlobalAILogs(body []byte) ([]globalAIConsumeLog, error) {
	var wrapped struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, err
	}
	if len(wrapped.Data) == 0 || string(wrapped.Data) == "null" {
		return nil, nil
	}
	var logs []globalAIConsumeLog
	if err := json.Unmarshal(wrapped.Data, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func recordedUpstreamRequestID(account *Account, headers http.Header, wsMode bool) *string {
	if id := usageUpstreamRequestIDPtr(account, headers, wsMode); id != nil {
		return id
	}
	if wsMode || !accountUsesGlobalAI(account) {
		return nil
	}
	id := globalAIRequestID(headers)
	if id == "" {
		return nil
	}
	if len(id) > maxUsageUpstreamRequestIDLen {
		id = id[:maxUsageUpstreamRequestIDLen]
		for len(id) > 0 && !utf8.ValidString(id) {
			id = id[:len(id)-1]
		}
	}
	if id == "" {
		return nil
	}
	return &id
}

func accountID(account *Account) int64 {
	if account == nil {
		return 0
	}
	return account.ID
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
