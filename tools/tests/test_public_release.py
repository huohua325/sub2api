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
        (self.dist / 'index.html').write_bytes(self.html)
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
                self.send_response(status)
                if owner.mode == 'content-length': self.send_header('Content-Length', str(len(body)))
                self.end_headers()
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

    def test_health_and_public_entry_checks_run_before_a_slow_large_asset(self):
        self.mode = 'slow'
        result = self.check(total_timeout=.15)
        self.assertFalse(result['ok'])
        for path in ['/health', '/login', '/api/v1/settings/public']:
            self.assertTrue(result['resources'][path]['ok'])

    def test_missing_dist_rejected_before_network(self):
        from delivery_core import DeliveryError
        self.assertIsNotNone(verify)
        with self.assertRaises(DeliveryError): verify(self.base, self.dist / 'missing')
        self.assertEqual(self.count['/'], 0)
    def test_content_length_response_can_close_after_last_chunk(self):
        self.mode = 'content-length'
        self.assertTrue(self.check()['ok'])

    def test_resume_only_fetches_failed_assets_but_rechecks_entry_and_health(self):
        (self.dist / 'assets/keep.js').write_bytes(b'correct-script')
        self.mode = 'transient'
        first = self.check(attempts=1)
        self.assertFalse(first['ok'])
        original = json.dumps(first, sort_keys=True)
        self.mode = 'normal'
        result = self.check(resume=first)
        self.assertTrue(result['ok'])
        self.assertEqual(self.count['/assets/app.js'], 2)
        self.assertEqual(self.count['/assets/keep.js'], 1)
        for path in ['/', '/health', '/login', '/api/v1/settings/public']:
            self.assertEqual(self.count[path], 2)
        self.assertEqual(json.dumps(first, sort_keys=True), original)
        self.assertTrue(result['resources']['/assets/keep.js']['reused'])

    def test_resume_rejects_another_build_origin_or_expired_report(self):
        from delivery_core import DeliveryError
        first = self.check()
        for change in ['origin', 'build', 'time']:
            with self.subTest(change=change):
                report = json.loads(json.dumps(first))
                if change == 'origin': report['base_url'] = 'https://other.invalid'
                if change == 'build': report['dist_sha256'] = '0' * 64
                if change == 'time': report['checked_at'] = '2000-01-01T00:00:00+00:00'
                with self.assertRaises(DeliveryError): self.check(resume=report)

    def test_resume_success_needs_the_original_matching_asset_hash(self):
        from delivery_core import DeliveryError
        first = self.check()
        first['resources']['/assets/app.js']['sha256'] = '0' * 64
        with self.assertRaises(DeliveryError): self.check(resume=first)

    def test_resume_chain_cannot_extend_the_original_asset_verification_age(self):
        first = self.check()
        first['resources']['/assets/app.js']['verified_at'] = '2000-01-01T00:00:00+00:00'
        self.assertTrue(self.check(resume=first)['ok'])
        self.assertEqual(self.count['/assets/app.js'], 2)


if __name__ == '__main__': unittest.main()
