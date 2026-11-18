"""Keep async admission rollout and migration scope pinned to reviewed evidence."""
import copy
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import experiment_concurrency_upgrade as upgrade
from delivery_core import DeliveryError, digest
import test_experiment_accounting_upgrade as accounting_tests


class ConcurrencyUpgradeTests(unittest.TestCase):
    def setUp(self):
        self.hashes = {'273_experiment_async_admission.sql': 'b' * 64}
        constants = patch.multiple(upgrade, APPLICATION='a' * 40, MIGRATIONS=self.hashes,
                                   DATABASE_MIGRATIONS=self.hashes)
        constants.start(); self.addCleanup(constants.stop)
        self.policy = upgrade.ExperimentConcurrencyUpgrade(upgrade.contract_for_release())

    def test_migration_preserves_ledger_and_cannot_expand_rollout(self):
        fixture = accounting_tests.AccountingUpgradeTests(); fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        before = fixture.before
        after = copy.deepcopy(before)
        after['migrations'] += [{'filename': name, 'checksum': value} for name, value in self.hashes.items()]
        self.policy.assert_preserved(before, after)
        for key in ('purpose_allowance_policy', 'experiment_async_admission_policy'):
            changed = copy.deepcopy(after)
            changed['experiment']['policy'].append({'key': key, 'value': 'unreviewed'})
            with self.subTest(key=key), self.assertRaises(DeliveryError):
                self.policy.assert_preserved(before, changed)
        self.assertTrue(self.policy.can_rollback(None), 'the pinned accounting-compatible baseline may restore strict admission without discarding liabilities')

    def test_manifest_is_explicit_and_contract_is_pinned(self):
        manifest = dict(backend_unchanged=False, baseline_commit=upgrade.BASELINE,
                        upgrade_contract=upgrade.contract_for_release())
        self.policy.validate(manifest)
        with self.assertRaises(DeliveryError): self.policy.validate({**manifest, 'backend_unchanged': True})
        with self.assertRaises(DeliveryError): upgrade.ExperimentConcurrencyUpgrade({})

    def test_source_identity_and_only_one_new_migration(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            def git(*args):
                return subprocess.check_output(['git', *args], cwd=root, text=True, stderr=subprocess.PIPE).strip()
            def commit():
                git('add', '-A')
                git('-c', 'user.name=Delivery test', '-c', 'user.email=delivery@example.invalid', 'commit', '-qm', 'test')
                return git('rev-parse', 'HEAD')
            git('init', '-q')
            migrations = root / 'backend/migrations'; migrations.mkdir(parents=True)
            (migrations / 'old.sql').write_text('SELECT 1;')
            baseline = commit()
            target = migrations / '273_experiment_async_admission.sql'; target.write_text('SELECT 2;')
            application = commit()
            hashes = {target.name: digest(target)}
            with patch.multiple(upgrade, BASELINE=baseline, APPLICATION=application,
                                MIGRATIONS=hashes, DATABASE_MIGRATIONS=hashes):
                policy = upgrade.ExperimentConcurrencyUpgrade(upgrade.contract_for_release())
                (root / 'notes.md').write_text('record'); descendant = commit()
                policy.validate_sources(root, baseline, descendant)
                (migrations / 'old.sql').write_text('SELECT 3;'); changed = commit()
                with self.assertRaises(DeliveryError): policy.validate_sources(root, baseline, changed)
                with patch.object(upgrade, 'APPLICATION', changed):
                    with self.assertRaises(DeliveryError):
                        upgrade.ExperimentConcurrencyUpgrade(upgrade.contract_for_release()).validate_sources(root, baseline, changed)


if __name__ == '__main__': unittest.main()
