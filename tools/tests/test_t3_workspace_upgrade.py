"""The T3 release only packages the reviewed application against the live baseline."""
import copy
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError
import t3_workspace_upgrade as upgrade


class T3WorkspaceUpgradeTests(unittest.TestCase):
    def test_validate_binds_source_baseline_and_empty_migrations(self):
        policy = upgrade.T3WorkspaceUpgrade(upgrade.contract_for_release('a' * 40))
        manifest = dict(backend_unchanged=False, baseline_commit=upgrade.BASELINE,
                        baseline_sha256=upgrade.BASELINE_SHA256, source_commit='a' * 40,
                        upgrade_contract=policy.contract)
        policy.validate(manifest)
        for field, value in (('backend_unchanged', True), ('baseline_commit', 'b' * 40),
                             ('baseline_sha256', 'b' * 64), ('source_commit', 'b' * 40)):
            with self.subTest(field=field), self.assertRaises(DeliveryError):
                policy.validate({**manifest, field: value})
        changed = copy.deepcopy(policy.contract)
        changed['database_migrations'] = {'new.sql': 'b' * 64}
        with self.assertRaisesRegex(DeliveryError, 'migration-free'):
            upgrade.T3WorkspaceUpgrade(changed)

    def test_only_reviewed_app_scope_and_unchanged_migrations_are_accepted(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            def git(*args):
                return subprocess.check_output(['git', *args], cwd=root, text=True,
                                               stderr=subprocess.PIPE).strip()

            def write(name, contents):
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(contents, encoding='utf-8')

            def commit(message):
                git('add', '-A')
                git('-c', 'user.name=Delivery test', '-c', 'user.email=delivery@example.invalid',
                    'commit', '-qm', message)
                return git('rev-parse', 'HEAD')

            git('init', '-q')
            write('backend/server.go', 'before')
            write('backend/migrations/old.sql', 'SELECT 1;')
            write('backend/billing.go', 'before')
            baseline = commit('live')
            write('backend/billing.go', 'unrelated billing update')
            commit('separate billing')
            git('checkout', '--detach', baseline)
            write('backend/server.go', 'reviewed')
            write('frontend/native.ts', 'workspace')
            reviewed = commit('reviewed workspace')
            write('tools/test.py', 'release tool only')
            descendant = commit('tools')
            write('backend/server.go', 'changed after review')
            changed = commit('post-review source')
            write('backend/migrations/old.sql', 'SELECT 2;')
            migration = commit('migration change')
            git('checkout', '--detach', reviewed)
            write('backend/billing.go', 'unrelated billing update')
            unrelated = commit('review plus unrelated code')
            with patch.multiple(upgrade, BASELINE=baseline, ADDED={'frontend/native.ts'},
                                MODIFIED={'backend/server.go'}):
                policy = upgrade.T3WorkspaceUpgrade(upgrade.contract_for_release(reviewed))
                policy.validate_sources(root, baseline, descendant)
                with self.assertRaisesRegex(DeliveryError, 'after the reviewed'):
                    policy.validate_sources(root, baseline, changed)
                with self.assertRaisesRegex(DeliveryError, 'preserve every deployed migration'):
                    policy.validate_sources(root, baseline, migration)
                policy = upgrade.T3WorkspaceUpgrade(upgrade.contract_for_release(unrelated))
                with self.assertRaisesRegex(DeliveryError, 'backend/billing.go'):
                    policy.validate_sources(root, baseline, unrelated)


if __name__ == '__main__':
    unittest.main()
