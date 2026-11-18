package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExperimentConcurrencyRequiresActorAndValidExplicitGrant(t *testing.T) {
	for _, tc := range []struct {
		body  string
		actor bool
		code  int
	}{
		{`{"enabled":true,"max_active":20}`, false, http.StatusUnauthorized},
		{`{"max_active":20}`, true, http.StatusBadRequest},
		{`{"enabled":true,"max_active":0}`, true, http.StatusBadRequest},
		{`{"enabled":true,"max_active":1001}`, true, http.StatusBadRequest},
		{`{"enabled":true,"max_active":2.5}`, true, http.StatusBadRequest},
	} {
		t.Run(tc.body, func(t *testing.T) {
			h := NewUsageMonitorHandler(service.NewUsageMonitorService(handlerAdjustStore{t: t}))
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Params = gin.Params{{Key: "id", Value: "1"}}
			c.Request = httptest.NewRequest(http.MethodPut, "/users/1/experiment-concurrency", strings.NewReader(tc.body))
			if tc.actor {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 2})
			}
			h.SetExperimentConcurrency(c)
			require.Equal(t, tc.code, w.Code)
		})
	}
}
