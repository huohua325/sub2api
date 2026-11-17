package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// CodingSubscriptionExclusions keeps API accounts out of the normal unified
// coding scheduler, including previous-response and sticky selection paths.
func (s *OpenAIGatewayService) CodingSubscriptionExclusions(ctx context.Context, groupID int64) (map[int64]struct{}, error) {
	accounts, err := s.accountRepo.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	excluded := make(map[int64]struct{})
	for i := range accounts {
		if !accounts[i].IsOAuth() {
			excluded[accounts[i].ID] = struct{}{}
		}
	}
	return excluded, nil
}

// EvaluateAPIFallbackPrimaryPool uses the complete current group membership.
// Neither an empty pool, a missing member, nor a temporary scheduler exclusion
// authorizes paid inference. It makes no upstream requests.
func (s *OpenAIGatewayService) EvaluateAPIFallbackPrimaryPool(ctx context.Context, p *APIFallbackPolicy) (*APIFallbackPreview, error) {
	view := &APIFallbackPreview{Reason: "primary_state_unknown", Checks: []APIFallbackCheck{}}
	if p == nil || s == nil || s.accountRepo == nil {
		return view, nil
	}
	accounts, err := s.accountRepo.ListByGroup(ctx, p.GroupID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	actual := make(map[int64]*Account)
	for i := range accounts {
		a := &accounts[i]
		if a.Platform != p.Platform || !a.IsOAuth() || !a.IsModelSupported(p.Model) {
			continue
		}
		// A disabled shadow must not inherit the active parent's eligibility.
		if a.ParentAccountID != nil {
			autoExpired := a.AutoPauseOnExpired && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt)
			if (a.Status != StatusActive && a.Status != StatusError) || (!a.Schedulable && !autoExpired) {
				continue
			}
		}
		id := a.ID
		if a.ParentAccountID != nil {
			id = *a.ParentAccountID
		}
		if a.ParentAccountID != nil {
			parent, e := s.accountRepo.GetByID(ctx, id)
			if e != nil {
				return nil, e
			}
			if parent == nil || !parent.IsOAuth() || parent.Platform != p.Platform {
				return view, nil
			}
			a = parent
		}
		actual[id] = a
	}
	if len(actual) == 0 {
		view.Reason = "primary_pool_empty"
		return view, nil
	}
	if len(actual) != len(p.PrimaryAccountIDs) {
		view.Reason = "primary_pool_changed"
		return view, nil
	}
	for _, id := range p.PrimaryAccountIDs {
		a := actual[id]
		if a == nil {
			view.Reason = "primary_pool_changed"
			return view, nil
		}
		check := APIFallbackCheck{Name: fmt.Sprintf("account:%d", id), Detail: "primary_state_unknown"}
		if a.Status != StatusActive && a.Status != "error" {
			check.Detail = "primary_paused"
			view.Checks = append(view.Checks, check)
			continue
		}
		var evidence *APIFallbackPrimaryEvidence
		for i := range p.PrimaryEvidence {
			if p.PrimaryEvidence[i].AccountID == id {
				evidence = &p.PrimaryEvidence[i]
				break
			}
		}
		used, reset, observed, _ := readCodexOfficial(a.Extra, nil, now)
		if p.Platform == PlatformGrok {
			used, reset, observed, _, _ = grokOfficial(a.Extra, now)
		}
		if evidence == nil || evidence.ObservedAt.After(now) || !now.Before(evidence.ValidUntil) || evidence.ValidUntil.Sub(evidence.ObservedAt) > 24*time.Hour {
			// A fresh persisted official exhausted reading is independently
			// sufficient evidence; an ordinary 429 or local busy flag is not.
			if a.Schedulable && used != nil && reset != nil && observed != nil && *used >= 100 && reset.After(now) && !observed.After(now) && now.Sub(*observed) <= 20*time.Minute {
				check.Passed = true
				check.Detail = "quota_exhausted"
			}
			view.Checks = append(view.Checks, check)
			continue
		}
		switch evidence.Reason {
		case "subscription_expired", "quota_exhausted", "auth_invalid":
		default:
			view.Checks = append(view.Checks, check)
			continue
		}
		autoExpiredEvidence := a.AutoPauseOnExpired && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt) && evidence.Reason == "subscription_expired"
		if !a.Schedulable && !autoExpiredEvidence {
			check.Detail = "primary_paused"
			view.Checks = append(view.Checks, check)
			continue
		}
		if evidence.Reason == "auth_invalid" && a.Status == StatusActive && a.UpdatedAt.After(evidence.ObservedAt) {
			check.Detail = "primary_credentials_or_state_changed"
			view.Checks = append(view.Checks, check)
			continue
		}
		// A newer fresh healthy observation invalidates an older failure assertion.
		if used != nil && reset != nil && observed != nil && *used < 100 && reset.After(now) && !observed.Before(evidence.ObservedAt) && now.Sub(*observed) <= 20*time.Minute {
			check.Detail = "subscription_recovered"
			view.Checks = append(view.Checks, check)
			view.Reason = "subscription_available"
			continue
		}
		// A quota window ending also expires its exhaustion evidence.
		if evidence.Reason == "quota_exhausted" && reset != nil && !now.Before(*reset) {
			check.Detail = "quota_window_ended"
			view.Checks = append(view.Checks, check)
			continue
		}
		check.Passed = true
		check.Detail = evidence.Reason
		view.Checks = append(view.Checks, check)
	}
	view.Eligible = len(view.Checks) == len(actual)
	for _, check := range view.Checks {
		if !check.Passed {
			view.Eligible = false
		}
	}
	if view.Eligible {
		view.Reason = "all_subscriptions_unavailable"
	}
	return view, nil
}

