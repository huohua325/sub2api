import copy
import json
import sys
from pathlib import Path
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError
from release_server import assert_preserved, deploy
from full_upgrade import PersonalQuotaUpgrade
import test_release_server as release_tests


class FullUpgradeTests(unittest.TestCase):
    def setUp(self):
        self.contract = PersonalQuotaUpgrade({'name': 'personal-quota-reset-v1',
                         'migrations': {'260_personal_quota_resets.sql': 'a' * 64}})
        self.before = {'as_of': 'same', 'clocks': [], 'immutable': {'personal_resets': [], 'personal_reset_requests': []},
                       'durable_ids': {}, 'requests': [],
                       'migrations': [{'filename': '001.sql', 'checksum': 'old'}]}
        self.after = copy.deepcopy(self.before)
        self.after['migrations'].append({'filename': '260_personal_quota_resets.sql', 'checksum': 'a' * 64})

    def test_only_the_exact_reviewed_additive_migration_is_allowed(self):
        self.contract.assert_preserved(self.before, self.after)
        self.after['migrations'][-1]['checksum'] = 'different'
        with self.assertRaises(DeliveryError): self.contract.assert_preserved(self.before, self.after)

    def test_database_checksum_is_distinct_from_file_checksum(self):
        contract = PersonalQuotaUpgrade({'name': 'personal-quota-reset-v1',
            'migrations': {'260_personal_quota_resets.sql': 'a' * 64},
            'database_migrations': {'260_personal_quota_resets.sql': 'b' * 64}})
        self.after['migrations'][-1]['checksum'] = 'b' * 64
        contract.assert_preserved(self.before, self.after)
        self.after['migrations'][-1]['checksum'] = 'a' * 64
        with self.assertRaises(DeliveryError): contract.assert_preserved(self.before, self.after)

    def test_prior_migrations_and_audits_cannot_be_changed(self):
        self.before['immutable']['personal_resets'] = [{'id': 'retained'}]
        self.after['immutable']['personal_resets'] = []
        with self.assertRaises(DeliveryError): self.contract.assert_preserved(self.before, self.after)
        self.after['immutable']['personal_resets'] = [{'id': 'retained'}]
        self.after['migrations'][0]['checksum'] = 'changed'
        with self.assertRaises(DeliveryError): self.contract.assert_preserved(self.before, self.after)

    def test_frontend_lane_still_rejects_schema_change(self):
        with self.assertRaises(DeliveryError): assert_preserved(self.before, self.after)

    def test_old_program_rollback_is_blocked_after_personal_reset(self):
        class Runtime:
            def snapshot(inner, at): return self.after
        self.assertTrue(self.contract.can_rollback(Runtime()))
        self.after['immutable']['personal_resets'] = [{'id': 'newly-used'}]
        self.assertFalse(self.contract.can_rollback(Runtime()))

    def test_full_install_preserves_original_data_and_keeps_frontend_guard(self):
        fixture = release_tests.ReleaseServerTests('test_dry_run_does_not_stop_or_create_backup')
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        manifest_path = fixture.release / 'manifest.json'
        manifest = json.loads(manifest_path.read_text())
        manifest.update(backend_unchanged=False, upgrade_contract=self.contract.contract)
        manifest_path.write_text(json.dumps(manifest))
        contract, before, after = self.contract, self.before, self.after
        class Runtime(release_tests.FakeRuntime):
            def snapshot(inner, at):
                inner.calls.append('snapshot')
                data = copy.deepcopy(before if inner.calls.count('snapshot') == 1 else after)
                data['as_of'] = at
                return data
        runtime = Runtime()
        with self.assertRaises(DeliveryError): deploy(fixture.root, fixture.release, runtime, apply=True)
        self.assertNotIn('stop', runtime.calls)
        result = deploy(fixture.root, fixture.release, runtime, apply=True, upgrade=contract)
        self.assertEqual(result['status'], 'deployed')
        self.assertEqual(fixture.active.read_bytes(), b'new binary')

    def test_failure_never_installs_old_program_after_a_new_reset_was_used(self):
        fixture = release_tests.ReleaseServerTests('test_dry_run_does_not_stop_or_create_backup')
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        manifest_path = fixture.release / 'manifest.json'
        manifest = json.loads(manifest_path.read_text())
        manifest.update(backend_unchanged=False, upgrade_contract=self.contract.contract)
        manifest_path.write_text(json.dumps(manifest))
        before, after = self.before, self.after
        class Runtime(release_tests.FakeRuntime):
            def snapshot(inner, at):
                data = copy.deepcopy(after if 'start' in inner.calls else before)
                if 'start' in inner.calls: data['immutable']['personal_resets'] = [{'id': 'already-used'}]
                data['as_of'] = at
                return data
            def health(inner): raise DeliveryError('post-start verification failed')
        runtime = Runtime()
        with self.assertRaises(DeliveryError): deploy(fixture.root, fixture.release, runtime, apply=True, upgrade=self.contract)
        self.assertEqual(fixture.active.read_bytes(), b'new binary')
        self.assertIn('program_rollback_blocked', json.loads((fixture.release / 'deployment.json').read_text()))


if __name__ == '__main__': unittest.main()
