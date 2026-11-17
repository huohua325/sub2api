"""Exercise pinned standby deployment, preservation and rollback without production access."""
import copy
import ctypes
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import uuid
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError, digest
import api_fallback_upgrade as upgrade

ROOT = Path(__file__).resolve().parents[2]
FILES = ('270_api_fallback_metering.sql', '271_api_fallback_control.sql')


def migrations():
    return {name: digest(ROOT / 'backend/migrations' / name) for name in FILES}


def database_migrations():
    return {name: hashlib.sha256((ROOT / 'backend/migrations' / name).read_text(encoding='utf-8').strip().encode()).hexdigest()
            for name in FILES}


def empty_fallback():
    return {name: [] for name in (*upgrade.TABLES, 'api_fallback_requests')}


class StandbyUpgradeTests(unittest.TestCase):
    def setUp(self):
        self.constants = patch.multiple(upgrade, APPLICATION='a' * 40,
                                        MIGRATIONS=migrations(), DATABASE_MIGRATIONS=database_migrations())
        self.constants.start(); self.addCleanup(self.constants.stop)
        self.policy = upgrade.APIFallbackUpgrade(upgrade.contract_for_release())
        self.before = dict(as_of='fixed', clocks=[{'user': 1, 'anchor': 'original', 'end': 'original end'}],
                           migrations=[dict(filename='old.sql', checksum='original checksum')],
                           immutable={'resets': [{'id': 'confirmed', 'at': 'original'}], 'capacity': [{'id': 1}],
                                      'personal_resets': [{'id': 1}], 'corrections': [{'request': 'old'}]},
                           durable_ids={'billing_tasks': [1], 'reservations': [2]},
                           requests=[dict(id=1, state='settled', points='2')],
                           access={'groups': [{'id': 17}], 'keys': [{'id': 1, 'group': 17}], 'grants': [], 'routes': [1]},
                           ultrafast_enabled=False, fallback=empty_fallback())
        self.after = copy.deepcopy(self.before)
        self.after['migrations'] += [dict(filename=k, checksum=v) for k, v in upgrade.DATABASE_MIGRATIONS.items()]

    def test_unfrozen_incomplete_or_modified_contract_is_rejected(self):
        valid = dict(backend_unchanged=False, baseline_commit=upgrade.BASELINE, upgrade_contract=upgrade.contract_for_release())
        self.policy.validate(valid)
        for field in ('baseline_commit', 'source_commit', 'migrations', 'database_migrations'):
            contract = upgrade.contract_for_release(); contract[field] = 'changed'
            with self.subTest(field=field), self.assertRaises(DeliveryError): upgrade.APIFallbackUpgrade(contract)
        for field, value in [('backend_unchanged', True), ('baseline_commit', '0' * 40), ('upgrade_contract', {})]:
            with self.subTest(field=field), self.assertRaises(DeliveryError): self.policy.validate(dict(valid, **{field: value}))
        with patch.object(upgrade, 'APPLICATION', 'UNFROZEN'):
            with self.assertRaises(DeliveryError): upgrade.APIFallbackUpgrade(upgrade.contract_for_release())
        with patch.object(upgrade, 'MIGRATIONS', {FILES[0]: '0' * 64}):
            with self.assertRaises(DeliveryError): upgrade.APIFallbackUpgrade(upgrade.contract_for_release())

    def test_sources_require_pinned_ancestry_exact_additions_and_matching_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            def git(*args):
                return subprocess.check_output(['git', *args], cwd=root, text=True, stderr=subprocess.PIPE).strip()
            def commit(message):
                git('add', '-A'); git('-c', 'user.name=Delivery test', '-c', 'user.email=delivery@example.invalid', 'commit', '-qm', message)
                return git('rev-parse', 'HEAD')
            git('init', '-q')
            folder = root / 'backend/migrations'; folder.mkdir(parents=True)
            (folder / 'old.sql').write_text('SELECT 1;')
            baseline = commit('baseline')
            for name in FILES: (folder / name).write_bytes((ROOT / 'backend/migrations' / name).read_bytes())
            application = commit('reviewed application')
            (root / 'notes.md').write_text('Documentation only')
            descendant = commit('documentation')
            with patch.multiple(upgrade, BASELINE=baseline, APPLICATION=application):
                policy = upgrade.APIFallbackUpgrade(upgrade.contract_for_release())
                policy.validate_sources(root, baseline, application)
                policy.validate_sources(root, baseline, descendant)
                with self.assertRaises(DeliveryError): policy.validate_sources(root, '0' * 40, descendant)
                with self.assertRaises((DeliveryError, subprocess.CalledProcessError)):
                    policy.validate_sources(root, baseline, baseline)
                (root / 'backend/changed.go').write_text('package changed')
                changed = commit('unreviewed source')
                with self.assertRaises(DeliveryError): policy.validate_sources(root, baseline, changed)
                with patch('api_fallback_upgrade.digest', return_value='0' * 64):
                    with self.assertRaises(DeliveryError): policy.validate_sources(root, baseline, descendant)
            (folder / 'old.sql').write_text('SELECT 2;')
            changed_migration = commit('old migration modified')
            with patch.multiple(upgrade, BASELINE=baseline, APPLICATION=changed_migration):
                policy = upgrade.APIFallbackUpgrade(upgrade.contract_for_release())
                with self.assertRaises(DeliveryError): policy.validate_sources(root, baseline, changed_migration)

    def test_preserves_all_business_state_and_only_the_two_fixed_migrations(self):
        self.policy.assert_preserved(self.before, self.after)
        changes = [('clocks', []), ('requests', []), ('immutable', {}), ('durable_ids', {}),
                   ('access', {'grants': [99]}), ('ultrafast_enabled', True)]
        for field, value in changes:
            with self.subTest(field=field), self.assertRaises(DeliveryError):
                self.policy.assert_preserved(self.before, dict(self.after, **{field: value}))
        for position in range(3):
            after = copy.deepcopy(self.after); after['migrations'][position]['checksum'] = 'changed'
            with self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, after)
        with self.assertRaises(DeliveryError):
            self.policy.assert_preserved(self.before, dict(self.after, migrations=self.after['migrations'][:-1]))
        extra = copy.deepcopy(self.after); extra['migrations'].append({'filename': 'unexpected.sql', 'checksum': 'x'})
        with self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, extra)
        for table in (*upgrade.TABLES, 'api_fallback_requests'):
            after = copy.deepcopy(self.after); after['fallback'][table].append({'id': 'unexpected'})
            with self.subTest(table=table), self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, after)

    def test_initial_upgrade_requires_disabled_policies(self):
        before, after = copy.deepcopy(self.before), copy.deepcopy(self.after)
        for value in (before, after):
            value['fallback']['api_fallback_policies'].append({'id': 'draft', 'enabled': False, 'config': {'enabled': False}})
        self.policy.assert_preserved(before, after)
        for column, config in [(True, False), (False, True), (True, True)]:
            for value in (before, after):
                value['fallback']['api_fallback_policies'][0].update(enabled=column, config={'enabled': config})
            with self.subTest(column=column, config=config), self.assertRaises(DeliveryError):
                self.policy.assert_preserved(before, after)

    def test_preflight_rejects_prior_spending_and_either_enabled_flag(self):
        runtime = object.__new__(upgrade.APIFallbackRuntime)
        runtime.fallback_snapshot = empty_fallback
        preflight = {'modes': ['shadow', 'shadow'], 'environment_hash': 'preserved'}
        with patch('upstream_additive_upgrade.AdditiveRuntime.preflight', return_value=preflight):
            self.assertEqual(runtime.preflight(), preflight)
            for table, row in [('api_fallback_attempts', {'id': 'old'}), ('api_fallback_requests', {'id': 'fixed'}),
                               ('api_fallback_policies', {'enabled': True, 'config': {'enabled': False}}),
                               ('api_fallback_policies', {'enabled': False, 'config': {'enabled': True}})]:
                data = empty_fallback(); data[table].append(row)
                runtime.fallback_snapshot = lambda: data
                with self.subTest(table=table, row=row), self.assertRaises(DeliveryError): runtime.preflight()

    def test_every_attempt_and_fixed_charge_blocks_old_program_rollback(self):
        class Runtime:
            def fallback_snapshot(inner): return inner.data
        runtime = Runtime(); runtime.data = empty_fallback()
        self.assertTrue(self.policy.can_rollback(runtime))
        for state in ('reserved', 'sent', 'settled', 'unresolved', 'cancelled'):
            runtime.data = empty_fallback(); runtime.data['api_fallback_attempts'].append({'id': 'sent', 'state': state})
            with self.subTest(state=state): self.assertFalse(self.policy.can_rollback(runtime))
        runtime.data = empty_fallback(); runtime.data['api_fallback_requests'].append({'id': 'fixed', 'points': '0.13'})
        self.assertFalse(self.policy.can_rollback(runtime))

    def test_current_or_historical_backup_role_blocks_old_program_rollback(self):
        class Runtime:
            def fallback_snapshot(inner): return inner.data
        runtime = Runtime()
        for table in ('api_fallback_policies', 'api_fallback_policy_versions'):
            for config in ({'enabled': False, 'backup_account_ids': [20]},
                           {'enabled': False, 'backup_account_ids': [20], 'expires_at': '2000-01-01'},
                           'invalid persisted config'):
                runtime.data = empty_fallback()
                runtime.data[table].append({'config': config})
                with self.subTest(table=table, config=config):
                    self.assertFalse(self.policy.can_rollback(runtime))
        runtime.data = empty_fallback()
        runtime.data['api_fallback_policies'].append({'config': {'enabled': False, 'backup_account_ids': []}})
        self.assertTrue(self.policy.can_rollback(runtime))

    def test_success_and_post_switch_failure_never_restore_database(self):
        from release_server import deploy
        import test_release_server as fixtures
        for fail_health, charged in [(False, False), (True, False), (True, True)]:
            with self.subTest(fail_health=fail_health, charged=charged):
                fixture = fixtures.ReleaseServerTests('test_dry_run_does_not_stop_or_create_backup')
                fixture.setUp(); self.addCleanup(fixture.doCleanups)
                path = fixture.release / 'manifest.json'
                manifest = json.loads(path.read_text())
                manifest.update(backend_unchanged=False, baseline_commit=upgrade.BASELINE, upgrade_contract=upgrade.contract_for_release())
                path.write_text(json.dumps(manifest))
                before, after = self.before, self.after
                class Runtime(fixtures.FakeRuntime):
                    def snapshot(inner, at):
                        data = copy.deepcopy(after if 'start' in inner.calls else before); data['as_of'] = at
                        return data
                    def fallback_snapshot(inner):
                        data = empty_fallback()
                        if charged: data['api_fallback_attempts'].append({'id': 'unknown', 'state': 'unresolved'})
                        return data
                    def health(inner):
                        if fail_health and not getattr(inner, 'failed_once', False):
                            inner.failed_once = True; raise DeliveryError('simulated candidate unhealthy')
                        return super().health()
                runtime = Runtime()
                if fail_health:
                    with self.assertRaises(DeliveryError): deploy(fixture.root, fixture.release, runtime, apply=True, upgrade=self.policy)
                    record = json.loads((fixture.release / 'deployment.json').read_text())
                    self.assertEqual(fixture.active.read_bytes(), b'new binary' if charged else b'old binary')
                    self.assertEqual(bool(record.get('program_rolled_back')), not charged)
                    self.assertEqual(bool(record.get('program_rollback_blocked')), charged)
                else:
                    result = deploy(fixture.root, fixture.release, runtime, apply=True, upgrade=self.policy)
                    self.assertEqual(result['status'], 'deployed')
                    self.assertEqual(fixture.active.read_bytes(), b'new binary')
                self.assertNotIn('restore', runtime.calls)
                self.assertTrue(next((fixture.root / 'backups').glob('*/database.dump')).exists())


