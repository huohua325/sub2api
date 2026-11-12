"""Read-only persistence evidence for frontend-only upgrades; no ledger writes."""
from datetime import datetime
import json
import subprocess

from delivery_core import DeliveryError


def snapshot(container, env, as_of):
    # Canonical parsing prevents caller-provided text from becoming SQL syntax.
    at = datetime.fromisoformat(as_of)
    if at.tzinfo is None:
        raise DeliveryError('Snapshot reference time needs a timezone')
    stamp = at.isoformat()
    command = ['docker', 'exec', '-i', container, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
               '-U', env['POSTGRES_USER'], '-d', env['POSTGRES_DB']]
    def sql(query):
        text = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL statement_timeout='20s'; " + query + '; COMMIT;'
        return subprocess.check_output(command, input=text, text=True, timeout=30).strip()
    capacity = sql("SELECT to_regclass('coding_quota_capacity_events') IS NOT NULL") == 't'
    def rows(query):
        return "(SELECT COALESCE(jsonb_agg(to_jsonb(s)), '[]'::jsonb) FROM (" + query + ') s)'
    immutable = {
        'reset_events': "SELECT * FROM coding_quota_reset_events WHERE status='confirmed'",
        'corrections': 'SELECT * FROM coding_quota_corrections',
        'reconciliations': 'SELECT * FROM coding_quota_reconciliations',
        'anchors': "SELECT u.id,u.created_at,a.purpose,a.window_start,a.limit_amount,a.used_amount,a.period FROM users u LEFT JOIN user_purpose_allowances a ON a.user_id=u.id WHERE u.deleted_at IS NULL",
        'admin_key_policies': 'SELECT id,policy::text,enabled,revoked_at,deleted_at,expires_at,allowed_ips::text,created_by FROM scoped_admin_api_keys',
    }
    immutable_sql = ','.join("'" + name + "'," + rows(query) for name, query in immutable.items())
    immutable_sql += ",'capacity_events'," + (rows('SELECT * FROM coding_quota_capacity_events') if capacity else "'[]'::jsonb")
    for key, table in [('personal_resets', 'personal_quota_resets'),
                       ('personal_reset_requests', 'personal_quota_reset_requests')]:
        exists = sql("SELECT to_regclass('" + table + "') IS NOT NULL") == 't'
        immutable_sql += ",'" + key + "'," + (rows('SELECT * FROM ' + table) if exists else "'[]'::jsonb")
    # Same original wallet anchors and confirmed-reset formula as the runbook.
    clocks = f"""WITH anchors AS (
      SELECT u.id,p.purpose,GREATEST(COALESCE(a.window_start,u.created_at),
       CASE WHEN p.purpose='openai' THEN COALESCE((SELECT MAX(effective_at) FROM coding_quota_reset_events
        WHERE status='confirmed' AND effective_at<='{stamp}'::timestamptz),'-infinity'::timestamptz)
        ELSE '-infinity'::timestamptz END) AS anchor
      FROM users u CROSS JOIN(VALUES('openai'),('grok')) p(purpose)
      LEFT JOIN user_purpose_allowances a ON a.user_id=u.id AND a.purpose=p.purpose WHERE u.deleted_at IS NULL
     ), windows AS (
      SELECT *,anchor+FLOOR(GREATEST(EXTRACT(EPOCH FROM('{stamp}'::timestamptz-anchor)),0)/604800)*INTERVAL '168 hours' AS start FROM anchors
     ) SELECT id,purpose,start,start+INTERVAL '168 hours' AS resets_at FROM windows ORDER BY id,purpose"""
    durable = []
    for table in ['coding_quota_reservations', 'coding_quota_reservation_events', 'coding_quota_billing_tasks']:
        durable.append("'" + table + "',(SELECT COALESCE(jsonb_agg(id ORDER BY id),'[]'::jsonb) FROM " + table + ')')
    # Async jobs have a composite key rather than a generic id column.
    durable.append("'coding_quota_async_jobs',(SELECT COALESCE(jsonb_agg(concat_ws(':',job_id,user_id,api_key_id,reservation_id) ORDER BY job_id,user_id,api_key_id),'[]'::jsonb) FROM coding_quota_async_jobs)")
    query = "SELECT jsonb_build_object('as_of','" + stamp + "','immutable',jsonb_build_object(" + immutable_sql + "),'durable_ids',jsonb_build_object(" + ','.join(durable) + "),'requests'," + rows('SELECT id,user_id,purpose,api_key_id,account_id,started_at,completed_at,weight::text,points::text,estimated_points::text,state FROM coding_quota_requests ORDER BY id') + ",'clocks'," + rows(clocks) + ",'migrations'," + rows('SELECT * FROM schema_migrations ORDER BY filename') + ')'
    return json.loads(sql(query))
