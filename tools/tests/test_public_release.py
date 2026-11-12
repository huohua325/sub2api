from collections import Counter
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import sys
import tempfile
import threading
import time
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
try:
    from check_public_release import verify
except ImportError:
    verify = None


class PublicReleaseTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.dist = Path(self.tmp.name)
        (self.dist / 'assets').mkdir()
        (self.dist / 'assets/app.js').write_bytes(b'correct-script')
        self.html = b'<html><script src="/assets/app.js"></script></html>'
        self.count = Counter(); self.mode = 'normal'
        owner = self
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                owner.count[self.path] += 1
                status, body = 200, owner.html if self.path == '/' else b'correct-script'
                if self.path == '/assets/app.js':
                    if owner.mode == 'transient' and owner.count[self.path] == 1: status = 503
                    if owner.mode == 'mismatch': body = b'wrong-script'
                    if owner.mode == 'missing': status = 404
                    if owner.mode == 'slow': time.sleep(.4)
                self.send_response(status); self.end_headers()
                try: self.wfile.write(body)
                except OSError: pass
            def log_message(self, *args): pass
        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.addCleanup(self.server.server_close); self.addCleanup(self.server.shutdown)
        self.base = 'http://127.0.0.1:' + str(self.server.server_port)

    def check(self, **kwargs):
        self.assertIsNotNone(verify, 'Bounded public verifier is not implemented')
        return verify(self.base, self.dist, **kwargs)

    def test_hash_mismatch_fails_without_retry(self):
        self.mode = 'mismatch'
        result = self.check()
        self.assertFalse(result['ok'])
        self.assertEqual(self.count['/assets/app.js'], 1)
        self.assertIn('hash mismatch', json.dumps(result))

    def test_transient_failure_is_retried_with_evidence(self):
        self.mode = 'transient'
        result = self.check()
        self.assertTrue(result['ok'])
        self.assertEqual(self.count['/assets/app.js'], 2)
        self.assertEqual(len(result['resources']['/assets/app.js']['attempts']), 2)

    def test_missing_asset_fails_without_retry(self):
        self.mode = 'missing'
        result = self.check()
        self.assertFalse(result['ok'])
        self.assertEqual(self.count['/assets/app.js'], 1)

    def test_global_deadline_bounds_slow_response(self):
        self.mode = 'slow'; start = time.monotonic()
        result = self.check(total_timeout=.15)
        self.assertFalse(result['ok'])
        self.assertLess(time.monotonic() - start, .7)


if __name__ == '__main__': unittest.main()
