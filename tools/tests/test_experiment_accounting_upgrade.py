"""Prove that only immutable per-request evidence can explain an accounting change."""
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
from delivery_core import DeliveryError
import experiment_accounting_upgrade as upgrade
import experiment_account_standby_upgrade as account_standby
import experiment_key_standby_upgrade as standby_upgrade
import admin_quota_self_reset_upgrade as self_reset


def empty_experiment():
    return {**{name: [] for name in upgrade.TABLES}, 'usage': [], 'policy': []}


class ExperimentKeyStandbyUpgradeTests(unittest.TestCase):
    def test_admin_self_reset_preserves_compatible_rollback_and_pins_sources(self):
        root = Path(__file__).resolve().parents[2]
        contract = self_reset.contract_for_release()
        policy = self_reset.AdminQuotaSelfResetUpgrade(contract)
        policy.validate(dict(backend_unchanged=False, upgrade_contract=contract, baseline_commit=self_reset.BASELINE))
        policy.validate_sources(root, self_reset.BASELINE, self_reset.APPLICATION)
        with self.assertRaises(DeliveryError):
            policy.validate_sources(root, standby_upgrade.BASELINE, self_reset.APPLICATION)
        self.assertTrue(policy.can_rollback(None))

    def test_fixed_source_tree_and_manifest(self):
        root = Path(__file__).resolve().parents[2]
        policy = standby_upgrade.ExperimentKeyStandbyUpgrade(standby_upgrade.contract_for_release())
        manifest = {'backend_unchanged': False, 'upgrade_contract': standby_upgrade.contract_for_release(),
                    'baseline_commit': standby_upgrade.BASELINE}
        policy.validate(manifest)
        policy.validate_sources(root, standby_upgrade.BASELINE, standby_upgrade.APPLICATION)
        with self.assertRaises(DeliveryError):
            policy.validate_sources(root, standby_upgrade.APPLICATION, standby_upgrade.APPLICATION)

    def test_experiment_key_in_current_or_prior_policy_blocks_rollback(self):
        from types import SimpleNamespace
        policy = standby_upgrade.ExperimentKeyStandbyUpgrade(standby_upgrade.contract_for_release())
        current, histories = [], []
        runtime = SimpleNamespace(fallback_snapshot=lambda: {
            'api_fallback_policies': current, 'api_fallback_policy_versions': histories,
        })
        self.assertTrue(policy.can_rollback(runtime))
        for rows in (current, histories):
            rows.append({'config': {'experiment_key_id': 7, 'enabled': False}})
            self.assertFalse(policy.can_rollback(runtime))
            rows.clear()

    def test_account_release_has_a_fixed_baseline_and_quarantines_rollback(self):
        from types import SimpleNamespace
        root = Path(__file__).resolve().parents[2]
        contract = account_standby.contract_for_release()
        policy = account_standby.ExperimentAccountStandbyUpgrade(contract)
        policy.validate(dict(backend_unchanged=False, upgrade_contract=contract, baseline_commit=account_standby.BASELINE))
        policy.validate_sources(root, account_standby.BASELINE, account_standby.APPLICATION)
        with self.assertRaises(DeliveryError):
            policy.validate_sources(root, standby_upgrade.BASELINE, account_standby.APPLICATION)
        self.assertTrue(policy.can_rollback(SimpleNamespace(account_standby_snapshot=lambda: [])))
        self.assertFalse(policy.can_rollback(SimpleNamespace(account_standby_snapshot=lambda: [[8, [17]]])))


