-- Read-only evidence, not an automatic authorization to enable enforcement.
-- psql -v shadow_started_at='<deployment.json shadow_started_at>' -f this-file
BEGIN READ ONLY;
SET LOCAL statement_timeout = '20s';
WITH boundary AS (
  SELECT :'shadow_started_at'::timestamptz AS started
), purposes(purpose) AS (VALUES ('openai'), ('grok')),
checks AS (
  SELECT p.purpose, b.started AS shadow_started_at,
    CURRENT_TIMESTAMP >= b.started + INTERVAL '24 hours' AS age_ok,
    (SELECT count(*) FROM coding_quota_reservations r
      WHERE r.purpose=p.purpose AND r.mode='shadow' AND r.started_at>=b.started
        AND r.state='captured') AS completed,
    (SELECT count(*) FROM coding_quota_reservations r
      WHERE r.purpose=p.purpose AND r.started_at>=b.started
        AND r.state='unresolved') AS unresolved,
    (SELECT count(*) FROM coding_quota_reservations r
      WHERE r.purpose=p.purpose AND r.started_at>=b.started
        AND r.state IN ('reserved','sent','handoff') AND r.expires_at<CURRENT_TIMESTAMP)
      AS expired_active,
    (SELECT count(*) FROM coding_quota_billing_tasks t
      JOIN coding_quota_reservations r ON r.id=t.reservation_id
      WHERE r.purpose=p.purpose AND r.started_at>=b.started
        AND (t.state='failed' OR (t.state='pending' AND t.last_error<>''))) AS retry_failures,
    (SELECT count(*) FROM coding_quota_billing_tasks t
      JOIN coding_quota_reservations r ON r.id=t.reservation_id
      WHERE r.purpose=p.purpose AND r.started_at>=b.started AND t.state='pending')
      AS pending_tasks
  FROM purposes p CROSS JOIN boundary b
)
SELECT *, age_ok AND completed>=20 AND unresolved=0 AND expired_active=0
  AND retry_failures=0 AND pending_tasks=0 AS automatic_checks_ready
FROM checks ORDER BY purpose;
COMMIT;
