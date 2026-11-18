package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// SetExperimentStandbyGateway reuses the existing authenticated Responses route.
func (h *OpenAIGatewayHandler) SetExperimentStandbyGateway(gateway http.Handler) {
	if h != nil {
		h.experimentStandbyGateway = gateway
	}
}

func (h *OpenAIGatewayHandler) dispatchExperimentStandby(c *gin.Context, source *service.APIKey, policy *service.APIFallbackPolicy, body []byte) error {
	if h.experimentStandbyGateway == nil || h.apiKeyService == nil || h.cfg == nil || h.cfg.RunMode == config.RunModeSimple ||
		c.Writer.Written() || c.Request.Context().Err() != nil || service.ExperimentStandbyFromContext(c.Request.Context()) != nil {
		return errors.New("experiment standby is unavailable for this request")
	}
	if policy.ApprovedBy != source.UserID || policy.ExperimentGroupID <= 0 {
		return errors.New("experiment standby owner or group is unavailable")
	}
	dispatch := &service.ExperimentStandby{UserID: source.UserID, KeyID: policy.ExperimentKeyID, GroupID: policy.ExperimentGroupID, Platform: policy.Platform, Model: policy.Model}
	key, err := h.apiKeyService.GetExperimentStandbyKey(c.Request.Context(), dispatch.KeyID)
	if err != nil || !dispatch.MatchesKey(key) {
		return errors.New("experiment standby Key is unavailable")
	}
	dispatch.BeforeSend = func(ctx context.Context) error {
		if err := c.Request.Context().Err(); err != nil {
			return err
		}
		current, err := h.apiFallback.FindAPIFallbackPolicy(ctx, policy.GroupID, policy.Platform, policy.Model)
		if err != nil || current == nil || current.ID != policy.ID || current.Version != policy.Version || !current.ActiveAt(time.Now()) {
			return service.ErrExperimentAccountingUnavailable
		}
		currentSource, err := h.apiKeyService.GetExperimentStandbyKey(ctx, source.ID)
		if err != nil || currentSource == nil || currentSource.UserID != source.UserID || currentSource.User == nil ||
			!currentSource.User.IsAdmin() || !currentSource.User.IsActive() || !currentSource.IsActive() ||
			currentSource.IsExpired() || currentSource.Quota > 0 || currentSource.HasRateLimits() || currentSource.GroupID == nil ||
			*currentSource.GroupID != policy.GroupID || currentSource.Group == nil || !currentSource.Group.IsActive() ||
			currentSource.Group.UsageKind != service.UsageKindCoding || !currentSource.User.CanBindLabGroup(currentSource.Group) && !currentSource.Group.IsSubscriptionType() {
			return service.ErrExperimentAccountingUnavailable
		}
		target, err := h.apiKeyService.GetExperimentStandbyKey(ctx, dispatch.KeyID)
		if err != nil || !dispatch.MatchesKey(target) || target.Key != key.Key {
			return service.ErrExperimentAccountingUnavailable
		}
		clientIP := ip.GetSecurityClientIP(c, h.cfg.TrustForwardedIPForAPIKeyACL())
		for _, checked := range []*service.APIKey{currentSource, target} {
			if len(checked.IPWhitelist) > 0 || len(checked.IPBlacklist) > 0 {
				if allowed, _ := ip.CheckIPRestrictionWithCompiledRules(clientIP, checked.CompiledIPWhitelist, checked.CompiledIPBlacklist); !allowed {
					return service.ErrExperimentAccountingUnavailable
				}
			}
		}
		if !target.User.CanBindLabGroup(target.Group) || !target.Group.ModelAllowlist.Allows(policy.Model) {
			return service.ErrExperimentAccountingUnavailable
		}
		preview, err := h.gatewayService.EvaluateAPIFallbackPrimaryPool(ctx, current)
		if err != nil || !preview.Eligible {
			return service.ErrExperimentAccountingUnavailable
		}
		return nil
	}

	req := c.Request.Clone(service.WithExperimentStandby(c.Request.Context(), dispatch))
	req.URL.Path, req.URL.RawPath, req.URL.RawQuery, req.URL.ForceQuery = "/v1/responses", "", "", false
	req.RequestURI = req.URL.Path
	req.Header.Del("x-api-key")
	req.Header.Del("x-goog-api-key")
	req.Header.Del("Cookie")
	req.Header.Set("Authorization", "Bearer "+key.Key)
	req.Header.Set("Content-Type", "application/json")
	req.GetBody = nil
	requestmodel.ResetRequestBody(req, body)
	c.Header("X-Mile-API-Fallback", "experiment")
	h.experimentStandbyGateway.ServeHTTP(c.Writer, req)
	return nil
}
