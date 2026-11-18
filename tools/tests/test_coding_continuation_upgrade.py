"""Keep the long-conversation hotfix limited to its reviewed sources and existing ledger contract."""
import copy
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import coding_continuation_upgrade as upgrade
from delivery_core import DeliveryError
import test_experiment_accounting_upgrade as accounting_tests


class ContinuationUpgradeTests(unittest.TestCase):
    def test_no_migrations_and_audited_settlements_preserve_existing_state(self):
        fixture = accounting_tests.AccountingUpgradeTests()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        policy = upgrade.CodingContinuationUpgrade(upgrade.contract_for_release())
        policy.assert_preserved(fixture.before, copy.deepcopy(fixture.before))
        after = fixture.settled()
        after['migrations'] = copy.deepcopy(fixture.before['migrations'])
        policy.assert_preserved(fixture.before, after)
        for field in ('migrations', 'access', 'fallback'):
            changed = copy.deepcopy(after)
            if field == 'migrations': changed[field].append({'filename': 'new.sql', 'checksum': 'new'})
            elif field == 'access': changed[field]['keys'].append(99)
            else: changed[field]['api_fallback_policies'].append({'enabled': True})
            with self.subTest(field=field), self.assertRaises(DeliveryError):
                policy.assert_preserved(fixture.before, changed)
        after['immutable']['anchors'][0]['used_amount'] += 1
        with self.assertRaises(DeliveryError): policy.assert_preserved(fixture.before, after)

    def test_manifest_and_contract_are_pinned(self):
        policy = upgrade.CodingContinuationUpgrade(upgrade.contract_for_release())
        manifest = dict(backend_unchanged=False, baseline_commit=upgrade.BASELINE,
                        upgrade_contract=upgrade.contract_for_release())
        policy.validate(manifest)
        for field, value in [('baseline_commit', '0' * 40), ('backend_unchanged', True)]:
            with self.subTest(field=field), self.assertRaises(DeliveryError):
                policy.validate({**manifest, field: value})
        changed = upgrade.contract_for_release(); changed['migrations'] = {'new.sql': '0' * 64}
        with self.assertRaises(DeliveryError): upgrade.CodingContinuationUpgrade(changed)

    def test_only_two_reviewed_file_modifications_and_document_descendants_are_accepted(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            def git(*args):
                return subprocess.check_output(['git', *args], cwd=root, text=True, stderr=subprocess.PIPE).strip()
            def commit():
                git('add', '-A')
                git('-c', 'user.name=Delivery test', '-c', 'user.email=delivery@example.invalid', 'commit', '-qm', 'test')
                return git('rev-parse', 'HEAD')
            git('init', '-q')
            for name in upgrade.FILES:
                path = root / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_text('before')
            baseline = commit()
            for name in upgrade.FILES: (root / name).write_text('reviewed')
            application = commit()
            (root / 'notes.md').write_text('documentation'); descendant = commit()
            with patch.multiple(upgrade, BASELINE=baseline, APPLICATION=application):
                policy = upgrade.CodingContinuationUpgrade(upgrade.contract_for_release())
                policy.validate_sources(root, baseline, descendant)
                with self.assertRaises(DeliveryError): policy.validate_sources(root, '0' * 40, descendant)
                (root / 'backend/new.sql').write_text('unreviewed'); changed = commit()
                with self.assertRaises(DeliveryError): policy.validate_sources(root, baseline, changed)
            with patch.multiple(upgrade, BASELINE=baseline, APPLICATION=changed):
                with self.assertRaises(DeliveryError):
                    upgrade.CodingContinuationUpgrade(upgrade.contract_for_release()).validate_sources(root, baseline, changed)


if __name__ == '__main__': unittest.main()