// prepareAPIFallbackBody accepts only self-contained, bounded text/tool requests.
// Byte-based token sizing is deliberately conservative (not the usual bytes/3
// estimate): UTF-8 byte tokens plus generous framing overhead for verified BPE
// Responses endpoints. Cold input pricing is used when reserving spend.
func prepareAPIFallbackBody(p *APIFallbackPolicy, raw []byte) ([]byte, int64, int64, error) {
	return prepareAPIFallbackBodyWithLineage(p, raw, false)
}

// prepareAPIFallbackBodyWithLineage preserves encrypted history only after the
// caller has verified every digest against this user's key, group and model,
// and pinned this attempt to the same API account that produced that history.
func prepareAPIFallbackBodyWithLineage(p *APIFallbackPolicy, raw []byte, allowEncrypted bool) ([]byte, int64, int64, error) {
	return prepareAPIFallbackRequest(p, raw, allowEncrypted)
}

// TryAPIFallback runs at most one paid HTTP attempt, outside the normal wallet
// billing/failover path. False leaves the existing subscription request intact.
func (s *OpenAIGatewayService) TryAPIFallback(ctx context.Context, c *gin.Context, control *UsageMonitorService, key *APIKey, model string, raw []byte, started time.Time) (bool, error) {
	if control == nil || key == nil || key.User == nil || !key.User.IsAdmin() || key.GroupID == nil || key.Group == nil || key.Group.ID != *key.GroupID {
		return false, nil
	}
	resolved, _ := ResolvedTargetPlatformFromContext(ctx)
	platform, ok := APIFallbackPlatformForGroup(key.Group, resolved)
	if !ok {
		return false, nil
	}
	hashes := apiFallbackEncryptedDigests(gjson.ParseBytes(raw))
	p, err := control.FindAPIFallbackPolicy(ctx, *key.GroupID, platform, model)
	if err != nil {
		if len(hashes) == 0 {
			slog.Error("api_fallback_policy_read_failed", "group", *key.GroupID, "error", err)
			return false, nil
		}
		return true, errors.New("standby policy cannot be verified")
	}
	var lineageAccount int64
	if len(hashes) > 0 {
		lineageAccount, err = control.ResolveAPIFallbackLineage(ctx, key.UserID, key.ID, *key.GroupID, model, hashes)
		if err != nil {
			return true, errors.New("standby conversation provenance cannot be verified")
		}
	}
	if p == nil || !p.Enabled || p.ExpiresAt == nil || !time.Now().Before(*p.ExpiresAt) {
		if lineageAccount > 0 {
			return true, errors.New("standby is disabled or expired; start a new conversation to use subscriptions")
		}
		return false, nil
	}
	decision, err := s.EvaluateAPIFallbackPrimaryPool(ctx, p)
	if err != nil {
		return true, errors.New("subscription state cannot be verified")
	}
	if !decision.Eligible {
		if lineageAccount > 0 {
			return true, errors.New("subscription routing has resumed; start a new conversation to avoid moving encrypted standby state")
		}
		return false, nil
	}
	if c.Request.Method != http.MethodPost || strings.TrimRight(c.Request.URL.Path, "/") != "/v1/responses" && strings.TrimRight(c.Request.URL.Path, "/") != "/responses" && strings.TrimRight(c.Request.URL.Path, "/") != "/backend-api/codex/responses" {
		return true, errors.New("standby supports HTTP Responses only")
	}
	if len(hashes) > 0 && lineageAccount == 0 {
		return true, errors.New("standby cannot migrate encrypted subscription state; start a new conversation")
	}
	var body []byte
	var input, output int64
	if lineageAccount > 0 {
		body, input, output, err = prepareAPIFallbackBodyWithLineage(p, raw, true)
	} else {
		body, input, output, err = prepareAPIFallbackBody(p, raw)
	}
	if err != nil {
		return true, err
	}
	// Legacy monetary Key/account quotas can have channel-specific prices.
	// Until that price is proven equivalent, preserve their enforcement by
	// refusing paid admission rather than silently bypassing a configured cap.
	if key.Quota > 0 || key.HasRateLimits() {
		return true, errors.New("standby currently requires a Key without monetary quota or rolling cost limits")
	}
	if s.codingPoints == nil || s.httpUpstream == nil || s.concurrencyService == nil {
		return true, errors.New("standby metering or concurrency is unavailable")
	}
	var account *Account
	var release func()
	for _, id := range p.BackupAccountIDs {
		if lineageAccount > 0 && id != lineageAccount {
			continue
		}
		a, e := s.accountRepo.GetByID(ctx, id)
		if e != nil {
			return true, errors.New("standby account cannot be verified")
		}
		if a == nil || a.Type != AccountTypeAPIKey || a.Platform != platform || !slices.Contains(a.GroupIDs, p.GroupID) || !a.IsSchedulable() || !a.IsModelSupported(model) || a.GetMappedModel(model) != model || a.GetCredential("api_key") == "" || a.HasAnyQuotaLimit() {
			continue
		}
		slot, e := s.concurrencyService.AcquireAccountSlot(ctx, a.ID, a.Concurrency)
		if e != nil {
			return true, errors.New("standby concurrency cannot be verified")
		}
		if slot != nil && slot.Acquired {
			account = a
			release = slot.ReleaseFunc
			break
		}
	}
	if account == nil {
		return true, errors.New("no verified standby account is ready")
	}
	if release != nil {
		defer release()
	}
	base := account.GetOpenAIBaseURL()
	if platform == PlatformGrok {
		base = account.GetGrokBaseURL()
	}
	base, err = s.validateUpstreamBaseURL(base)
	if err != nil {
		return true, errors.New("standby endpoint is not permitted")
	}
	url := buildOpenAIResponsesURL(base)
	upstreamCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return true, errors.New("invalid standby endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+account.GetCredential("api_key"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	requestID := uuid.NewString()
	reservation, err := control.ReserveAPIFallback(ctx, APIFallbackReservationInput{ID: requestID, RequestID: requestID, PolicyID: p.ID, PolicyVersion: p.Version, UserID: key.UserID, APIKeyID: key.ID, AccountID: account.ID, GroupID: p.GroupID, Platform: p.Platform, Model: model, EstimatedInputTokens: input, MaxOutputTokens: output, PersonalLimit: s.codingPoints.Budget(ctx).ForPurpose(PurposeForPlatform(key.Group, platform)), StartedAt: started, Now: time.Now().UTC()})
	if err != nil {
		return true, err
	}
	// Recheck after acquiring both concurrency and spend reservations.
	decision, err = s.EvaluateAPIFallbackPrimaryPool(ctx, p)
	if err != nil || !decision.Eligible || ctx.Err() != nil {
		cleanCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer done()
		if e := control.CancelAPIFallbackUnsent(cleanCtx, reservation.ID, "subscription_or_request_changed"); e != nil {
			slog.Error("api_fallback_cancel_failed", "attempt", reservation.ID, "error", e)
		}
		if err != nil {
			return true, errors.New("subscription state cannot be verified")
		}
		return false, nil
	}
	if err = control.MarkAPIFallbackSent(ctx, reservation.ID); err != nil {
		cleanCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer done()
		if e := control.CancelAPIFallbackUnsent(cleanCtx, reservation.ID, "send_not_authorized"); e != nil {
			slog.Error("api_fallback_cancel_failed", "attempt", reservation.ID, "error", e)
		}
		return true, err
	}
	c.Header("X-Mile-API-Fallback", "true")
	c.Header("X-Request-ID", requestID)
	proxy := ""
	if account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	resp, forwardErr := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
	if resp == nil && forwardErr == nil {
		forwardErr = errors.New("standby upstream returned no response")
	}
	var receipt *APIFallbackSettlementInput
	if forwardErr == nil && resp != nil {
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				slog.Warn("api_fallback_upstream_close_failed", "attempt", reservation.ID, "error", closeErr)
			}
		}()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			receipt, forwardErr = relayAPIFallbackResponse(c, resp, model)
		} else {
			forwardErr = fmt.Errorf("standby upstream returned status %d; spend awaits verification", resp.StatusCode)
		}
	}
	settleCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer done()
	if receipt != nil {
		// Keep provider identifiers, never prompts/output/key material, for billing
		// reconciliation against the supplier's request log.
		if resp != nil {
			var evidence map[string]any
			if json.Unmarshal(receipt.Usage, &evidence) == nil {
				evidence["provider_request_id"] = resp.Header.Get("X-Request-ID")
				if encoded, e := json.Marshal(evidence); e == nil {
					receipt.Usage = encoded
				}
			}
		}
		receipt.ID = reservation.ID
		receipt.Now = time.Now().UTC()
		if _, err = control.SettleAPIFallback(settleCtx, *receipt); err != nil {
			slog.Error("api_fallback_settlement_failed", "attempt", reservation.ID, "error", err)
			if e := control.MarkAPIFallbackUnknown(settleCtx, reservation.ID, "settlement_failed"); e != nil {
				slog.Error("api_fallback_unknown_failed", "attempt", reservation.ID, "error", e)
			}
			return true, errors.New("standby billing awaits verification")
		}
	} else {
		reason := "upstream_outcome_unknown"
		if resp != nil {
			providerID := resp.Header.Get("X-Request-ID")
			if len(providerID) > 160 {
				providerID = providerID[:160]
			}
			providerID = strings.Map(func(r rune) rune {
				if r < ' ' || r == 127 {
					return -1
				}
				return r
			}, providerID)
			reason = fmt.Sprintf("upstream_outcome_unknown status=%d provider_request_id=%s", resp.StatusCode, providerID)
		}
		if err = control.MarkAPIFallbackUnknown(settleCtx, reservation.ID, reason); err != nil {
			slog.Error("api_fallback_unknown_failed", "attempt", reservation.ID, "error", err)
		}
	}
	return true, forwardErr
}

