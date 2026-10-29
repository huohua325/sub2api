package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type summaryReuseStore struct {
	codingReadTestStore
	reads    int
	received []CodingQuotaReading
}

func (s *summaryReuseStore) ReadCodingQuotas(context.Context, []int64, time.Time) ([]CodingQuotaReading, error) {
	s.reads++
	return s.rows, nil
}
func (s *summaryReuseStore) ReadCodingReservationSummaries(ctx context.Context, users []int64, now time.Time) ([]CodingReservationSummary, error) {
	_, err := s.ReadCodingQuotas(ctx, users, now)
	return []CodingReservationSummary{{UserID: 1, Purpose: PurposeOpenAI, Active: 1}}, err
}
func (s *summaryReuseStore) ReadCodingReservationSummariesForReadings(_ context.Context, rows []CodingQuotaReading, _ time.Time) ([]CodingReservationSummary, error) {
	s.received = rows
	return []CodingReservationSummary{{UserID: 1, Purpose: PurposeOpenAI, Active: 1}}, nil
}

func TestCodingStudentActivityReusesLedgerWithoutChangingPercent(t *testing.T) {
	store := &summaryReuseStore{codingReadTestStore: codingReadTestStore{rows: []CodingQuotaReading{{UserID: 1, Purpose: PurposeOpenAI, Used: "20", Pending: "0"}}}}
	points := NewOfficialPointService(nil, store, nil)
	card := points.CardsForUserStored(context.Background(), 1)[PurposeOpenAI]
	require.Equal(t, 1, store.reads, "activity status must reuse the same ledger snapshot")
	require.Len(t, store.received, 1)
	require.Equal(t, 40, *card.Percent)
	require.Equal(t, "processing", card.ActivityStatus)
}

func TestCodingReservationUsesConfiguredReliabilityThresholds(t *testing.T) {
	t.Setenv("SUB2API_CODING_QUOTA_MIN_SAMPLES", "30")
	t.Setenv("SUB2API_CODING_QUOTA_MAX_AGE_SECONDS", "300")
	store := &codingRuntimeStore{}
	points := NewOfficialPointService(nil, store, nil)
	handle, err := points.BeginCodingReservation(context.Background(), CodingReservationInput{UserID: 1, APIKeyID: 1, AccountID: 10, Purpose: "openai"})
	require.NoError(t, err)
	defer handle.Finish()
	require.Equal(t, 30, store.inputs[0].MinSamples)
	require.Equal(t, 5*time.Minute, store.inputs[0].MaxAge)
}
