"""Opt-in real PostgreSQL validation of the read-only release snapshot.

RUN_DELIVERY_POSTGRES=1 creates and removes its own disposable local container.
It never connects to the application database or accepts a production DSN.
"""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
import unittest
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from quota_snapshot import snapshot
from release_server import assert_preserved
from delivery_core import DeliveryError


@unittest.skipUnless(os.environ.get('RUN_DELIVERY_POSTGRES') == '1', 'Set RUN_DELIVERY_POSTGRES=1 for disposable PostgreSQL')
class SnapshotPostgresTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.name = 'mile-delivery-test-' + uuid.uuid4().hex[:10]
        if not shutil.which('docker'): raise RuntimeError('Docker is required for the requested PostgreSQL test')
        subprocess.run(['docker', 'run', '-d', '--name', cls.name, '--tmpfs', '/var/lib/postgresql',
                        '-e', 'POSTGRES_USER=delivery_test', '-e', 'POSTGRES_DB=delivery_test',
                        '-e', 'POSTGRES_PASSWORD=local_test_only', 'postgres:18-alpine'],
                       check=True, stdout=subprocess.DEVNULL, timeout=60)
        cls.addClassCleanup(lambda: subprocess.run(['docker', 'rm', '-f', cls.name], check=True, stdout=subprocess.DEVNULL, timeout=30))
        cls.env = {'POSTGRES_USER': 'delivery_test', 'POSTGRES_DB': 'delivery_test'}
        for attempt in range(40):
            # The image briefly starts a socket-only init server; wait for the final TCP server.
            p = subprocess.run(['docker', 'exec', cls.name, 'pg_isready', '-h', '127.0.0.1', '-U', 'delivery_test', '-d', 'delivery_test'], capture_output=True)
            if p.returncode == 0: break
            time.sleep(.25)
        else: raise RuntimeError('Disposable PostgreSQL did not become ready')
    @classmethod
    def sql(cls, text):
        return subprocess.check_output(['docker', 'exec', '-i', cls.name, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1', '-U', 'delivery_test', '-d', 'delivery_test'], input=text, text=True, timeout=20)
    def test_real_queries_preserve_fixed_cycles_and_detect_changed_charge(self):
        self.sql("""
        CREATE TABLE users(id bigint PRIMARY KEY, created_at timestamptz, deleted_at timestamptz);
        CREATE TABLE user_purpose_allowances(user_id bigint,purpose text,window_start timestamptz,limit_amount numeric,used_amount numeric,period text);
        CREATE TABLE coding_quota_reset_events(id text PRIMARY KEY,status text,effective_at timestamptz);
        CREATE TABLE coding_quota_corrections(id bigint,delta numeric);
        CREATE TABLE coding_quota_reconciliations(id bigint);
        CREATE TABLE scoped_admin_api_keys(id bigint,policy jsonb,enabled bool,revoked_at timestamptz,deleted_at timestamptz,expires_at timestamptz,allowed_ips jsonb,created_by bigint);
        CREATE TABLE coding_quota_reservations(id bigint);
        CREATE TABLE coding_quota_reservation_events(id bigint);
        CREATE TABLE coding_quota_billing_tasks(id bigint);
        CREATE TABLE coding_quota_async_jobs(job_id text,user_id bigint,api_key_id bigint,reservation_id bigint);
        CREATE TABLE schema_migrations(filename text PRIMARY KEY,checksum text,applied_at timestamptz);
        CREATE TABLE coding_quota_requests(id bigint PRIMARY KEY,user_id bigint,purpose text,api_key_id bigint,account_id bigint,started_at timestamptz,completed_at timestamptz,weight numeric,points numeric,estimated_points numeric,state text);
        INSERT INTO users VALUES(1,'2026-10-01T00:00:00Z',NULL);
        INSERT INTO coding_quota_reset_events VALUES('verified-event','confirmed','2026-11-11T00:00:00Z');
        INSERT INTO coding_quota_requests VALUES(1,1,'openai',1,1,'2026-11-12T00:00:00Z','2026-11-12T00:01:00Z',1,10,0,'settled');
        INSERT INTO schema_migrations VALUES('001.sql','original','2026-10-01T00:00:00Z');
        INSERT INTO coding_quota_billing_tasks VALUES(1);
        """)
        at = '2026-11-13T00:00:00+00:00'
        before = snapshot(self.name, self.env, at)
        openai = next(c for c in before['clocks'] if c['purpose'] == 'openai')
        self.assertEqual(openai['start'], '2026-11-11T00:00:00+00:00')
        self.assertEqual(openai['resets_at'], '2026-11-18T00:00:00+00:00')
        self.sql('INSERT INTO coding_quota_billing_tasks VALUES(2);')
        assert_preserved(before, snapshot(self.name, self.env, at))
        self.sql('UPDATE coding_quota_requests SET points=0 WHERE id=1;')
        with self.assertRaises(DeliveryError): assert_preserved(before, snapshot(self.name, self.env, at))
        self.sql('CREATE TABLE coding_quota_capacity_events(id text);')
        self.assertEqual(snapshot(self.name, self.env, at)['immutable']['capacity_events'], [])
        self.sql("CREATE TABLE personal_quota_resets(id text); CREATE TABLE personal_quota_reset_requests(request_row_id bigint,reset_id text); INSERT INTO personal_quota_resets VALUES('retained-personal'); INSERT INTO personal_quota_reset_requests VALUES(1,'retained-personal');")
        personal = snapshot(self.name, self.env, at)
        self.assertEqual(personal['immutable']['personal_resets'], [{'id': 'retained-personal'}])
        self.assertEqual(personal['immutable']['personal_reset_requests'], [{'request_row_id': 1, 'reset_id': 'retained-personal'}])
        self.sql('DELETE FROM personal_quota_resets;')
        with self.assertRaises(DeliveryError): assert_preserved(personal, snapshot(self.name, self.env, at))

    def test_coding_preview_rollback_query_uses_real_postgres(self):
        from coding_upgrade import CodingRuntime, UnifiedCodingUpgrade, contract_for_release
        self.sql('CREATE TABLE groups(id bigint,platform text,usage_kind text,deleted_at timestamptz);')
        runtime = object.__new__(CodingRuntime)
        runtime.database, runtime.db_env = self.name, self.env
        policy = UnifiedCodingUpgrade(contract_for_release())
        self.assertTrue(policy.can_rollback(runtime))
        self.sql("INSERT INTO groups VALUES(1,'openai','coding',NULL);")
        self.assertTrue(policy.can_rollback(runtime))
        self.sql("INSERT INTO groups VALUES(2,'composite','coding',NULL);")
        self.assertFalse(policy.can_rollback(runtime))


if __name__ == '__main__': unittest.main()
