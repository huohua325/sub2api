package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type unifiedCodingReadStore struct {
	service.CodingQuotaLedger
	rows []service.CodingQuotaReading
}

func (s unifiedCodingReadStore) ReadCodingQuotas(context.Context, []int64, time.Time) ([]service.CodingQuotaReading, error) {
	return s.rows, nil
}
func (unifiedCodingReadStore) ListOfficialParents(context.Context) ([]service.UsageMonitorParentRow, error) {
	panic("no upstream quota reads")
}
func (unifiedCodingReadStore) SumOfficialWindowCost(context.Context, int64, []service.UsageMonitorWindowQuery, time.Duration) ([]service.OfficialWindowCost, error) {
	panic("no legacy recalculation")
}

func TestUnifiedCodingRouteAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, exhausted := range []string{service.PurposeOpenAI, service.PurposeGrok} {
		store := unifiedCodingReadStore{}
		for _, purpose := range []string{service.PurposeOpenAI, service.PurposeGrok} {
			used := "0"
			if purpose == exhausted {
				used = "1000000"
			}
			store.rows = append(store.rows, service.CodingQuotaReading{Purpose: purpose, Used: used, Pending: "0"})
		}
		keyService := &service.APIKeyService{}
		keyService.SetOfficialPoints(service.NewOfficialPointService(nil, store, nil))
		key := &service.APIKey{ID: 2, UserID: 1, Group: &service.Group{ID: 10, Platform: service.PlatformComposite, UsageKind: service.UsageKindCoding}}
		router := gin.New()
		router.Use(func(c *gin.Context) { c.Set("api_key", key); c.Next() })
		router.Use(compositeTargetPlatformMiddleware(nil, keyService))
		router.POST("/v1/responses", func(c *gin.Context) {
			body, _ := io.ReadAll(c.Request.Body)
			platform, _ := service.ResolvedTargetPlatformFromContext(c.Request.Context())
			c.JSON(200, gin.H{"platform": platform, "body": string(body), "key_id": key.ID})
		})
		for _, tc := range []struct{ model, platform string }{
			{"gpt-6.1-sol", service.PlatformOpenAI}, {"grok-4.6", service.PlatformGrok},
			{"claude-sonnet-4-6", service.PlatformAnthropic}, {"unknown", ""},
		} {
			t.Run(exhausted+"/"+tc.model, func(t *testing.T) {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"`+tc.model+`","input":[{"type":"function_call_output","call_id":"call_1","output":"ok"}],"stream":true}`))
				req.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(rec, req)
				switch tc.platform {
				case exhausted:
					require.Equal(t, 403, rec.Code, rec.Body.String())
				case service.PlatformOpenAI, service.PlatformGrok:
					require.Equal(t, 200, rec.Code, rec.Body.String())
					require.Contains(t, rec.Body.String(), `"platform":"`+tc.platform+`"`)
					require.Contains(t, rec.Body.String(), "function_call_output")
				default:
					require.Equal(t, 400, rec.Code, rec.Body.String())
				}
			})
		}
	}
}
