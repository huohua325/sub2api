import copy
import ctypes
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
import uuid
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError
import upstream_additive_upgrade as upgrade


class AdditiveUpgradeTests(unittest.TestCase):
    def setUp(self):
        self.policy = upgrade.AdditiveUpstreamUpgrade(upgrade.contract_for_release())
        self.before = dict(as_of='fixed', clocks=['old clock'],
                           migrations=[dict(filename='old.sql', checksum='unchanged')],
                           immutable={'personal_resets': [{'id': 1}]}, durable_ids={'billing_tasks': [1]},
                           requests=[dict(id=1, state='settled', points='2')],
                           access={'grants': [], 'keys': [1]}, ultrafast_enabled=False)
        self.after = copy.deepcopy(self.before)
        self.after['migrations'] += [dict(filename=k, checksum=v) for k, v in upgrade.DATABASE_MIGRATIONS.items()]

    def test_reviewed_source_and_tool_only_descendants(self):
        root = Path(__file__).resolve().parents[2]
        source = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
        self.policy.validate_sources(root, upgrade.BASELINE, source)
        for base, tip in [('0' * 40, source), (upgrade.BASELINE, upgrade.PARENTS[0])]:
            with self.assertRaises(DeliveryError): self.policy.validate_sources(root, base, tip)
        with patch('upstream_additive_upgrade.digest', return_value='0' * 64):
            with self.assertRaises(DeliveryError): self.policy.validate_sources(root, upgrade.BASELINE, source)

    def test_only_fixed_contract_and_manifest_are_allowed(self):
        valid = dict(backend_unchanged=False, baseline_commit=upgrade.BASELINE,
                     upgrade_contract=upgrade.contract_for_release())
        self.policy.validate(valid)
        for field in ['baseline_commit', 'merge_commit', 'migrations', 'database_migrations']:
            contract = upgrade.contract_for_release(); contract[field] = {}
            with self.assertRaises(DeliveryError): upgrade.AdditiveUpstreamUpgrade(contract)
        with self.assertRaises(DeliveryError): self.policy.validate(dict(valid, backend_unchanged=True))

    def test_exact_additions_preserve_all_existing_business_data(self):
        self.policy.assert_preserved(self.before, self.after)
        for field, value in [('clocks', []), ('requests', []), ('immutable', {}),
                             ('durable_ids', {}), ('access', {'grants': [99]}), ('ultrafast_enabled', True)]:
            with self.subTest(field=field), self.assertRaises(DeliveryError):
                self.policy.assert_preserved(self.before, dict(self.after, **{field: value}))
        for position in range(3):
            changed = copy.deepcopy(self.after); changed['migrations'][position]['checksum'] = 'wrong'
            with self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, changed)
        with self.assertRaises(DeliveryError):
            self.policy.assert_preserved(self.before, dict(self.after, migrations=self.after['migrations'][:-1]))

    def test_rollback_respects_new_feature_use(self):
        class Runtime:
            def access_snapshot(inner): return {'ultrafast_enabled': inner.enabled}
        runtime = Runtime(); runtime.enabled = False
        self.assertTrue(self.policy.can_rollback(runtime))
        runtime.enabled = True
        self.assertFalse(self.policy.can_rollback(runtime))

    def test_deploy_and_post_switch_failure_preserve_database(self):
        from release_server import deploy
        import test_release_server as fixtures
        for fail_health in [False, True]:
            with self.subTest(fail_health=fail_health):
                fixture = fixtures.ReleaseServerTests('test_dry_run_does_not_stop_or_create_backup')
                fixture.setUp(); self.addCleanup(fixture.doCleanups)
                path = fixture.release / 'manifest.json'
                manifest = json.loads(path.read_text())
                manifest.update(backend_unchanged=False, baseline_commit=upgrade.BASELINE,
                                upgrade_contract=upgrade.contract_for_release())
                path.write_text(json.dumps(manifest))
                before, after = self.before, self.after
                class Runtime(fixtures.FakeRuntime):
                    def snapshot(inner, at):
                        data = copy.deepcopy(after if 'start' in inner.calls else before)
                        data['as_of'] = at
                        return data
                    def access_snapshot(inner): return {'ultrafast_enabled': False}
                    def health(inner):
                        if fail_health and not getattr(inner, 'failed_once', False):
                            inner.failed_once = True
                            raise DeliveryError('simulated candidate unhealthy')
                        return super().health()
                runtime = Runtime()
                if fail_health:
                    with self.assertRaises(DeliveryError):
                        deploy(fixture.root, fixture.release, runtime, apply=True, upgrade=self.policy)
                    self.assertEqual(fixture.active.read_bytes(), b'old binary')
                    self.assertTrue(json.loads((fixture.release / 'deployment.json').read_text())['program_rolled_back'])
                else:
                    result = deploy(fixture.root, fixture.release, runtime, apply=True, upgrade=self.policy)
                    self.assertEqual(result['status'], 'deployed')
                    self.assertEqual(fixture.active.read_bytes(), b'new binary')


