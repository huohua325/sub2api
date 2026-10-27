package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPurposeAdjustUnconfirmedReturnsNumericCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewUsageMonitorHandler(service.NewUsageMonitorService(handlerAdjustStore{
		t: t,
	}))
	recorder := postAdjust(t, handler, `{"user_id":12,"purpose":"openai","action":"set_limit","value":"80","confirm":false}`)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	body := decodeBody(t, recorder)
	require.Equal(t, float64(400), body["code"])
	_, isString := body["code"].(string)
	require.False(t, isString)
	require.Equal(t, "CONFIRMATION_REQUIRED", body["reason"])
	require.Equal(t, "confirmation required", body["message"])
	require.NotEqual(t, "CONFIRMATION_REQUIRED", body["code"])
}

func TestPurposeAdjustRetiredSetUsedReturnsNumericCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	store := handlerAdjustStore{
		t: t,
		row: service.UsageMonitorUserRow{
			ID: 12, Purpose: service.PurposeOpenAI, LimitAmount: "100.00000000", UsedAmount: "3.00000000",
			Period: "week", WindowStart: now.Add(-8 * 24 * time.Hour), HasWallet: true,
		},
		hasRow: true,
	}
	handler := NewUsageMonitorHandler(service.NewUsageMonitorService(store))
	recorder := postAdjust(t, handler, `{"user_id":12,"purpose":"openai","action":"set_used","value":"1","confirm":true}`)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	body := decodeBody(t, recorder)
	require.Equal(t, float64(400), body["code"])
	_, isString := body["code"].(string)
	require.False(t, isString)
	require.Equal(t, "PURPOSE_YUAN_RETIRED", body["reason"])
	require.Equal(t, "only the experiment yuan cap can be changed", body["message"])
	require.NotEqual(t, "PURPOSE_YUAN_RETIRED", body["code"])
}

func postAdjust(t *testing.T, handler *UsageMonitorHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/usage-monitor/adjustments", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	handler.Adjust(ctx)
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

type handlerAdjustStore struct {
	t      *testing.T
	row    service.UsageMonitorUserRow
	hasRow bool
}

func (s handlerAdjustStore) AdjustPurposeWallet(_ context.Context, cmd service.PurposeWalletAdjust) (service.PurposeWalletAdjustResult, error) {
	if !s.hasRow {
		s.t.Fatal("adjust store called")
	}
	_, err := service.PurposeAdjustmentEffect(s.row, cmd, "0")
	if err != nil {
		return service.PurposeWalletAdjustResult{}, err
	}
	s.t.Fatal("expired window wrote a wallet")
	return service.PurposeWalletAdjustResult{}, nil
}

func (handlerAdjustStore) ListPurposeAdjustments(context.Context, service.PurposeAdjustmentQuery) ([]service.PurposeAdjustmentAudit, error) {
	return nil, nil
}
func (handlerAdjustStore) ListOfficialParents(context.Context) ([]service.UsageMonitorParentRow, error) {
	return nil, nil
}
func (handlerAdjustStore) ListStampedParentIDs(context.Context, time.Time) ([]int64, error) {
	return nil, nil
}
func (handlerAdjustStore) ListLatestStamps(context.Context, []int64, time.Time) ([]service.UsageMonitorStamp, error) {
	return nil, nil
}
func (handlerAdjustStore) ListWindowUsage(context.Context, []service.UsageMonitorWindowQuery, time.Duration) ([]service.UsageMonitorWindowUsage, error) {
	return nil, nil
}
func (handlerAdjustStore) ListLiveUsers(context.Context) ([]service.UsageMonitorUserRow, error) {
	return nil, nil
}
func (handlerAdjustStore) ListExperimentGroupIDs(context.Context) ([]int64, error) {
	return nil, nil
}
func (handlerAdjustStore) ListExperimentRequestCounts(context.Context, []int64, time.Time, time.Time) ([]service.UsageMonitorExperimentCount, error) {
	return nil, nil
}

var _ service.UsageMonitorStore = handlerAdjustStore{}
