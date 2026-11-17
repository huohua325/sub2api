"""Read-only persistence evidence for frontend-only upgrades; no ledger writes."""
from datetime import datetime
import json
import subprocess
import tempfile
import threading

from delivery_core import DeliveryError


def build_snapshot_script(stamp, optional_tables):
    immutable = {
        'reset_events': "SELECT * FROM coding_quota_reset_events WHERE status='confirmed'",
        'corrections': 'SELECT * FROM coding_quota_corrections',
        'reconciliations': 'SELECT * FROM coding_quota_reconciliations',
        'anchors': "SELECT u.id,u.created_at,a.purpose,a.window_start,a.limit_amount,a.used_amount,a.period FROM users u LEFT JOIN user_purpose_allowances a ON a.user_id=u.id WHERE u.deleted_at IS NULL",
        'admin_key_policies': 'SELECT id,policy::text,enabled,revoked_at,deleted_at,expires_at,allowed_ips::text,created_by FROM scoped_admin_api_keys',
    }
    optional = [('capacity_events', 'coding_quota_capacity_events'),
                ('personal_resets', 'personal_quota_resets'),
                ('personal_reset_requests', 'personal_quota_reset_requests')]
    for key, table in optional:
        if table in optional_tables: immutable[key] = 'SELECT * FROM ' + table
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
    tables = ['coding_quota_reservations', 'coding_quota_reservation_events', 'coding_quota_billing_tasks', 'coding_quota_async_jobs']
    result = {'as_of': stamp, 'immutable': {key: [] for key in [*immutable, *(key for key, _ in optional)]},
              'durable_ids': {table: [] for table in tables}, 'requests': [], 'clocks': [], 'migrations': []}
    statements = ["BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL statement_timeout='20s'; SET LOCAL jit=off;"]
    for key, query in immutable.items():
        statements.append("SELECT json_build_array('immutable." + key + "',to_jsonb(s)) FROM (" + query + ') s;')
    for table in tables[:-1]:
        statements.append("SELECT json_build_array('durable_ids." + table + "',id) FROM " + table + ' ORDER BY id;')
    statements.append("SELECT json_build_array('durable_ids.coding_quota_async_jobs',concat_ws(':',job_id,user_id,api_key_id,reservation_id)) FROM coding_quota_async_jobs ORDER BY job_id,user_id,api_key_id;")
    queries = {'requests': 'SELECT id,user_id,purpose,api_key_id,account_id,started_at,completed_at,weight::text,points::text,estimated_points::text,state FROM coding_quota_requests ORDER BY id',
               'clocks': clocks, 'migrations': 'SELECT * FROM schema_migrations ORDER BY filename'}
    for key, query in queries.items():
        statements.append("SELECT json_build_array('" + key + "',to_jsonb(s)) FROM (" + query + ') s;')
    statements.append('COMMIT;')
    return '\n'.join(statements), result


def stream_rows(command, script):
    # File-backed stdin avoids pipe deadlock while psql streams earlier SELECTs.
    # The result grows in Python, never as one PostgreSQL aggregate allocation.
    with tempfile.TemporaryFile() as source, tempfile.TemporaryFile() as errors:
        source.write(script.encode('utf-8'))
        source.seek(0)
        with subprocess.Popen(command, stdin=source, stdout=subprocess.PIPE, stderr=errors,
                              text=True, encoding='utf-8') as process:
            timer = threading.Timer(90, process.kill)
            timer.start()
            try:
                for line in process.stdout:
                    if line.strip(): yield line
                if process.wait() != 0:
                    raise DeliveryError('Read-only quota snapshot command failed; partial rows rejected')
            finally:
                timer.cancel()
                if process.poll() is None:
                    process.kill()
                    process.wait()


def snapshot(container, env, as_of):
    # Canonical parsing prevents caller-provided text from becoming SQL syntax.
    at = datetime.fromisoformat(as_of)
    if at.tzinfo is None: raise DeliveryError('Snapshot reference time needs a timezone')
    stamp = at.isoformat()
    command = ['docker', 'exec', '-i', container, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1', '-v', 'FETCH_COUNT=1000',
               '-U', env['POSTGRES_USER'], '-d', env['POSTGRES_DB']]
    optional = set()
    for table in ['coding_quota_capacity_events', 'personal_quota_resets', 'personal_quota_reset_requests']:
        query = "BEGIN READ ONLY; SET LOCAL statement_timeout='20s'; SELECT to_regclass('" + table + "') IS NOT NULL; COMMIT;"
        if subprocess.check_output(command, input=query, text=True, timeout=30).strip() == 't': optional.add(table)
    script, result = build_snapshot_script(stamp, optional)
    sections = {key: result[key] for key in ['requests', 'clocks', 'migrations']}
    sections.update({'immutable.' + key: values for key, values in result['immutable'].items()})
    sections.update({'durable_ids.' + key: values for key, values in result['durable_ids'].items()})
    for line in stream_rows(command, script):
        try:
            key, row = json.loads(line)
            sections[key].append(row)
        except (ValueError, TypeError, KeyError) as error:
            raise DeliveryError('Invalid quota snapshot row; partial rows rejected') from error
    return result
