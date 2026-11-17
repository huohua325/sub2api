"""Private evidence staging integrity, interruption/resume and path confinement."""
import base64
from copy import deepcopy
import importlib.util
import json
import os
from pathlib import Path
import random
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location('globalai_stage_under_test', Path(__file__).parents[1] / 'globalai_stage.py')
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def payload(size=64):
    return {'schema': 1, 'evidence_base64': base64.b64encode(random.Random(7).randbytes(size)).decode(), 'plan': {}}


class ArtifactTests(unittest.TestCase):
    def test_payload_is_independent_of_execution_controls(self):
        manifest, chunks = MODULE.artifact(payload(600000))
        self.assertEqual(len(chunks), 3)
        self.assertEqual(manifest['chunk_bytes'], 256 << 10)
        self.assertEqual(sum(x['bytes'] for x in manifest['chunks']), manifest['blob_bytes'])
        self.assertEqual(MODULE.digest(b''.join(chunks)), manifest['blob_sha256'])
        for key in ('backup', 'source_commit', 'apply'):
            with self.subTest(key=key), self.assertRaises(MODULE.StageError):
                MODULE.artifact({**payload(), key: True})

    def test_default_wire_chunk_stays_below_known_failed_ssh_size(self):
        manifest, chunks = MODULE.artifact(payload(600000))
        source = MODULE.remote_script('chunk', manifest, chunks[0], 0)
        self.assertLess(len(source.encode()), 672 << 10)
        self.assertGreater(len(source.encode()), len(chunks[0]))

    def test_staged_reference_binds_exact_payload_and_manifest(self):
        value = payload()
        manifest, _ = MODULE.artifact(value)
        report = {'status': 'ready', 'manifest': manifest, 'manifest_sha256': MODULE.digest(MODULE.compact(manifest))}
        reference = MODULE.stage_reference(report, value)
        self.assertEqual(reference['payload_sha256'], manifest['payload_sha256'])
        for changed in ({**value, 'plan': {'changed': True}}, {**value, 'evidence_base64': 'different'}):
            with self.assertRaises(MODULE.StageError):
                MODULE.stage_reference(report, changed)
        report['manifest']['payload_bytes'] += 1
        with self.assertRaises(MODULE.StageError):
            MODULE.stage_reference(report, value)

    def test_incomplete_report_cannot_execute(self):
        with self.assertRaises(MODULE.StageError):
            MODULE.stage_reference({'status': 'partial'}, payload())

    def test_rpc_timeout_does_not_turn_into_success(self):
        manifest, _ = MODULE.artifact(payload())
        with patch.object(MODULE.subprocess, 'run', side_effect=subprocess.TimeoutExpired('ssh', 45)):
            with self.assertRaisesRegex(MODULE.StageError, 'retained'):
                MODULE.ssh_rpc('host', 'prepare', manifest)


class RemoteFilesystemTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'reconciliation'

    def rpc(self, _host, action, manifest, chunk=None, index=None):
        code = MODULE.remote_script(action, manifest, chunk, index)
        code = code.replace("STAGING_ROOT=pathlib.Path('/opt/sub2api/reconciliation')",
                            'STAGING_ROOT=pathlib.Path(' + repr(str(self.root)) + ')')
        result = subprocess.run([sys.executable, '-'], input=code, text=True, encoding='utf-8',
                                capture_output=True, timeout=15)
        response = json.loads(result.stdout)
        if result.returncode or not response.get('ok'):
            raise MODULE.StageError(response.get('error', 'remote rejection'))
        return response['result']

    def test_real_chunks_and_final_payload_are_verified_and_private(self):
        value = payload(600000)
        report = MODULE.stage_payload('local', value, rpc=self.rpc)
        self.assertEqual(report['status'], 'ready')
        self.assertEqual(len(report['uploaded_chunks']), 3)
        folder = self.root / report['manifest']['blob_sha256']
        self.assertEqual(MODULE.digest((folder/'payload.zlib').read_bytes()), report['manifest']['blob_sha256'])
        if os.name != 'nt':
            self.assertEqual(folder.stat().st_mode & 0o777, 0o700)
            self.assertEqual((folder/'payload.zlib').stat().st_mode & 0o777, 0o600)
        # An unchanged second invocation reuses the complete verified artifact.
        again = MODULE.stage_payload('local', value, rpc=self.rpc)
        self.assertEqual(again['status'], 'ready')
        self.assertEqual(again['uploaded_chunks'], [])
        self.assertEqual(again['reused_chunks'], 3)

    def test_resume_preserves_first_verified_chunk_after_transfer_failure(self):
        value = payload(600000)
        progress = []
        def fail_second(host, action, manifest, chunk=None, index=None):
            if action == 'chunk' and index == 1:
                raise MODULE.StageError('simulated transfer interruption')
            return self.rpc(host, action, manifest, chunk, index)
        with self.assertRaises(MODULE.StageError):
            MODULE.stage_payload('local', value, rpc=fail_second, progress=lambda x: progress.append(deepcopy(x)))
        self.assertEqual(progress[-1]['status'], 'partial')
        self.assertEqual(len(progress[-1]['uploaded_chunks']), 1)
        result = MODULE.stage_payload('local', value, rpc=self.rpc)
        self.assertEqual(result['reused_chunks'], 1)
        self.assertEqual([x['index'] for x in result['uploaded_chunks']], [1, 2])

    def test_probe_uploads_only_one_chunk_then_real_resume_finishes(self):
        value = payload(600000)
        first = MODULE.stage_payload('local', value, rpc=self.rpc, max_new_chunks=1)
        self.assertEqual(first['status'], 'partial')
        self.assertEqual(len(first['uploaded_chunks']), 1)
        final = MODULE.stage_payload('local', value, rpc=self.rpc)
        self.assertEqual(final['status'], 'ready')
        self.assertEqual(final['reused_chunks'], 1)

    def test_corrupt_chunk_is_replaced_without_touching_valid_ones(self):
        value = payload(600000)
        first = MODULE.stage_payload('local', value, rpc=self.rpc, max_new_chunks=1)
        folder = self.root / first['manifest']['blob_sha256']
        (folder/'chunk-000000.bin').write_bytes(b'corrupted')
        final = MODULE.stage_payload('local', value, rpc=self.rpc)
        self.assertEqual(final['status'], 'ready')
        self.assertEqual(final['reused_chunks'], 0)

    def test_bad_chunk_digest_and_assembly_before_complete_are_rejected(self):
        manifest, chunks = MODULE.artifact(payload(600000))
        self.rpc('local', 'prepare', manifest)
        with self.assertRaisesRegex(MODULE.StageError, 'digest'):
            self.rpc('local', 'chunk', manifest, chunks[0][:-1], 0)
        with self.assertRaisesRegex(MODULE.StageError, 'incomplete'):
            self.rpc('local', 'finish', manifest)

    def test_path_traversal_and_conflicting_manifest_are_rejected(self):
        manifest, _ = MODULE.artifact(payload())
        malicious = deepcopy(manifest); malicious['blob_sha256'] = '../outside'
        with self.assertRaisesRegex(MODULE.StageError, 'digest'):
            self.rpc('local', 'prepare', malicious)
        self.rpc('local', 'prepare', manifest)
        changed = deepcopy(manifest); changed['payload_sha256'] = 'a'*64
        with self.assertRaisesRegex(MODULE.StageError, 'conflicts'):
            self.rpc('local', 'prepare', changed)

    def test_symbolic_staging_leaf_is_rejected(self):
        manifest, _ = MODULE.artifact(payload())
        self.root.mkdir()
        external = Path(self.temp.name) / 'external'; external.mkdir()
        try:
            (self.root / manifest['blob_sha256']).symlink_to(external, target_is_directory=True)
        except OSError:
            self.skipTest('Local Windows account cannot create symlinks; server paths are checked in real stage')
        with self.assertRaisesRegex(MODULE.StageError, 'link'):
            self.rpc('local', 'prepare', manifest)


if __name__ == '__main__':
    unittest.main()
