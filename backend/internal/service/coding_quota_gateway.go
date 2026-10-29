package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

const codingRequestHandleKey = "mile.coding-reservation"

// CodingControlMode defaults to observation; off still drains durable work.
func CodingControlMode(purpose string) string {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("SUB2API_CODING_QUOTA_" + strings.ToUpper(purpose) + "_MODE")))
	if value == "off" || value == "enforce" {
		return value
	}
	return "shadow"
}

func codingRawEstimate(billing *BillingService, account *Account, body []byte) (string, string) {
	model := gjson.GetBytes(body, "model").String()
	if account != nil {
		model = account.GetMappedModel(model)
	}
	// Codex removes client output caps; they cannot bound the actual generation.
	if account != nil && account.IsOpenAIOAuthLike() {
		return model, ""
	}
	// Unknown payloads (including media) remain explicitly unknown, never free.
	if billing == nil || !billing.HasIdentifiedTokenPricing(model) || !gjson.ValidBytes(body) {
		return model, ""
	}
	maxOutput := gjson.GetBytes(body, "max_output_tokens").Int()
	if maxOutput <= 0 {
		maxOutput = gjson.GetBytes(body, "max_completion_tokens").Int()
	}
	if maxOutput <= 0 {
		maxOutput = gjson.GetBytes(body, "max_tokens").Int()
	}
	if maxOutput <= 0 {
		return model, ""
	}
	pricing, err := billing.GetModelPricing(model)
	if err != nil || pricing == nil || pricing.InputPricePerToken < 0 || pricing.OutputPricePerToken < 0 {
		return model, ""
	}
	input := decimal.NewFromInt(int64(len(body))).DivRound(decimal.NewFromInt(3), 8)
	weight := input.Mul(decimal.NewFromFloat(pricing.InputPricePerToken)).Add(decimal.NewFromInt(maxOutput).Mul(decimal.NewFromFloat(pricing.OutputPricePerToken)))
	if !weight.IsPositive() {
		return model, ""
	}
	return model, weight.String()
}

func codingPositiveEnv(key string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// CodingCalibrationLimits is shared by admission and diagnostics.
func CodingCalibrationLimits() (int, time.Duration) {
	return codingPositiveEnv("SUB2API_CODING_QUOTA_MIN_SAMPLES", 20), time.Duration(codingPositiveEnv("SUB2API_CODING_QUOTA_MAX_AGE_SECONDS", 1200)) * time.Second
}

func prepareCodingRequest(ctx context.Context, c *gin.Context, points *OfficialPointService, billing *BillingService, account *Account, body []byte) (context.Context, error) {
	if points == nil || c == nil || account == nil {
		return ctx, nil
	}
	if c.Request != nil && c.Request.Method == http.MethodGet && strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		return ctx, nil
	}

	v, _ := c.Get("api_key")
	key, _ := v.(*APIKey)
	if key == nil || key.Group == nil {
		return ctx, nil
	}
	purpose := PurposeOfGroup(key.Group.Platform, key.Group.UsageKind)
	if purpose != PurposeOpenAI && purpose != PurposeGrok {
		return ctx, nil
	}
	mode := CodingControlMode(purpose)
	if mode == "off" {
		return ctx, nil
	}
	model, weight := codingRawEstimate(billing, account, body)
	parentID := account.ID
	if account.ParentAccountID != nil {
		parentID = *account.ParentAccountID
	}
	in := CodingReservationInput{UserID: key.UserID, APIKeyID: key.ID, AccountID: parentID, Purpose: purpose, Model: model, Weight: weight, Mode: mode, StartedAt: time.Now().UTC(), Limit: points.Budget(ctx).ForPurpose(purpose), MinSamples: codingPositiveEnv("SUB2API_CODING_QUOTA_MIN_SAMPLES", 20), MaxAge: time.Duration(codingPositiveEnv("SUB2API_CODING_QUOTA_MAX_AGE_SECONDS", 1200)) * time.Second}
	if at := OpenAIPricingAtFromContext(ctx); !at.IsZero() {
		in.StartedAt = at
	} else if at := GatewayTokenRequestPricingAtFromContext(ctx); !at.IsZero() {
		in.StartedAt = at
	}
	var h *CodingReservationHandle
	if old, exists := c.Get(codingRequestHandleKey); exists {
		h, _ = old.(*CodingReservationHandle)
	}
	var err error
	if h != nil {
		in.ID = h.ID
		_, err = h.store.ReserveCodingQuota(ctx, in)
	} else {
		h, err = points.BeginCodingReservation(ctx, in)
	}
	if err != nil {
		if mode == "shadow" {
			slog.Error("coding_reservation_shadow_failed", "purpose", purpose, "user_id", key.UserID, "error", err)
			return ctx, nil
		}
		if !errors.Is(err, ErrCodingQuotaBusy) && !errors.Is(err, ErrCodingQuotaExhausted) {
			err = fmt.Errorf("%w: %v", ErrCodingQuotaUnavailable, err)
		}
		WriteCodingQuotaError(c, err)
		return ctx, err
	}
	c.Set(codingRequestHandleKey, h)
	ctx = WithCodingReservation(ctx, h)
	c.Request = c.Request.WithContext(WithCodingReservation(c.Request.Context(), h))
	return ctx, nil
}

func (s *OpenAIGatewayService) SetCodingPoints(points *OfficialPointService) { s.codingPoints = points }
func (s *GatewayService) SetCodingPoints(points *OfficialPointService)       { s.codingPoints = points }
func (s *OpenAIGatewayService) PrepareCodingRequest(ctx context.Context, c *gin.Context, account *Account, body []byte) (context.Context, error) {
	return prepareCodingRequest(ctx, c, s.codingPoints, s.billingService, account, body)
}
func (s *GatewayService) PrepareCodingRequest(ctx context.Context, c *gin.Context, account *Account, body []byte) (context.Context, error) {
	return prepareCodingRequest(ctx, c, s.codingPoints, s.billingService, account, body)
}

func FinishCodingRequest(c *gin.Context) {
	if c == nil {
		return
	}
	if v, ok := c.Get(codingRequestHandleKey); ok {
		if h, ok := v.(*CodingReservationHandle); ok {
			h.Finish()
		}
	}
}

func WriteCodingQuotaError(c *gin.Context, err error) {
	status, code, message := http.StatusServiceUnavailable, "CODING_QUOTA_UNAVAILABLE", "Quota service temporarily unavailable; retry later"
	if errors.Is(err, ErrCodingQuotaBusy) {
		status, code, message = http.StatusTooManyRequests, "CODING_QUOTA_BUSY", "Concurrent quota capacity is busy; retry later"
		c.Header("Retry-After", "5")
	}
	if errors.Is(err, ErrCodingQuotaExhausted) {
		status, code, message = http.StatusForbidden, "PURPOSE_QUOTA_EXHAUSTED", "Your quota is exhausted"
	}
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"type": code, "code": code, "message": message}})
}
