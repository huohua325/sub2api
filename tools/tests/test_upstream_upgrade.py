import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError
import upstream_upgrade as upgrade


class UpstreamUpgradeTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.git('init', '-b', 'local')
        self.git('config', 'user.email', 'test@example.invalid')
        self.git('config', 'user.name', 'Test')
        self.write('backend/app.go', 'baseline')
        self.write('backend/migrations/001.sql', 'existing')
        self.git('add', '.')
        self.git('commit', '-m', 'base')
        self.base = self.git('rev-parse', 'HEAD')
        self.git('checkout', '-b', 'upstream')
        self.write('backend/app.go', 'reviewed upstream')
        self.git('commit', '-am', 'upstream')
        self.upstream = self.git('rev-parse', 'HEAD')
        self.git('checkout', 'local')
        self.git('merge', '--no-ff', self.upstream, '-m', 'merge')
        self.source = self.git('rev-parse', 'HEAD')
        self.addCleanup(patch.stopall)
        patch.object(upgrade, 'BASELINE', self.base).start()
        patch.object(upgrade, 'UPSTREAM', self.upstream).start()
        self.contract = upgrade.contract_for_release(self.source)
        self.policy = upgrade.UpstreamUpgrade(self.contract)

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, text=True,
                                       stderr=subprocess.DEVNULL).strip()

    def write(self, name, data):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(data)

    def test_clean_true_merge_and_later_tool_commit_are_allowed(self):
        self.policy.validate_sources(self.root, self.base, self.source)
        self.write('tools/note.txt', 'tool only')
        self.git('add', '.')
        self.git('commit', '-m', 'tool')
        self.policy.validate_sources(self.root, self.base, self.git('rev-parse', 'HEAD'))

    def test_extra_application_change_and_nonmerge_source_are_rejected(self):
        self.write('backend/app.go', 'unreviewed')
        self.git('commit', '-am', 'extra')
        with self.assertRaises(DeliveryError):
            self.policy.validate_sources(self.root, self.base, self.git('rev-parse', 'HEAD'))
        policy = upgrade.UpstreamUpgrade(upgrade.contract_for_release(self.upstream))
        with self.assertRaises(DeliveryError):
            policy.validate_sources(self.root, self.base, self.upstream)

    def test_migration_change_is_rejected_even_in_real_merge(self):
        self.git('checkout', 'upstream')
        self.write('backend/migrations/002.sql', 'new')
        self.git('add', '.')
        self.git('commit', '-m', 'schema')
        tip = self.git('rev-parse', 'HEAD')
        self.git('checkout', '-b', 'other', self.base)
        self.git('merge', '--no-ff', tip, '-m', 'schema merge')
        source = self.git('rev-parse', 'HEAD')
        with patch.object(upgrade, 'UPSTREAM', tip):
            policy = upgrade.UpstreamUpgrade(upgrade.contract_for_release(source))
            with self.assertRaises(DeliveryError):
                policy.validate_sources(self.root, self.base, source)

    def test_manifest_and_contract_must_match_reviewed_identity(self):
        manifest = dict(backend_unchanged=False, upgrade_contract=self.contract,
                        baseline_commit=self.base, source_commit=self.source)
        self.policy.validate(manifest)
        for key, value in [('backend_unchanged', True), ('source_commit', '0' * 40),
                           ('baseline_commit', '0' * 40)]:
            with self.subTest(key=key), self.assertRaises(DeliveryError):
                self.policy.validate(dict(manifest, **{key: value}))
        contract = dict(self.contract, upstream_commit='0' * 40)
        with self.assertRaises(DeliveryError): upgrade.UpstreamUpgrade(contract)

    def test_audits_and_schema_preserved_and_program_only_rollback_allowed(self):
        before = dict(as_of='fixed', clocks=[], migrations=['existing'], requests=[],
                      immutable={'personal_resets': [{'id': 1}]}, durable_ids={'jobs': [1]})
        self.policy.assert_preserved(before, copy.deepcopy(before))
        for key, value in [('migrations', []), ('immutable', {}), ('durable_ids', {})]:
            with self.subTest(key=key), self.assertRaises(DeliveryError):
                self.policy.assert_preserved(before, dict(before, **{key: value}))
        self.assertTrue(self.policy.can_rollback(object()))


if __name__ == '__main__': unittest.main()