func fallbackReceipt(raw []byte, model string) *APIFallbackSettlementInput {
	if !gjson.ValidBytes(raw) {
		return nil
	}
	root := gjson.ParseBytes(raw)
	if root.Get("response").IsObject() {
		root = root.Get("response")
	}
	if observed := root.Get("model").String(); observed != model {
		return nil
	}
	if tier := root.Get("service_tier").String(); tier != "" && tier != "default" {
		return nil
	}
	u := root.Get("usage")
	in, out := u.Get("input_tokens"), u.Get("output_tokens")
	cache := u.Get("input_tokens_details.cached_tokens")
	if !u.IsObject() || in.Type != gjson.Number || out.Type != gjson.Number || in.Int() < 0 || out.Int() < 0 || in.Float() != float64(in.Int()) || out.Float() != float64(out.Int()) {
		return nil
	}
	if cache.Exists() && (cache.Type != gjson.Number || cache.Int() < 0 || cache.Int() > in.Int() || cache.Float() != float64(cache.Int())) {
		return nil
	}
	evidence, err := json.Marshal(map[string]any{"response_id": root.Get("id").String(), "model": model, "service_tier": root.Get("service_tier").String(), "usage": json.RawMessage(u.Raw), "encrypted_digests": apiFallbackEncryptedDigests(root.Get("output"))})
	if err != nil {
		return nil
	}
	return &APIFallbackSettlementInput{InputTokens: in.Int(), CachedInputTokens: cache.Int(), OutputTokens: out.Int(), Usage: evidence}
}

