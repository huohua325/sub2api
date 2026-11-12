import copy
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import digest, DeliveryError, write_json
try:
    from release_server import deploy, assert_preserved, stage_release
except ImportError:
    deploy = assert_preserved = stage_release = None


class FakeRuntime:
    """Only Docker/HTTP boundaries are faked; real filesystem install and rollback."""
    def __init__(self):
        self.calls = []; self.fail_backup = False; self.fail_after = False; self.after_backup = lambda: None
        self.views = {'as_of': 'fixed', 'immutable': {'events': [{'id': 1, 'value': 'original'}]},
                      'durable_ids': {'billing_tasks': [1]}, 'requests': [{'id': 1, 'state': 'settled', 'points': '10'}],
                      'clocks': [{'user_id': 1, 'purpose': 'openai', 'start': 'old', 'end': 'next'}],
                      'migrations': ['migration-a']}
    def preflight(self): self.calls.append('preflight'); return {'environment_hash': 'unchanged', 'modes': ['shadow', 'shadow']}
    def backup(self, folder):
        self.calls.append('backup')
        if self.fail_backup: raise DeliveryError('backup failed')
        (folder / 'database.dump').write_bytes(b'new verified database backup')
        self.after_backup()
    def stop(self): self.calls.append('stop')
    def start(self): self.calls.append('start')
    def health(self): self.calls.append('health')
    def snapshot(self, as_of):
        self.calls.append('snapshot'); data = copy.deepcopy(self.views); data['as_of'] = as_of
        if self.fail_after and self.calls.count('snapshot') > 1: data['requests'][0]['points'] = '0'
        return data


class ReleaseServerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name); (self.root / 'bin').mkdir()
        self.active = self.root / 'bin/sub2api'; self.active.write_bytes(b'old binary')
        self.release = self.root / 'releases/new'; self.release.mkdir(parents=True)
        (self.release / 'sub2api').write_bytes(b'new binary')
        write_json(self.release / 'manifest.json', {'schema': 1, 'version': 'test-release', 'source_commit': 'a'*40,
                    'sha256': digest(self.release / 'sub2api'), 'baseline_sha256': digest(self.active),
                    'backend_unchanged': True, 'quota_tests': {'passed': 66, 'skipped': 0, 'failed': 0}})
        self.runtime = FakeRuntime()
    def apply(self, **kwargs):
        self.assertIsNotNone(deploy, 'Safe reusable deployer is not implemented')
        return deploy(self.root, self.release, self.runtime, **kwargs)
    def test_dry_run_does_not_stop_or_create_backup(self):
        result = self.apply()
        self.assertEqual(result['status'], 'planned')
        self.assertEqual(self.runtime.calls, ['preflight'])
        self.assertFalse((self.root / 'backups').exists())
    def test_install_is_atomic_and_keeps_existing_backup_and_database(self):
        previous = self.root / 'backups/history'; previous.mkdir(parents=True)
        (previous / 'database.dump').write_bytes(b'historical')
        result = self.apply(apply=True)
        self.assertEqual(self.active.read_bytes(), b'new binary')
        self.assertEqual((Path(result['backup']) / 'sub2api.previous').read_bytes(), b'old binary')
        self.assertEqual((previous / 'database.dump').read_bytes(), b'historical')
        self.assertLess(self.runtime.calls.index('backup'), self.runtime.calls.index('stop'))
        self.assertEqual(result['status'], 'deployed')
    def test_changed_baseline_rejected_before_backup_or_stop(self):
        self.active.write_bytes(b'someone else deployed')
        with self.assertRaises(DeliveryError): self.apply(apply=True)
        self.assertNotIn('stop', self.runtime.calls)
    def test_corrupt_candidate_rejected_before_stop(self):
        (self.release / 'sub2api').write_bytes(b'corrupt')
        with self.assertRaises(DeliveryError): self.apply(apply=True)
        self.assertNotIn('stop', self.runtime.calls)
    def test_failed_backup_never_stops_app(self):
        self.runtime.fail_backup = True
        with self.assertRaises(DeliveryError): self.apply(apply=True)
        self.assertNotIn('stop', self.runtime.calls)
        self.assertEqual(self.active.read_bytes(), b'old binary')
    def test_candidate_changed_during_backup_rejected_before_stop(self):
        self.runtime.after_backup = lambda: (self.release / 'sub2api').write_bytes(b'replaced upload')
        with self.assertRaises(DeliveryError): self.apply(apply=True)
        self.assertNotIn('stop', self.runtime.calls)
    def test_guard_failure_after_health_rolls_back_program_only(self):
        self.runtime.fail_after = True
        with self.assertRaises(DeliveryError): self.apply(apply=True)
        self.assertEqual(self.active.read_bytes(), b'old binary')
        self.assertEqual(self.runtime.calls.count('start'), 2)
        self.assertTrue(next((self.root / 'backups').glob('*/database.dump')).exists())
        self.assertNotIn('restore', self.runtime.calls)
    def test_path_escape_and_missing_quota_proof_fail_closed(self):
        with self.assertRaises(DeliveryError): self._escape()
        manifest = __import__('json').loads((self.release / 'manifest.json').read_text())
        manifest['quota_tests']['skipped'] = 1; write_json(self.release / 'manifest.json', manifest)
        with self.assertRaises(DeliveryError): self.apply(apply=True)
        self.assertNotIn('stop', self.runtime.calls)
    def _escape(self):
        self.assertIsNotNone(deploy, 'Safe reusable deployer is not implemented')
        return deploy(self.root, self.root.parent, self.runtime, apply=True)
    def test_task_settlement_allowed_but_original_charges_cannot_change(self):
        self.assertIsNotNone(assert_preserved, 'Preservation guard is not implemented')
        before = copy.deepcopy(self.runtime.views); after = copy.deepcopy(before)
        after['durable_ids']['billing_tasks'].append(2)
        after['requests'].append({'id': 2, 'state': 'pending', 'points': '0'})
        assert_preserved(before, after)
        after['requests'][0]['points'] = '0'
        with self.assertRaises(DeliveryError): assert_preserved(before, after)

    def test_staging_verifies_payload_without_changing_active_program(self):
        self.assertIsNotNone(stage_release, 'Verified server staging is not implemented')
        import gzip, json
        (self.release / 'sub2api').unlink()
        with gzip.open(self.release / 'sub2api.gz', 'wb') as stream: stream.write(b'new binary')
        manifest = json.loads((self.release / 'manifest.json').read_text())
        manifest.update(payload='sub2api.gz', payload_sha256=digest(self.release / 'sub2api.gz'), bytes=10)
        write_json(self.release / 'manifest.json', manifest)
        stage_release(self.root, self.release)
        self.assertEqual((self.release / 'sub2api').read_bytes(), b'new binary')
        self.assertEqual(self.active.read_bytes(), b'old binary')


if __name__ == '__main__': unittest.main()
