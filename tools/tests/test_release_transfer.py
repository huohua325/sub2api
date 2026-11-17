import io
import json
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
import urllib.error
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError, digest
from release_transfer import transfer


class TransferTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory(); self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.payload = self.root / 'sub2api.gz'; self.payload.write_bytes(b'verified binary payload')
        self.checksum = digest(self.payload)
        self.output = self.root / 'result.json'
        (self.root / 'manifest.json').write_text(json.dumps(dict(payload='sub2api.gz',
            payload_sha256=self.checksum, source_commit='a' * 40)))

    def test_private_transfer_and_failure_both_delete_only_owned_draft(self):
        for fail in [False, True]:
            with self.subTest(fail=fail):
                requests = []; ssh_calls = []
                class Client:
                    def open(inner, req, timeout):
                        requests.append(req)
                        if req.get_method() == 'DELETE': return io.BytesIO(b'')
                        if '/assets?' in req.full_url: return io.BytesIO(b'{"id":7}')
                        if '/releases/assets/' in req.full_url:
                            raise urllib.error.HTTPError(req.full_url, 302, 'redirect',
                                {'Location': 'https://release-assets.githubusercontent.com/temporary'}, None)
                        return io.BytesIO(b'{"id":42}')
                def run(argv, **kwargs):
                    if argv[0] == 'git':
                        return SimpleNamespace(returncode=0, stdout='username=owner\npassword=private-git-credential\n')
                    ssh_calls.append((argv, kwargs))
                    return SimpleNamespace(returncode=1 if fail else 0, stdout=json.dumps(
                        dict(sha256=self.checksum, bytes=self.payload.stat().st_size)))
                with patch('release_transfer.urllib.request.build_opener', return_value=Client()), \
                     patch('release_transfer.subprocess.run', side_effect=run):
                    if fail:
                        with self.assertRaises(DeliveryError):
                            transfer(self.root, 'root@host', '/opt/sub2api/releases/test', None, self.output)
                    else: transfer(self.root, 'root@host', '/opt/sub2api/releases/test', None, self.output)
                result = json.loads(self.output.read_text())
                self.assertTrue(result['draft_deleted'])
                self.assertEqual(result['status'], 'failed' if fail else 'passed')
                deleted = [r.full_url for r in requests if r.get_method() == 'DELETE']
                self.assertEqual(deleted, ['https://api.github.com/repos/huohua325/sub2api/releases/42'])
                body = json.loads(requests[0].data)
                self.assertTrue(body['draft'])
                self.assertNotIn('private-git-credential', repr(ssh_calls))
                self.assertNotIn('temporary', self.output.read_text())

    def test_bad_payload_or_uncontained_remote_is_rejected_before_credential_access(self):
        with patch('release_transfer.subprocess.run') as run:
            with self.assertRaises(DeliveryError):
                transfer(self.root, 'root@host', '/opt/sub2api/bin', None, self.output)
            with self.assertRaises(DeliveryError):
                transfer(self.root, 'root@host', '/opt/sub2api/releases/..', None, self.output)
            self.payload.write_bytes(b'tampered')
            with self.assertRaises(DeliveryError):
                transfer(self.root, 'root@host', '/opt/sub2api/releases/test', None, self.output)
            run.assert_not_called()


if __name__ == '__main__': unittest.main()
