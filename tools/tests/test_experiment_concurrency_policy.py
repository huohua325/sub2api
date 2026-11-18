"""Rollout is explicit, scoped and idempotent; it never rewrites an existing policy."""
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import experiment_concurrency_policy as rollout
from delivery_core import DeliveryError


class ConcurrencyPolicyTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name); self.release = self.root / 'releases/test'
        self.release.mkdir(parents=True)
        self.users = [dict(id=2, email='admin@example.invalid', role='admin', status='active'),
                      dict(id=3, email='tester@example.invalid', role='user', status='active')]
        for target, value in [('digest', 'a' * 64), ('ExperimentAccountingRuntime', None)]:
            mock = patch.object(rollout, target, return_value=value)
            mock.start(); self.addCleanup(mock.stop)
        self.command = patch.object(rollout.subprocess, 'check_output')
        self.query = self.command.start(); self.addCleanup(self.command.stop)

    def configure(self, **kwargs):
        return rollout.configure(self.root, self.release, 'a' * 64, 2, 'tester@example.invalid', **kwargs)

    def responses(self, prior=None, insert=False):
        self.query.side_effect = [json.dumps(self.users), 't', json.dumps(prior)] + (
            ['experiment_async_admission_policy'] if insert else [])
        # A runtime supplies the fixed psql argv; no Docker process runs here.
        rollout.ExperimentAccountingRuntime.return_value = type('Runtime', (), {'_command': lambda self: ['synthetic-psql']})()

    def test_default_is_read_only_and_exact_two_user_scope(self):
        self.responses()
        result = self.configure()
        self.assertFalse(result['applied'])
        self.assertEqual(result['policy']['user_ids'], [2, 3])
        self.assertFalse(list(self.release.iterdir()))
        self.assertTrue(all('INSERT' not in call.kwargs['input'] for call in self.query.call_args_list))

    def test_apply_retains_prior_and_records_only_selected_scope(self):
        self.responses(insert=True)
        result = self.configure(apply=True)
        self.assertTrue(result['applied'])
        audit = json.loads((self.release / 'experiment-concurrency-rollout.json').read_text())
        self.assertEqual(audit['policy']['max_active'], 10)
        self.assertIsNone(audit['prior'])
        self.query.reset_mock()
        self.responses(prior={'value': json.dumps(result['policy']), 'updated_at': 'retained'})
        self.assertTrue(self.configure(apply=True)['applied'])
        self.assertEqual(self.query.call_count, 3, 'identical rollout is a no-write operation')

    def test_existing_other_scope_or_inactive_user_cannot_be_overwritten(self):
        self.responses(prior={'value': '{"user_ids":[99]}'})
        with self.assertRaises(DeliveryError): self.configure(apply=True)
        self.users[1]['status'] = 'disabled'; self.responses()
        with self.assertRaises(DeliveryError): self.configure(apply=True)
        self.assertFalse(list(self.release.iterdir()))


if __name__ == '__main__': unittest.main()
