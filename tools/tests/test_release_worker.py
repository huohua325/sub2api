"""Unit tests for the controlled release worker (privilege separation and integrity checks)."""
import gzip
import hashlib
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from delivery_core import DeliveryError
import release_worker


def make_candidate_zip(version, commit, run_id, binary_bytes, quota=None, extra_member=None):
    buf = io.BytesIO()
    gz_buf = io.BytesIO()
    with gzip.GzipFile(fileobj=gz_buf, mode='wb', mtime=0) as gz:
        gz.write(binary_bytes)
    gz_bytes = gz_buf.getvalue()
    manifest = {
        'schema': 1,
        'kind': 'mile-ci-release-candidate',
        'version': version,
        'source_commit': commit,
        'workflow_run_id': run_id,
        'sha256': hashlib.sha256(binary_bytes).hexdigest(),
        'bytes': len(binary_bytes),
        'payload': 'sub2api.gz',
        'payload_sha256': hashlib.sha256(gz_bytes).hexdigest(),
        'payload_bytes': len(gz_bytes),
        'quota_tests': quota if quota is not None else {'passed': 166, 'skipped': 0, 'failed': 0},
        'migrations_count': 324,
        'migrations_sha256': 'a' * 64,
    }
    with zipfile.ZipFile(buf, 'w', zipfile.ZIP_DEFLATED) as zf:
        zf.writestr('manifest.json', json.dumps(manifest))
        zf.writestr('sub2api.gz', gz_bytes)
        if extra_member is not None:
            zf.writestr(extra_member[0], extra_member[1])
    return buf.getvalue(), manifest


class ReleaseWorkerTests(unittest.TestCase):
    def test_submit_and_stage_verifies_checksum_and_quota_gate(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / 'deploy'
            spool = Path(tmp) / 'spool'
            (root / 'releases').mkdir(parents=True)
            commit = 'a' * 40
            binary = b'fake-linux-elf-binary-payload'
            expected_sha = hashlib.sha256(binary).hexdigest()
            zip_bytes, _ = make_candidate_zip('0.2.25-mile.20261126.rc1', commit, 12345, binary)
            local_zip = Path(tmp) / 'candidate.zip'
            local_zip.write_bytes(zip_bytes)

            submitted = release_worker.submit_candidate(spool, '0.2.25-mile.20261126.rc1', commit, expected_sha, 12345)
            staged = release_worker.stage_from_submission(
                submitted['submission_file'], root, spool, 'huohua325/sub2api', '',
                local_zip=local_zip, run_preflight=False,
            )
            self.assertEqual(staged['status'], 'staged-awaiting-approval')
            self.assertEqual(staged['binary_sha256'], expected_sha)
            unpacked = Path(staged['release_dir']) / 'sub2api'
            self.assertEqual(unpacked.read_bytes(), binary)

            # Applying without explicit --approve must be refused.
            with self.assertRaises(DeliveryError):
                release_worker.apply_approved_release(staged['release_id'], root, spool, approve=False)

    def test_rejects_skipped_quota_tests_and_zip_slip(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / 'deploy'
            spool = Path(tmp) / 'spool'
            (root / 'releases').mkdir(parents=True)
            commit = 'b' * 40
            binary = b'binary'
            sha = hashlib.sha256(binary).hexdigest()

            bad_quota_zip, _ = make_candidate_zip(
                '0.2.25-mile.20261126.rc2', commit, 99, binary,
                quota={'passed': 10, 'skipped': 1, 'failed': 0},
            )
            bad_zip_path = Path(tmp) / 'bad-quota.zip'
            bad_zip_path.write_bytes(bad_quota_zip)
            sub = release_worker.submit_candidate(spool, '0.2.25-mile.20261126.rc2', commit, sha, 99)
            with self.assertRaises(DeliveryError):
                release_worker.stage_from_submission(
                    sub['submission_file'], root, spool, 'huohua325/sub2api', '',
                    local_zip=bad_zip_path, run_preflight=False,
                )

            slip_zip, _ = make_candidate_zip(
                '0.2.25-mile.20261126.rc3', commit, 100, binary,
                extra_member=('../escape.txt', b'bad'),
            )
            slip_path = Path(tmp) / 'slip.zip'
            slip_path.write_bytes(slip_zip)
            sub2 = release_worker.submit_candidate(spool, '0.2.25-mile.20261126.rc3', commit, sha, 100)
            with self.assertRaises(DeliveryError):
                release_worker.stage_from_submission(
                    sub2['submission_file'], root, spool, 'huohua325/sub2api', '',
                    local_zip=slip_path, run_preflight=False,
                )


if __name__ == '__main__':
    unittest.main()