@unittest.skipUnless(os.environ.get('MILE_DELIVERY_TEST_LIBPQ'), 'Opt in to isolated local native PostgreSQL')
class StandbyPostgresTests(unittest.TestCase):
    def test_actual_migrations_preserve_access_audits_and_fixed_spending(self):
        libpath = Path(os.environ['MILE_DELIVERY_TEST_LIBPQ'])
        with os.add_dll_directory(str(libpath.parent)): lib = ctypes.CDLL(str(libpath))
        for name, args, result in [
            ('PQconnectdb', [ctypes.c_char_p], ctypes.c_void_p), ('PQstatus', [ctypes.c_void_p], ctypes.c_int),
            ('PQexec', [ctypes.c_void_p, ctypes.c_char_p], ctypes.c_void_p),
            ('PQsendQuery', [ctypes.c_void_p, ctypes.c_char_p], ctypes.c_int),
            ('PQgetResult', [ctypes.c_void_p], ctypes.c_void_p),
            ('PQntuples', [ctypes.c_void_p], ctypes.c_int), ('PQresultStatus', [ctypes.c_void_p], ctypes.c_int),
            ('PQgetvalue', [ctypes.c_void_p, ctypes.c_int, ctypes.c_int], ctypes.c_char_p),
            ('PQresultErrorMessage', [ctypes.c_void_p], ctypes.c_char_p),
            ('PQclear', [ctypes.c_void_p], None), ('PQfinish', [ctypes.c_void_p], None)]:
            function = getattr(lib, name); function.argtypes = args; function.restype = result
        dsn = os.environ.get('MILE_DELIVERY_TEST_PG_DSN',
                             'host=127.0.0.1 port=55432 user=mile_fallback dbname=postgres connect_timeout=5')
        conn = lib.PQconnectdb(dsn.encode())
        self.addCleanup(lib.PQfinish, conn)
        self.assertEqual(lib.PQstatus(conn), 0, 'Isolated PostgreSQL must be running')
        def sql(text, read=False, fail=False):
            result = lib.PQexec(conn, text.encode())
            try:
                if fail:
                    self.assertEqual(lib.PQresultStatus(result), 7)
                    return None
                self.assertIn(lib.PQresultStatus(result), (1, 2), lib.PQresultErrorMessage(result).decode())
                return lib.PQgetvalue(result, 0, 0).decode() if read else None
            finally: lib.PQclear(result)
        def rows(script):
            self.assertEqual(lib.PQsendQuery(conn, script.encode()), 1)
            values = []
            while result := lib.PQgetResult(conn):
                try:
                    self.assertIn(lib.PQresultStatus(result), (1, 2), lib.PQresultErrorMessage(result).decode())
                    values.extend(lib.PQgetvalue(result, index, 0).decode() for index in range(lib.PQntuples(result)))
                finally: lib.PQclear(result)
            return values
        schema = 'delivery_standby_' + uuid.uuid4().hex
        sql('CREATE SCHEMA ' + schema + '; SET search_path TO ' + schema)
        self.addCleanup(sql, 'DROP SCHEMA ' + schema + ' CASCADE')
        sql('''CREATE TABLE users(id bigint PRIMARY KEY);
            INSERT INTO users VALUES(1);
            CREATE TABLE accounts(id bigint PRIMARY KEY); INSERT INTO accounts VALUES(20);
            CREATE TABLE groups(id bigint PRIMARY KEY,platform text,usage_kind text,is_exclusive boolean,
                status text,subscription_type text,force_openai_fast boolean,force_openai_ultrafast boolean,
                model_allowlist jsonb,require_oauth_only boolean,deleted_at timestamptz);
            INSERT INTO groups VALUES(17,'composite','coding',true,'active','standard',false,false,'{}',true,NULL);
            CREATE TABLE api_keys(id bigint,user_id bigint,group_id bigint,deleted_at timestamptz);
            INSERT INTO api_keys VALUES(9,1,17,NULL);
            CREATE TABLE user_allowed_groups(user_id bigint,group_id bigint);
            CREATE TABLE composite_model_routes(id bigint,group_id bigint,public_model text);
            INSERT INTO composite_model_routes VALUES(1,17,'gpt-6.1-sol');
            CREATE TABLE coding_quota_requests(id text PRIMARY KEY,user_id bigint,purpose text,api_key_id bigint,
                account_id bigint,started_at timestamptz,completed_at timestamptz,weight numeric NOT NULL,
                points numeric NOT NULL,estimated_points numeric NOT NULL,state text,official_window_end timestamptz);
            INSERT INTO coding_quota_requests VALUES('original',1,'openai',9,20,'2026-11-16','2026-11-16',1,2,0,'settled',NULL);
            CREATE TABLE reset_audit(id text PRIMARY KEY,body jsonb);
            INSERT INTO reset_audit VALUES('confirmed','{"clock":"original","correction":"kept"}');
            CREATE FUNCTION coding_quota_audit_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
              BEGIN RAISE EXCEPTION 'append-only'; END $$;''')
        def access():
            from upstream_additive_upgrade import ACCESS_QUERY
            return json.loads(rows(ACCESS_QUERY)[0])
        def fixed():
            return sql("SELECT json_agg(json_build_object('id',id,'points',points::text,'weight',weight::text,'state',state)) FROM coding_quota_requests WHERE id='original'", read=True)
        before_access, before_fixed = access(), fixed()
        audit = sql('SELECT body::text FROM reset_audit', read=True)
        runtime = object.__new__(upgrade.APIFallbackRuntime)
        runtime.database = 'isolated-test'; runtime.db_env = {'POSTGRES_USER': 'mile_fallback', 'POSTGRES_DB': 'postgres'}
        def output(command, *, input, **kwargs):
            self.assertEqual(command[:3], ['docker', 'exec', '-i'])
            self.assertIn('pg_tables', input)
            return sql("SELECT COALESCE(json_agg(tablename),'[]') FROM pg_tables WHERE schemaname=current_schema()", read=True)
        def stream(command, script):
            self.assertIn('READ ONLY', script)
            return iter(rows(script))
        with patch('api_fallback_upgrade.subprocess.check_output', side_effect=output), patch('quota_snapshot.stream_rows', side_effect=stream):
            self.assertEqual(runtime.fallback_snapshot(), empty_fallback())
            for name in FILES: sql((ROOT / 'backend/migrations' / name).read_text(encoding='utf-8'))
            self.assertEqual(runtime.fallback_snapshot(), empty_fallback(), 'Migrations must not seed paid policies or charges')
            self.assertEqual(before_access, access())
            self.assertEqual(before_fixed, fixed())
            self.assertEqual(audit, sql('SELECT body::text FROM reset_audit', read=True))
            self.assertEqual(sql("SELECT metering_source FROM coding_quota_requests WHERE id='original'", read=True), 'subscription')
            sql("INSERT INTO api_fallback_policies(id,group_id,platform,model,version,config) VALUES('draft',17,'openai','gpt-6.1-sol',1,'{\"enabled\":false}')")
            self.assertEqual(sql("SELECT enabled::text FROM api_fallback_policies WHERE id='draft'", read=True), 'false')
            sql("INSERT INTO api_fallback_policy_versions(policy_id,version,actor_user_id,config) VALUES('draft',1,1,'{}')")
            sql("UPDATE api_fallback_policy_versions SET config='{}' WHERE policy_id='draft'", fail=True)
            sql("INSERT INTO api_fallback_events(policy_id,actor_user_id,event_type,evidence) VALUES('draft',1,'verified','supplier evidence')")
            sql("DELETE FROM api_fallback_events WHERE policy_id='draft'", fail=True)
            with patch.multiple(upgrade, APPLICATION='a' * 40, MIGRATIONS=migrations(), DATABASE_MIGRATIONS=database_migrations()):
                policy = upgrade.APIFallbackUpgrade(upgrade.contract_for_release())
                self.assertTrue(policy.can_rollback(runtime))
                sql("""INSERT INTO api_fallback_attempts(id,request_id,policy_id,policy_version,policy_snapshot,user_id,
                    api_key_id,account_id,platform,model,state,reserved_usd,reserved_points,started_at)
                    VALUES('unknown','request', 'draft',1,'{}',1,9,20,'openai','gpt-6.1-sol','unresolved',.1,.04,NOW())""")
                self.assertFalse(policy.can_rollback(runtime))
                sql("""INSERT INTO coding_quota_requests(id,user_id,purpose,api_key_id,account_id,started_at,completed_at,
                    weight,points,estimated_points,state,official_window_end,metering_source)
                    VALUES('fixed',1,'openai',9,20,NOW(),NOW(),0,.13,0,'settled',NULL,'api_fallback')""")
                sql("UPDATE coding_quota_requests SET points=0 WHERE id='fixed'", fail=True)
                self.assertEqual(len(runtime.fallback_snapshot()['api_fallback_requests']), 1)
                self.assertFalse(policy.can_rollback(runtime))
                self.assertEqual(before_access, access())
                self.assertEqual(before_fixed, fixed())


if __name__ == '__main__': unittest.main()