func apiFallbackEncryptedDigests(root gjson.Result) []string {
	digests := make(map[string]struct{})
	var walk func(gjson.Result)
	walk = func(value gjson.Result) {
		if value.IsArray() {
			value.ForEach(func(_, child gjson.Result) bool { walk(child); return len(digests) <= 100 })
		} else if value.IsObject() {
			if value.Get("type").String() == "reasoning" {
				child := value.Get("encrypted_content")
				if child.Type == gjson.String && child.String() != "" {
					sum := sha256.Sum256([]byte(child.String()))
					digests[hex.EncodeToString(sum[:])] = struct{}{}
				}
			} else if input := value.Get("input"); input.IsArray() {
				walk(input)
			}
		}
	}
	walk(root)
	out := make([]string, 0, len(digests))
	for digest := range digests {
		out = append(out, digest)
	}
	slices.Sort(out)
	return out
}

func relayAPIFallbackResponse(c *gin.Context, resp *http.Response, model string) (*APIFallbackSettlementInput, error) {
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		var receipt *APIFallbackSettlementInput
		var frame bytes.Buffer
		var data []string
		terminal := false
		dispatch := func() error {
			payload := []byte(strings.Join(data, "\n"))
			if len(data) > 0 && string(payload) != "[DONE]" {
				if !gjson.ValidBytes(payload) {
					return errors.New("invalid standby stream event")
				}
				response := gjson.GetBytes(payload, "response")
				if observed := response.Get("model").String(); observed != "" && observed != model {
					receipt = nil
					return errors.New("standby returned a different model")
				}
				switch gjson.GetBytes(payload, "type").String() {
				case "response.completed", "response.incomplete":
					if terminal {
						receipt = nil
						return errors.New("standby returned multiple terminal responses")
					}
					terminal = true
					receipt = fallbackReceipt(payload, model)
				}
			}
			if _, err := c.Writer.Write(frame.Bytes()); err != nil {
				return err
			}
			c.Writer.Flush()
			frame.Reset()
			data = nil
			return nil
		}
		for scanner.Scan() {
			line := scanner.Bytes()
			if frame.Len()+len(line) > 4*1024*1024 {
				return nil, errors.New("standby stream event exceeded the supported size")
			}
			if _, err := frame.Write(line); err != nil {
				return receipt, err
			}
			if err := frame.WriteByte('\n'); err != nil {
				return receipt, err
			}
			if bytes.HasPrefix(line, []byte("data:")) {
				data = append(data, string(bytes.TrimPrefix(bytes.TrimPrefix(line, []byte("data:")), []byte(" "))))
			}
			if len(line) == 0 {
				if err := dispatch(); err != nil {
					return receipt, err
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return receipt, err
		}
		if frame.Len() > 0 {
			if err := dispatch(); err != nil {
				return receipt, err
			}
		}
		if receipt == nil {
			return nil, errors.New("standby stream ended without verified usage")
		}
		return receipt, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 8*1024*1024 {
		return nil, errors.New("standby response exceeded the supported size")
	}
	receipt := fallbackReceipt(raw, model)
	if observed := gjson.GetBytes(raw, "model").String(); observed != "" && observed != model {
		return nil, errors.New("standby returned a different model")
	}
	c.Header("Content-Type", "application/json")
	if _, err = c.Writer.Write(raw); err != nil {
		return receipt, err
	}
	if receipt == nil {
		return nil, errors.New("standby response lacks verified model or usage")
	}
	return receipt, nil
}
