package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

// recordAPIFallbackCodingCharge shares the attempt settlement transaction, so a
// durable paid receipt cannot be applied without its matching personal charge.
func recordAPIFallbackCodingCharge(ctx context.Context, tx *sql.Tx, in service.APIFallbackCodingCharge) error {
	points, err := decimal.NewFromString(in.Points)
	if err != nil || points.IsNegative() || strings.TrimSpace(in.RequestID) == "" || len(in.RequestID) > 256 ||
		in.APIKeyID <= 0 || in.UserID <= 0 || in.AccountID <= 0 ||
		(in.Purpose != service.PurposeOpenAI && in.Purpose != service.PurposeGrok) ||
		in.StartedAt.IsZero() || in.CompletedAt.Before(in.StartedAt) {
		return fmt.Errorf("invalid API fallback coding charge")
	}
	if err := lockCodingPerson(ctx, tx, in.UserID, in.Purpose); err != nil {
		return err
	}
	points = points.Round(8)
	_, err = tx.ExecContext(ctx, `INSERT INTO coding_quota_requests
 (request_id,api_key_id,user_id,purpose,account_id,started_at,completed_at,weight,points,estimated_points,state,metering_source)
 VALUES($1,$2,$3,$4,$5,$6,$7,0,$8,0,'settled','api_fallback')
 ON CONFLICT(request_id,api_key_id) DO NOTHING`, in.RequestID, in.APIKeyID, in.UserID, in.Purpose, in.AccountID,
		in.StartedAt.UTC(), in.CompletedAt.UTC(), points.StringFixed(8))
	if err != nil {
		return err
	}
	var matches bool
	err = tx.QueryRowContext(ctx, `SELECT user_id=$3 AND purpose=$4 AND account_id=$5
 AND started_at=$6 AND completed_at=$7 AND points=$8::numeric AND metering_source='api_fallback'
 AND state='settled' FROM coding_quota_requests WHERE request_id=$1 AND api_key_id=$2`,
		in.RequestID, in.APIKeyID, in.UserID, in.Purpose, in.AccountID, in.StartedAt.UTC(), in.CompletedAt.UTC(), points.StringFixed(8)).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("API fallback coding charge identity conflict")
	}
	return nil
}

// readAPIFallbackAdmissionUsage is called after locking the person. It reads
// quota periods using original start time and capacity using current evidence.
// An unresolved or unbounded existing request prevents a new paid admission.
func readAPIFallbackAdmissionUsage(ctx context.Context, tx *sql.Tx, user int64, purpose string, startedAt, now time.Time) (service.APIFallbackAdmissionUsage, error) {
	var out service.APIFallbackAdmissionUsage
	readings, err := readCodingQuotasAt(ctx, tx, []int64{user}, startedAt, now)
	if err != nil {
		return out, err
	}
	var reading *service.CodingQuotaReading
	for i := range readings {
		if readings[i].Purpose == purpose {
			reading = &readings[i]
			break
		}
	}
	if reading == nil {
		return out, service.ErrCodingQuotaUnavailable
	}
	out.Used, out.Pending = reading.Used, reading.Pending
	out.PeriodStart, out.PeriodEnd = reading.Start, reading.End
	var uncertain int
	err = tx.QueryRowContext(ctx, `WITH RECURSIVE `+codingCapacityCTE("$5::timestamptz")+`
 SELECT COALESCE(ROUND(SUM(r.estimated_points/`+codingCapacityFactorSQL("r.account_id", "r.purpose", codingReservationBasisSQL("$5::timestamptz"))+`),8),0)::text,
 COUNT(*) FILTER(WHERE r.state='unresolved' OR NOT r.reliable OR r.estimated_points IS NULL)
 FROM coding_quota_reservations r WHERE r.user_id=$1 AND r.purpose=$2
 AND r.started_at>=$3 AND r.started_at<$4 AND r.state IN('reserved','sent','handoff','unresolved')`,
		user, purpose, reading.Start, reading.End, now).Scan(&out.Reserved, &uncertain)
	if err != nil {
		return out, err
	}
	// Subscription estimates are observations, not a hard paid-spend bound. A
	// pending settlement must first be reconciled before API backup can spend.
	out.Uncertain = uncertain > 0 || reading.PendingCount > 0 || reading.UnmeteredCount > 0
	return out, nil
}
