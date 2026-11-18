"""Protect the deployed continuation fix, settlement history and ordinary routing."""
import copy
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import coding_standby_upgrade as upgrade
from delivery_core import DeliveryError
import test_experiment_accounting_upgrade as accounting_tests


class CodingStandbyUpgradeTests(unittest.TestCase):
    def setUp(self):
        patches = patch.object(upgrade, 'APPLICATION', 'a' * 40)
        patches.start()
        self.addCleanup(patches.stop)
        self.policy = upgrade.CodingStandbyUpgrade(upgrade.contract_for_release())

    def test_zero_migrations_preserve_settlements_and_access(self):
        fixture = accounting_tests.AccountingUpgradeTests()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        self.policy.assert_preserved(fixture.before, copy.deepcopy(fixture.before))
        after = fixture.settled()
        after['migrations'] = copy.deepcopy(fixture.before['migrations'])
        self.policy.assert_preserved(fixture.before, after)
        for field in ('migrations', 'access', 'fallback'):
            changed = copy.deepcopy(after)
            if field == 'migrations': changed[field].append({'filename': 'new.sql', 'checksum': 'new'})
            elif field == 'access': changed[field]['keys'].append(99)
            else: changed[field]['api_fallback_policies'].append({'enabled': True})
            with self.subTest(field=field), self.assertRaises(DeliveryError):
                self.policy.assert_preserved(fixture.before, changed)

    def test_contract_is_pinned_and_requires_reviewed_source(self):
        valid = dict(backend_unchanged=False, baseline_commit=upgrade.BASELINE,
                     upgrade_contract=upgrade.contract_for_release())
        self.policy.validate(valid)
        for field, value in [('baseline_commit', '0' * 40), ('backend_unchanged', True)]:
            with self.subTest(field=field), self.assertRaises(DeliveryError):
                self.policy.validate({**valid, field: value})
        changed = upgrade.contract_for_release()
        changed['migrations'] = {'unexpected.sql': '0' * 64}
        with self.assertRaises(DeliveryError): upgrade.CodingStandbyUpgrade(changed)
        with patch.object(upgrade, 'APPLICATION', 'UNFROZEN'), self.assertRaises(DeliveryError):
            upgrade.CodingStandbyUpgrade(upgrade.contract_for_release())

    def test_only_compatible_baseline_without_roles_can_be_restored(self):
        class Runtime:
            def fallback_snapshot(inner): return inner.data
            def experiment_snapshot(inner): raise AssertionError('the pinned baseline already supports experiment evidence')
        runtime = Runtime()
        empty = {name: [] for name in ('api_fallback_policies', 'api_fallback_policy_versions',
                                      'api_fallback_attempts', 'api_fallback_requests')}
        runtime.data = copy.deepcopy(empty)
        self.assertTrue(self.policy.can_rollback(runtime))
        for table, row in [('api_fallback_policies', {'config': {'enabled': False, 'backup_account_ids': [30]}}),
                           ('api_fallback_policy_versions', {'config': {'backup_account_ids': [30]}}),
                           ('api_fallback_attempts', {'state': 'cancelled'}),
                           ('api_fallback_requests', {'points': '0.1'})]:
            runtime.data = copy.deepcopy(empty)
            runtime.data[table].append(row)
            with self.subTest(table=table): self.assertFalse(self.policy.can_rollback(runtime))

    def test_ancestry_requires_hotfix_and_all_migrations_are_unchanged(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            def git(*args):
                return subprocess.check_output(['git', *args], cwd=root, text=True, stderr=subprocess.PIPE).strip()
            def commit():
                git('add', '-A')
                git('-c', 'user.name=Delivery test', '-c', 'user.email=delivery@example.invalid', 'commit', '-qm', 'test')
                return git('rev-parse', 'HEAD')
            git('init', '-q')
            source = root / 'backend/relay.go'
            source.parent.mkdir()
            source.write_text('hotfix')
            hotfix = commit()
            source.write_text('deployed')
            baseline = commit()
            source.write_text('reviewed extension')
            application = commit()
            (root / 'notes.md').write_text('documentation')
            descendant = commit()
            with patch.multiple(upgrade, HOTFIX=hotfix, BASELINE=baseline, APPLICATION=application):
                policy = upgrade.CodingStandbyUpgrade(upgrade.contract_for_release())
                policy.validate_sources(root, baseline, descendant)
                with self.assertRaises(DeliveryError): policy.validate_sources(root, '0' * 40, descendant)
                with patch.object(upgrade, 'HOTFIX', 'f' * 40), self.assertRaises(DeliveryError):
                    policy.validate_sources(root, baseline, descendant)
                source.write_text('unreviewed')
                changed = commit()
                with self.assertRaises(DeliveryError): policy.validate_sources(root, baseline, changed)
            migration = root / 'backend/migrations/new.sql'
            migration.parent.mkdir()
            migration.write_text('SELECT 1')
            changed = commit()
            with patch.multiple(upgrade, HOTFIX=hotfix, BASELINE=baseline, APPLICATION=changed):
                with self.assertRaises(DeliveryError):
                    upgrade.CodingStandbyUpgrade(upgrade.contract_for_release()).validate_sources(root, baseline, changed)


if __name__ == '__main__':
    unittest.main()