class AccountingUpgradeTests(unittest.TestCase):
    def setUp(self):
        self.migrations = {name: hashlib.sha256(name.encode()).hexdigest() for name in upgrade.MIGRATIONS}
        constants = patch.multiple(upgrade, APPLICATION='a' * 40, MIGRATIONS=self.migrations,
                                   DATABASE_MIGRATIONS=self.migrations)
        constants.start(); self.addCleanup(constants.stop)
        self.policy = upgrade.ExperimentAccountingUpgrade(upgrade.contract_for_release())
        self.before = {
            'as_of': '2026-11-18T00:00:00+08:00', 'clocks': [{'user': 3, 'original': True}],
            'migrations': [{'filename': 'old.sql', 'checksum': 'original'}],
            'immutable': {'anchors': [{'id': 3, 'purpose': 'experiment', 'used_amount': 150,
                                      'limit_amount': 500, 'period': 'month', 'window_start': '2026-11-01T00:00:00+08:00'},
                                     {'id': 3, 'purpose': 'openai', 'used_amount': 30}],
                          'reset_events': [{'id': 'confirmed'}]},
            'durable_ids': {'coding_quota_billing_tasks': [1]},
            'requests': [{'id': 'original', 'state': 'settled', 'points': '2'}],
            'access': {'keys': [1], 'grants': []}, 'ultrafast_enabled': False,
            'fallback': {'api_fallback_policies': [], 'api_fallback_attempts': [], 'api_fallback_requests': []},
            'experiment': empty_experiment(),
        }
        self.charge = dict(id=7, request_id='request', api_key_id=31, user_id=3, account_id=15,
                           upstream_request_id='provider-request', started_at='2026-11-17T23:00:00+08:00',
                           amount=None, state='pending', billing_effects={}, attempts=0,
                           next_retry_at='2026-11-18T00:00:00+08:00', last_error='', updated_at='before')
        self.usage = dict(id=4, request_id='request', api_key_id=31, input_cost='0', total_cost='0',
                          actual_cost='0', immutable_hash='original-token-model-time-hash')
        self.before['experiment']['experiment_quota_charges'] = [self.charge]
        self.before['experiment']['usage'] = [self.usage]
        self.after = copy.deepcopy(self.before)
        self.after['migrations'] += [{'filename': key, 'checksum': value} for key, value in self.migrations.items()]

    def settled(self, amount='2.5'):
        result = copy.deepcopy(self.after)
        charge = result['experiment']['experiment_quota_charges'][0]
        charge.update(state='settled', amount=amount, attempts=4, updated_at='after')
        event = {key: charge[key] for key in ('request_id', 'api_key_id', 'user_id', 'account_id',
                                             'upstream_request_id', 'started_at')}
        event.update(id=1, charge_id=7, previous_state='pending', previous_amount=None,
                     original_usage_costs=[copy.deepcopy(self.usage)], amount=amount,
                     evidence_source='globalai-consume-log', evidence_sha256='d' * 64,
                     created_at='2026-11-18T00:00:01+08:00')
        result['experiment']['experiment_quota_settlement_events'] = [event]
        result['immutable']['anchors'][0]['used_amount'] += float(amount)
        result['experiment']['usage'][0].update(input_cost=amount, total_cost=amount, actual_cost=amount)
        return result

    def test_unfrozen_or_changed_contract_fails_closed(self):
        self.policy.validate(dict(backend_unchanged=False, baseline_commit=upgrade.BASELINE,
                                  upgrade_contract=upgrade.contract_for_release()))
        for field, value in [('APPLICATION', 'UNFROZEN'), ('MIGRATIONS', {'unexpected.sql': 'f' * 64})]:
            with self.subTest(field=field), patch.object(upgrade, field, value), self.assertRaises(DeliveryError):
                upgrade.ExperimentAccountingUpgrade(upgrade.contract_for_release())
        changed = upgrade.contract_for_release(); changed['source_commit'] = 'b' * 40
        with self.assertRaises(DeliveryError): upgrade.ExperimentAccountingUpgrade(changed)

    def test_exact_additive_migrations_and_untouched_state_are_accepted(self):
        self.policy.assert_preserved(self.before, self.after)
        changed = copy.deepcopy(self.after); changed['migrations'][-1]['checksum'] = 'different'
        with self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, changed)

    def test_audited_original_period_settlement_explains_exact_wallet_delta(self):
        changed = self.settled()
        untouched = copy.deepcopy(changed)
        self.policy.assert_preserved(self.before, changed)
        self.assertEqual(changed, untouched, 'Preservation review must not mutate original evidence')
        changed['immutable']['anchors'][0]['used_amount'] += 0.01
        with self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, changed)

    def test_legacy_float_wallet_residue_is_compared_at_the_billing_unit(self):
        changed = self.settled()
        changed['immutable']['anchors'][0]['used_amount'] = 152.50000000000003
        self.policy.assert_preserved(self.before, changed)
        changed['immutable']['anchors'][0]['used_amount'] = 152.50000002
        with self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, changed)

    def test_settlement_cannot_leave_the_original_usage_price_at_zero(self):
        changed = self.settled()
        changed['experiment']['usage'][0].update(input_cost='0', total_cost='0', actual_cost='0')
        with self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, changed)

    def test_prior_period_settlement_does_not_add_current_wallet_credit_or_cost(self):
        for data in (self.before, self.after):
            data['experiment']['experiment_quota_charges'][0]['started_at'] = '2026-10-31T23:59:00+08:00'
        changed = self.settled()
        changed['immutable']['anchors'][0]['used_amount'] = 150
        self.policy.assert_preserved(self.before, changed)
        changed['immutable']['anchors'][0]['used_amount'] += 2.5
        with self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, changed)

    def test_evidence_cannot_move_identity_amount_time_or_other_users_wallet(self):
        for field, value in [('user_id', 9), ('request_id', 'other'), ('amount', '1'),
                             ('started_at', '2026-11-16T23:00:00+08:00'),
                             ('created_at', '2026-11-17T23:59:59+08:00'), ('evidence_sha256', 'invalid')]:
            changed = self.settled()
            changed['experiment']['experiment_quota_settlement_events'][0][field] = value
            with self.subTest(field=field), self.assertRaises(DeliveryError):
                self.policy.assert_preserved(self.before, changed)

    def test_retry_metadata_is_allowed_but_zero_cost_is_not_free_settlement(self):
        changed = copy.deepcopy(self.after)
        changed['experiment']['experiment_quota_charges'][0].update(attempts=4, last_error='429', updated_at='after')
        self.policy.assert_preserved(self.before, changed)
        changed['experiment']['experiment_quota_charges'][0].update(state='settled', amount='0')
        with self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, changed)

    def test_old_settled_event_and_original_metadata_cannot_be_rewritten(self):
        before = self.settled()
        after = copy.deepcopy(before)
        before['migrations'] = copy.deepcopy(self.before['migrations'])
        self.policy.assert_preserved(before, after)
        for table, field, value in [('experiment_quota_settlement_events', 'amount', '0'),
                                    ('experiment_quota_charges', 'amount', '0'),
                                    ('usage', 'immutable_hash', 'changed-token-count')]:
            changed = copy.deepcopy(after); changed['experiment'][table][0][field] = value
            with self.subTest(table=table), self.assertRaises(DeliveryError): self.policy.assert_preserved(before, changed)

    def test_charge_missing_audit_duplicate_event_and_cross_wallet_changes_are_rejected(self):
        for kind in ('missing-event', 'duplicate', 'other-wallet', 'disabled-standby', 'charge-identity'):
            changed = self.settled()
            if kind == 'missing-event': changed['experiment']['experiment_quota_settlement_events'] = []
            if kind == 'duplicate':
                event = copy.deepcopy(changed['experiment']['experiment_quota_settlement_events'][0]); event['id'] = 2
                changed['experiment']['experiment_quota_settlement_events'].append(event)
            if kind == 'other-wallet': changed['immutable']['anchors'][1]['used_amount'] += 1
            if kind == 'disabled-standby': changed['fallback']['api_fallback_policies'] = [{'enabled': True}]
            if kind == 'charge-identity': changed['experiment']['experiment_quota_charges'][0]['account_id'] = 99
            with self.subTest(kind=kind), self.assertRaises(DeliveryError): self.policy.assert_preserved(self.before, changed)

    def test_new_synchronous_charge_requires_the_same_immutable_audit(self):
        before = copy.deepcopy(self.before); before['experiment']['experiment_quota_charges'] = []
        before['experiment']['usage'] = []
        changed = self.settled()
        self.policy.assert_preserved(before, changed)
        changed['experiment']['experiment_quota_settlement_events'] = []
        with self.assertRaises(DeliveryError): self.policy.assert_preserved(before, changed)

    def test_any_new_attempt_or_event_blocks_old_program_rollback(self):
        class Runtime:
            def fallback_snapshot(inner): return inner.fallback
            def experiment_snapshot(inner): return inner.experiment
        runtime = Runtime(); runtime.fallback = copy.deepcopy(self.before['fallback']); runtime.experiment = empty_experiment()
        self.assertTrue(self.policy.can_rollback(runtime))
        for state in ('reserved', 'sent', 'handed_off', 'unknown', 'released', 'settled'):
            runtime.experiment = empty_experiment(); runtime.experiment['experiment_quota_attempts'] = [{'id': 1, 'state': state}]
            with self.subTest(state=state): self.assertFalse(self.policy.can_rollback(runtime))
        runtime.experiment = empty_experiment(); runtime.experiment['experiment_quota_settlement_events'] = [{'id': 1}]
        self.assertFalse(self.policy.can_rollback(runtime))

    def test_streamed_snapshot_uses_one_read_only_mvcc_transaction(self):
        runtime = object.__new__(upgrade.ExperimentAccountingRuntime)
        runtime.database = 'isolated'; runtime.db_env = {'POSTGRES_USER': 'test', 'POSTGRES_DB': 'test'}
        runtime._tables = lambda: ['experiment_quota_charges', 'experiment_quota_attempts', 'experiment_quota_settlement_events']
        scripts = []
        def stream(command, sql):
            self.assertIn('FETCH_COUNT=1000', command)
            self.assertEqual(sql.count('BEGIN '), 1)
            self.assertEqual(sql.count('COMMIT;'), 1)
            self.assertIn('ISOLATION LEVEL REPEATABLE READ READ ONLY', sql)
            self.assertIn('immutable_hash', sql)
            self.assertNotIn('UPDATE ', sql)
            scripts.append(sql)
            return iter([json.dumps(['access', {'ultrafast_enabled': False, 'keys': []}])])
        with patch('quota_snapshot.stream_rows', side_effect=stream):
            result = runtime.snapshot('2026-11-18T00:00:00+08:00')
        self.assertEqual(result['experiment'], empty_experiment())
        self.assertFalse(result['ultrafast_enabled'])
        self.assertEqual(len(scripts), 1)

    def test_source_guard_rejects_unreviewed_backend_and_old_migration_edits(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            def git(*args):
                return subprocess.check_output(['git', *args], cwd=root, text=True, stderr=subprocess.PIPE).strip()
            def commit(message):
                git('add', '-A'); git('-c', 'user.name=Delivery test', '-c', 'user.email=delivery@example.invalid', 'commit', '-qm', message)
                return git('rev-parse', 'HEAD')
            git('init', '-q')
            path = root / 'backend/migrations'; path.mkdir(parents=True)
            (path / 'old.sql').write_text('SELECT 1;')
            baseline = commit('baseline')
            for name in self.migrations: (path / name).write_text(name)
            application = commit('reviewed')
            (root / 'notes.md').write_text('documentation')
            descendant = commit('docs')
            with patch.multiple(upgrade, BASELINE=baseline, STANDBY_APPLICATION=baseline, APPLICATION=application):
                policy = upgrade.ExperimentAccountingUpgrade(upgrade.contract_for_release())
                policy.validate_sources(root, baseline, descendant)
                (root / 'backend/unreviewed.go').write_text('package changed')
                changed = commit('unreviewed backend')
                with self.assertRaises(DeliveryError): policy.validate_sources(root, baseline, changed)
            (path / 'old.sql').write_text('SELECT 2;')
            changed = commit('old SQL edited')
            with patch.multiple(upgrade, BASELINE=baseline, STANDBY_APPLICATION=baseline, APPLICATION=changed):
                with self.assertRaises(DeliveryError):
                    upgrade.ExperimentAccountingUpgrade(upgrade.contract_for_release()).validate_sources(root, baseline, changed)


@unittest.skipUnless(os.environ.get('MILE_DELIVERY_TEST_LIBPQ') and os.environ.get('SUB2API_QUOTA_TEST_DSN'),
                     'Opt in to a real isolated PostgreSQL and native libpq')
class AccountingPostgresTests(unittest.TestCase):
    def test_real_migrations_atomic_snapshot_and_audited_settlement(self):
        libpath = Path(os.environ['MILE_DELIVERY_TEST_LIBPQ'])
        with os.add_dll_directory(str(libpath.parent)):
            lib = ctypes.CDLL(str(libpath))
        for name, args, result in [
            ('PQconnectdb', [ctypes.c_char_p], ctypes.c_void_p), ('PQstatus', [ctypes.c_void_p], ctypes.c_int),
            ('PQexec', [ctypes.c_void_p, ctypes.c_char_p], ctypes.c_void_p),
            ('PQsendQuery', [ctypes.c_void_p, ctypes.c_char_p], ctypes.c_int),
            ('PQgetResult', [ctypes.c_void_p], ctypes.c_void_p), ('PQntuples', [ctypes.c_void_p], ctypes.c_int),
            ('PQresultStatus', [ctypes.c_void_p], ctypes.c_int),
            ('PQgetvalue', [ctypes.c_void_p, ctypes.c_int, ctypes.c_int], ctypes.c_char_p),
            ('PQresultErrorMessage', [ctypes.c_void_p], ctypes.c_char_p),
            ('PQclear', [ctypes.c_void_p], None), ('PQfinish', [ctypes.c_void_p], None)]:
            function = getattr(lib, name); function.argtypes = args; function.restype = result
        conn = lib.PQconnectdb(os.environ['SUB2API_QUOTA_TEST_DSN'].encode())
        self.addCleanup(lib.PQfinish, conn)
        self.assertEqual(lib.PQstatus(conn), 0, 'The explicitly selected isolated PostgreSQL must be available')
        def sql(text, read=False, fail=False):
            value = lib.PQexec(conn, text.encode())
            try:
                if fail:
                    self.assertEqual(lib.PQresultStatus(value), 7)
                else:
                    self.assertIn(lib.PQresultStatus(value), (1, 2), lib.PQresultErrorMessage(value).decode())
                    if read:
                        return lib.PQgetvalue(value, 0, 0).decode()
            finally:
                lib.PQclear(value)
        def rows(script):
            self.assertEqual(lib.PQsendQuery(conn, script.encode()), 1)
            values = []
            while value := lib.PQgetResult(conn):
                try:
                    self.assertIn(lib.PQresultStatus(value), (1, 2), lib.PQresultErrorMessage(value).decode())
                    values.extend(lib.PQgetvalue(value, index, 0).decode() for index in range(lib.PQntuples(value)))
                finally:
                    lib.PQclear(value)
            return values
        def psql_lines(_command, script):
            # psql -qAt writes embedded newlines in an unaligned column verbatim.
            for value in rows(script):
                yield from value.split('\n')
        schema = 'delivery_experiment_' + uuid.uuid4().hex
        sql('CREATE SCHEMA ' + schema + '; SET search_path TO ' + schema)
        self.addCleanup(sql, 'DROP SCHEMA ' + schema + ' CASCADE')
        sql('''CREATE TABLE users(id bigint PRIMARY KEY,created_at timestamptz,deleted_at timestamptz);
          INSERT INTO users VALUES(3,'2026-11-01',NULL);
          CREATE TABLE accounts(id bigint PRIMARY KEY); INSERT INTO accounts VALUES(15);
          CREATE TABLE user_purpose_allowances(user_id bigint,purpose text,window_start timestamptz,
            limit_amount numeric,used_amount numeric,period text);
          INSERT INTO user_purpose_allowances VALUES(3,'experiment','2026-11-01T00:00:00+08:00',500,150,'month');
          CREATE TABLE coding_quota_reset_events(id int,status text,effective_at timestamptz);
          CREATE TABLE coding_quota_corrections(id int); CREATE TABLE coding_quota_reconciliations(id int);
          CREATE TABLE scoped_admin_api_keys(id int,policy jsonb,enabled bool,revoked_at timestamptz,
            deleted_at timestamptz,expires_at timestamptz,allowed_ips jsonb,created_by bigint);
          CREATE TABLE coding_quota_reservations(id int); CREATE TABLE coding_quota_reservation_events(id int);
          CREATE TABLE coding_quota_billing_tasks(id int);
          CREATE TABLE coding_quota_async_jobs(job_id text,user_id bigint,api_key_id bigint,reservation_id text);
          CREATE TABLE coding_quota_requests(id text PRIMARY KEY,user_id bigint,purpose text,api_key_id bigint,
            account_id bigint,started_at timestamptz,completed_at timestamptz,weight numeric NOT NULL,
            points numeric NOT NULL,estimated_points numeric NOT NULL,state text,official_window_end timestamptz);
          CREATE TABLE schema_migrations(filename text,checksum text);
          INSERT INTO schema_migrations VALUES('old.sql','original');
          CREATE TABLE groups(id bigint PRIMARY KEY,platform text,usage_kind text,is_exclusive boolean,
            status text,subscription_type text,force_openai_fast boolean,force_openai_ultrafast boolean,
            model_allowlist jsonb,require_oauth_only boolean,deleted_at timestamptz);
          INSERT INTO groups VALUES(17,'composite','coding',true,'active','standard',false,false,'{}',true,NULL),
            (18,'openai','coding',true,'active','standard',false,false,'{}',true,NULL);
          CREATE TABLE api_keys(id bigint,user_id bigint,group_id bigint,deleted_at timestamptz);
          CREATE TABLE user_allowed_groups(user_id bigint,group_id bigint);
          CREATE TABLE composite_model_routes(id bigint,group_id bigint,public_model text);
          CREATE TABLE experiment_quota_charges(id bigint PRIMARY KEY,request_id text,api_key_id bigint,
            user_id bigint,account_id bigint,upstream_request_id text,started_at timestamptz,amount numeric,
            billing_effects jsonb,state text,attempts int,next_retry_at timestamptz,last_error text,updated_at timestamptz);
          INSERT INTO experiment_quota_charges VALUES(7,'request',31,3,15,'provider-request',
            '2026-11-17T23:00:00+08:00',NULL,'{}','pending',0,NOW(),'',NOW());
          CREATE TABLE usage_logs(id bigint,request_id text,api_key_id bigint,input_cost numeric,
            total_cost numeric,actual_cost numeric,input_tokens int,created_at timestamptz);
          INSERT INTO usage_logs VALUES(4,'request',31,0,0,0,100,'2026-11-17T23:00:00+08:00');
          CREATE TABLE settings(key text,value text);
          CREATE FUNCTION coding_quota_audit_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
            BEGIN RAISE EXCEPTION 'append-only'; END $$;''')
        runtime = object.__new__(upgrade.ExperimentAccountingRuntime)
        runtime.database = 'isolated'; runtime.db_env = {'POSTGRES_USER': 'test', 'POSTGRES_DB': 'test'}
        runtime._tables = lambda: json.loads(sql("SELECT json_agg(tablename) FROM pg_tables WHERE schemaname=current_schema()", read=True))
        root = Path(__file__).resolve().parents[2]
        checksums = {name: hashlib.sha256((root / 'backend/migrations' / name).read_bytes()).hexdigest()
                     for name in upgrade.MIGRATIONS}
        database = {name: hashlib.sha256((root / 'backend/migrations' / name).read_text(encoding='utf-8').strip().encode()).hexdigest()
                    for name in upgrade.MIGRATIONS}
        with patch.multiple(upgrade, APPLICATION='a' * 40, MIGRATIONS=checksums, DATABASE_MIGRATIONS=database), \
                patch('quota_snapshot.stream_rows', side_effect=psql_lines):
            policy = upgrade.ExperimentAccountingUpgrade(upgrade.contract_for_release())
            reference = sql('SELECT clock_timestamp()::text', read=True)
            before = runtime.snapshot(reference)
            self.assertEqual([group['id'] for group in before['access']['groups']], [17, 18])
            for name in checksums:
                sql((root / 'backend/migrations' / name).read_text(encoding='utf-8'))
                sql("INSERT INTO schema_migrations VALUES('" + name + "','" + database[name] + "')")
            after = runtime.snapshot(reference)
            policy.assert_preserved(before, after)
            sql((root / 'backend/migrations/272_experiment_accounting_guard.sql').read_text(encoding='utf-8'))
            self.assertEqual(after, runtime.snapshot(reference), 'Repeated additive guard migration must not alter business data')
            sql('''BEGIN;
              INSERT INTO experiment_quota_settlement_events(charge_id,request_id,api_key_id,user_id,account_id,
                upstream_request_id,started_at,previous_state,previous_amount,original_usage_costs,amount,evidence_source,evidence_sha256)
                SELECT id,request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,state,amount,
                  '[]',2.5,'tool-real-postgres','dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd'
                FROM experiment_quota_charges WHERE id=7;
              UPDATE experiment_quota_charges SET state='settled',amount=2.5 WHERE id=7;
              UPDATE user_purpose_allowances SET used_amount=used_amount+2.5 WHERE user_id=3;
              UPDATE usage_logs SET input_cost=2.5,total_cost=2.5,actual_cost=2.5 WHERE id=4;
              COMMIT;''')
            changed = runtime.snapshot(reference)
            policy.assert_preserved(before, changed)
            self.assertFalse(policy.can_rollback(type('RollbackRuntime', (), {
                'fallback_snapshot': lambda inner: changed['fallback'],
                'experiment_snapshot': lambda inner: changed['experiment']})()))
            sql('DELETE FROM experiment_quota_settlement_events', fail=True)
            sql('UPDATE usage_logs SET input_tokens=101 WHERE id=4')
            with self.assertRaises(DeliveryError): policy.assert_preserved(before, runtime.snapshot(reference))
            # Exercise the next release's real index/column migration and MVCC
            # persistence contract on exactly the same financial data.
            import experiment_concurrency_upgrade as concurrency
            name = '273_experiment_async_admission.sql'
            raw = (root / 'backend/migrations' / name).read_text(encoding='utf-8')
            file_hash = hashlib.sha256((root / 'backend/migrations' / name).read_bytes()).hexdigest()
            database_hash = hashlib.sha256(raw.strip().encode()).hexdigest()
            prior = runtime.snapshot(reference)
            sql(raw)
            sql("INSERT INTO schema_migrations VALUES('" + name + "','" + database_hash + "')")
            with patch.multiple(concurrency, APPLICATION='a' * 40, MIGRATIONS={name: file_hash},
                                DATABASE_MIGRATIONS={name: database_hash}):
                next_policy = concurrency.ExperimentConcurrencyUpgrade(concurrency.contract_for_release())
                next_policy.assert_preserved(prior, runtime.snapshot(reference))
                sql(raw)
                next_policy.assert_preserved(prior, runtime.snapshot(reference))
                sql("INSERT INTO settings VALUES('experiment_async_admission_policy','{}')")
                with self.assertRaises(DeliveryError): next_policy.assert_preserved(prior, runtime.snapshot(reference))


if __name__ == '__main__':
    unittest.main()
