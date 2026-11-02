package repository

import "fmt"

// Capacity conversion is a read projection. The append-only events, original
// charges and corrections remain the audit source; reads never create refunds.
// Descending suffix products keep numeric arithmetic exact for multiple changes.
func codingCapacityCTE(asOf string) string {
	return fmt.Sprintf(`capacity_steps AS (
 SELECT e.*,ROW_NUMBER() OVER(PARTITION BY account_id,purpose ORDER BY effective_at DESC,id DESC) step
 FROM coding_quota_capacity_events e WHERE effective_at<=%s
), capacity_factors AS (
 SELECT account_id,purpose,effective_at,window_end,previous_rate,capacity_factor::numeric AS factor,step
 FROM capacity_steps WHERE step=1
 UNION ALL
 SELECT e.account_id,e.purpose,e.effective_at,e.window_end,e.previous_rate,e.capacity_factor*f.factor,e.step
 FROM capacity_steps e JOIN capacity_factors f ON e.account_id=f.account_id AND e.purpose=f.purpose AND e.step=f.step+1
)`, asOf)
}

func codingCapacityFactorSQL(account, purpose, basis string) string {
	return fmt.Sprintf(`COALESCE((SELECT f.factor FROM capacity_factors f
 WHERE f.account_id=%s AND f.purpose=%s AND f.effective_at>%s ORDER BY f.effective_at LIMIT 1),1)`, account, purpose, basis)
}

// Re-estimate unresolved history only with evidence from the same official
// window and capacity stage. The unknown state and original estimate stay intact.
// A different/absent window, missing rate or later meter cannot manufacture an
// exact charge or silently turn an unknown request into a free settled request.
func codingProjectedEstimateSQL(asOf string) string {
	return fmt.Sprintf(`CASE WHEN q.state='unmetered' AND q.weight>0 THEN COALESCE(
 (SELECT q.weight*f.previous_rate FROM capacity_factors f
  WHERE f.account_id=q.account_id AND f.purpose=q.purpose AND f.effective_at>q.started_at AND f.previous_rate>0
  AND q.official_window_end BETWEEN f.window_end-INTERVAL '5 minutes' AND f.window_end+INTERVAL '5 minutes'
  ORDER BY f.effective_at LIMIT 1),
 CASE WHEN m.points_per_weight>0 AND q.started_at>=COALESCE(m.calibration_started_at,'-infinity'::timestamptz)
  AND COALESCE(m.calibration_started_at,'-infinity'::timestamptz)<=%s AND m.observed_at<=%s+INTERVAL '1 minute'
  AND q.official_window_end BETWEEN m.window_end-INTERVAL '5 minutes' AND m.window_end+INTERVAL '5 minutes'
  THEN q.weight*m.points_per_weight END,q.estimated_points) ELSE q.estimated_points END`, asOf, asOf)
}

// Retries/account switches can refresh a reservation estimate after an expansion
// while its original request start still belongs to the old personal period.
// The append-only reserved event records the actual estimate basis in both old
// and new installations, avoiding a schema backfill or a second discount.
func codingReservationBasisSQL(asOf string) string {
	return fmt.Sprintf(`COALESCE((SELECT MAX(e.created_at) FROM coding_quota_reservation_events e
 WHERE e.reservation_id=r.id AND e.state='reserved' AND e.created_at<=%s),r.started_at)`, asOf)
}