@unittest.skipUnless(os.environ.get('MILE_DELIVERY_TEST_LIBPQ'), 'Opt in to isolated local native PostgreSQL')
class AdditivePostgresTests(unittest.TestCase):
    def test_real_access_query_before_after_migrations_and_rollback_guard(self):
        # Fixed loopback test instance only. No production DSN is accepted.
        libpath = Path(os.environ['MILE_DELIVERY_TEST_LIBPQ'])
        with os.add_dll_directory(str(libpath.parent)): lib = ctypes.CDLL(str(libpath))
        for name, args, result in [
            ('PQconnectdb', [ctypes.c_char_p], ctypes.c_void_p),
            ('PQstatus', [ctypes.c_void_p], ctypes.c_int),
            ('PQexec', [ctypes.c_void_p, ctypes.c_char_p], ctypes.c_void_p),
            ('PQresultStatus', [ctypes.c_void_p], ctypes.c_int),
            ('PQgetvalue', [ctypes.c_void_p, ctypes.c_int, ctypes.c_int], ctypes.c_char_p),
            ('PQresultErrorMessage', [ctypes.c_void_p], ctypes.c_char_p),
            ('PQclear', [ctypes.c_void_p], None), ('PQfinish', [ctypes.c_void_p], None)]:
            func = getattr(lib, name); func.argtypes = args; func.restype = result
        conn = lib.PQconnectdb(b'host=127.0.0.1 port=55431 user=mile_upstream_20261117 dbname=mile_upstream_20261117 connect_timeout=5')
        self.addCleanup(lib.PQfinish, conn)
        self.assertEqual(lib.PQstatus(conn), 0, 'Isolated PostgreSQL must be running')
        def sql(text, read=False):
            result = lib.PQexec(conn, text.encode('utf-8'))
            try:
                self.assertIn(lib.PQresultStatus(result), [1, 2], lib.PQresultErrorMessage(result).decode())
                return lib.PQgetvalue(result, 0, 0).decode() if read else None
            finally: lib.PQclear(result)
        schema = 'delivery_additive_' + uuid.uuid4().hex
        sql('CREATE SCHEMA ' + schema + '; SET search_path TO ' + schema)
        self.addCleanup(sql, 'DROP SCHEMA ' + schema + ' CASCADE')
        sql('''CREATE TABLE groups(id bigint,platform text,usage_kind text,is_exclusive boolean,
            status text,subscription_type text,force_openai_fast boolean,model_allowlist jsonb,
            require_oauth_only boolean,deleted_at timestamptz);
            INSERT INTO groups VALUES(17,'composite','coding',true,'active','standard',true,'{}',true,NULL);
            CREATE TABLE api_keys(id bigint,user_id bigint,group_id bigint,deleted_at timestamptz);
            INSERT INTO api_keys VALUES(1,1,17,NULL);
            CREATE TABLE user_allowed_groups(user_id bigint,group_id bigint);
            CREATE TABLE composite_model_routes(id bigint,group_id bigint,public_model text);
            INSERT INTO composite_model_routes VALUES(1,17,'gpt-6.1-sol');
            CREATE TABLE usage_group_rollup_state(id smallint,closed_before date,updated_at timestamptz);''')
        def access():
            # Runtime psql emits SELECT rows even when COMMIT is last; libpq
            # returns only the last statement, so execute transaction pieces.
            sql('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY')
            try:
                sql("SET LOCAL statement_timeout='15s'")
                body = upgrade.ACCESS_QUERY.split("SET LOCAL statement_timeout='15s';", 1)[1].rsplit('COMMIT;', 1)[0]
                return json.loads(sql(body, read=True))
            finally: sql('COMMIT')
        before = access(); self.assertFalse(before['ultrafast_enabled'])
        root = Path(__file__).resolve().parents[2]
        for _ in range(2):
            sql('BEGIN')
            for name in upgrade.MIGRATIONS: sql((root / 'backend/migrations' / name).read_text(encoding='utf-8'))
            sql('COMMIT')
        self.assertEqual(before, access())
        runtime = object.__new__(upgrade.AdditiveRuntime); runtime.access_snapshot = access
        policy = upgrade.AdditiveUpstreamUpgrade(upgrade.contract_for_release())
        self.assertTrue(policy.can_rollback(runtime))
        sql('UPDATE groups SET force_openai_ultrafast=true')
        self.assertFalse(policy.can_rollback(runtime))
        self.assertTrue(access()['groups'][0]['force_openai_fast'])


if __name__ == '__main__': unittest.main()
