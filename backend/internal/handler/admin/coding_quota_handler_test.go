package admin

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

type codingHandlerReader struct {
	usageMonitorReader
	q service.CodingReservationQuery
}

func (s *codingHandlerReader) ListCodingReservations(_ context.Context, q service.CodingReservationQuery) (service.CodingReservationPage, error) {
	s.q = q
	return service.CodingReservationPage{Items: []service.CodingReservation{{ID: "safe-id", APIKeyID: 12345}}}, nil
}
func TestCodingReservationAdminFiltersAndSafeResponse(t *testing.T) {
	s := &codingHandlerReader{}
	h := NewUsageMonitorHandler(s)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/requests?user_id=7&purpose=grok&state=unresolved&limit=999&before=cursor", nil)
	h.ListCodingReservations(c)
	require.Equal(t, 200, w.Code)
	require.Equal(t, 100, s.q.Limit)
	require.EqualValues(t, 7, s.q.UserID)
	require.Equal(t, "grok", s.q.Purpose)
	require.Equal(t, "unresolved", s.q.State)
	require.Equal(t, "cursor", s.q.Before)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotContains(t, w.Body.String(), "api_key")
	require.NotContains(t, w.Body.String(), "12345")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}
func TestCodingReservationAdminInvalidNumericFilters(t *testing.T) {
	for _, q := range []string{"user_id=-1", "user_id=bad", "limit=0", "limit=bad"} {
		t.Run(q, func(t *testing.T) {
			h := NewUsageMonitorHandler(&codingHandlerReader{})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/requests?"+q, nil)
			h.ListCodingReservations(c)
			require.Equal(t, 400, w.Code)
		})
	}
}
