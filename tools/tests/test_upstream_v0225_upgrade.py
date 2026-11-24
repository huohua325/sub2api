import copy
import sys
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError
import upstream_v0225_upgrade as upgrade


class UpstreamV0225UpgradeTests(unittest.TestCase):
    def setUp(self):
        with patch.object(upgrade, 'APPLICATION', 'a' * 40):
            self.policy = upgrade.UpstreamV0225Upgrade(upgrade.contract_for_release())

    def test_manifest_is_pinned(self):
        manifest = dict(backend_unchanged=False, baseline_commit=upgrade.BASELINE, upgrade_contract=self.policy.contract)
        self.policy.validate(manifest)
        for change in (dict(backend_unchanged=True), dict(baseline_commit='b' * 40),
                       dict(upgrade_contract=dict(self.policy.contract, migrations={}))):
            with self.assertRaises(DeliveryError):
                self.policy.validate(dict(manifest, **change))

    def test_migration_and_channel_overrides_are_checked(self):
        before = dict(migrations=[dict(filename='previous.sql', checksum='old')],
                      access={}, ultrafast_enabled=False, fallback={'api_fallback_policies': []},
                      account_standby=[], channel_ultrafast=[])
        after = copy.deepcopy(before)
        after['migrations'].append(dict(filename=upgrade.MIGRATION, checksum=upgrade.DB_SHA256))
        with patch('experiment_accounting_upgrade.explain_experiment_settlements', return_value=copy.deepcopy(after)), \
             patch('experiment_accounting_upgrade.assert_preserved') as preserved:
            self.policy.assert_preserved(before, after)
            self.assertEqual(before['migrations'], preserved.call_args.args[1]['migrations'])
        with self.assertRaisesRegex(DeliveryError, 'migration'):
            self.policy.assert_preserved(before, dict(after, migrations=before['migrations']))
        with patch.object(upgrade.ExperimentAccountStandbyUpgrade, 'assert_preserved'):
            with self.assertRaisesRegex(DeliveryError, 'Ultra Fast'):
                self.policy.assert_preserved(before, dict(after, channel_ultrafast=[[1, 2]]))

    def test_program_rollback_disallowed_after_using_new_pricing(self):
        runtime = Mock()
        runtime.channel_ultrafast_snapshot.return_value = []
        self.assertTrue(self.policy.can_rollback(runtime))
        runtime.channel_ultrafast_snapshot.return_value = [[1, 2]]
        self.assertFalse(self.policy.can_rollback(runtime))
