package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodingSummaryBatchUsesOneQueryIndependentOfUserCount(t *testing.T) {
	for _, n := range []int{1, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			r := &UsageMonitorRepository{db: db}
			now := time.Now().UTC()
			readings := make([]service.CodingQuotaReading, 0, n)
			result := sqlmock.NewRows([]string{"user_id", "purpose", "reserved", "active", "unresolved", "pending_tasks", "failed_tasks", "corrections", "admission_mode", "reason", "reliable", "rate", "samples", "observed_at", "window_end"})
			for i := 1; i <= n; i++ {
				readings = append(readings, service.CodingQuotaReading{UserID: int64(i), Purpose: "openai", Used: "12", Pending: "3", Start: now.Add(-time.Hour), End: now.Add(time.Hour)})
				result.AddRow(i, "openai", "0", 0, 0, 0, 0, "0", "single", "uncalibrated", false, "", 0, nil, nil)
			}
			mock.ExpectQuery("WITH wanted AS").WillReturnRows(result)
			rows, err := r.ReadCodingReservationSummariesForReadings(context.Background(), readings, now)
			require.NoError(t, err)
			require.Len(t, rows, n)
			for _, row := range rows {
				require.Equal(t, "12", row.Settled)
				require.Equal(t, "3", row.Pending)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
