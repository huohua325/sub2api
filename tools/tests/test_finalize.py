from argparse import Namespace
import json
from pathlib import Path
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import delivery
from delivery_core import DeliveryError, Runner, write_json
from delivery_workflow import freeze_source, frontend_lint_command


class FinalizeTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'source').write_text('stable')
        self.dist = self.root / 'backend/internal/web/dist'; self.dist.mkdir(parents=True)
        (self.dist / 'index.html').write_text('<html/>')
        self.out = self.root / 'output/change'
        freeze_source(self.root, self.out, reviewed=True)
        runner = Runner(self.root, self.out)
        for name in ['lint', 'tests', 'types', 'frontend-build']:
            runner.run(name, [sys.executable, '-c', 'print("checked")'], ['source'],
                       artifacts=['backend/internal/web/dist'] if name == 'frontend-build' else [])
        self.args = Namespace(output=self.out, version='test1', features=['unified-coding'],
                              jobs=2, go_command='["go"]', go_executable=None)

    def invoke(self, quota, browser, build):
        function = getattr(delivery, 'finalize', None)
        self.assertIsNotNone(function, 'Finalize must schedule, verify and collect release evidence')
        with patch.object(delivery, 'ROOT', self.root), patch('release_package.require_committed_inputs'), \
             patch.object(delivery, 'quota', side_effect=quota), \
             patch.object(delivery, 'artifact_review', side_effect=browser), \
             patch.object(delivery, 'build_backend', side_effect=build):
            return function(self.args)

    def fake_quota(self, args):
        runner = Runner(self.root, args.output)
        record = runner.run('quota-race', [sys.executable, '-c',
            'print(\'{"Action":"pass","Package":"repo","Test":"TestCodingQuotaDBExample"}\')'], ['source'], cache=False)
        record['quota_tests'] = {'passed': 1, 'skipped': 0, 'failed': 0}
        write_json(args.output / 'manifest.json', runner.manifest)

    def fake_browser(self, args):
        runner = Runner(self.root, args.output)
        result = args.output / 'artifact-browser/result.json'
        write_json(result, {'ok': True, 'engines': {'chromium': {}, 'webkit': {}}})
        runner.run('artifact-browser', [sys.executable, '-c', 'print("browser checked")'], ['source'],
                   artifacts=[result.relative_to(self.root).as_posix()], cache=False)
        runner.run('artifact-feature-unified-coding', [sys.executable, '-c', 'print("feature checked")'], ['source'],
                   artifacts=[result.relative_to(self.root).as_posix()], cache=False)

    def fake_build(self, args):
        runner = Runner(self.root, args.output)
        binary = args.output / 'sub2api-linux-amd64'; binary.write_bytes(b'program')
        record = runner.run('backend-build', [sys.executable, '-c', 'print("compiled")'], ['source'],
                            artifacts=[binary.relative_to(self.root).as_posix()])
        record.update(source_commit='a' * 40, version=args.version)
        write_json(args.output / 'manifest.json', runner.manifest)

    def test_quota_and_browser_overlap_and_collector_preserves_build_and_full_checks(self):
        barrier = threading.Barrier(2)
        def quota(args): barrier.wait(timeout=3); self.fake_quota(args)
        def browser(args): barrier.wait(timeout=3); self.fake_browser(args)
        self.invoke(quota, browser, self.fake_build)
        result = json.loads((self.out / 'finalize.json').read_text())
        self.assertEqual(result['status'], 'passed')
        self.assertEqual(result['features'], ['unified-coding'])
        manifest = json.loads((self.out / 'manifest.json').read_text())
        self.assertEqual({s['name'] for s in manifest['steps']}, {'lint', 'tests', 'types', 'frontend-build', 'quota-race', 'artifact-browser', 'artifact-feature-unified-coding', 'backend-build'})
        self.assertTrue((self.out / 'sub2api-linux-amd64').exists())

    def test_failed_parallel_job_blocks_finalize_even_with_old_success(self):
        def failure(args): raise DeliveryError('new quota failed')
        write_json(self.out / 'finalize.json', {'status': 'passed'})
        with self.assertRaisesRegex(DeliveryError, 'quota'):
            self.invoke(failure, self.fake_browser, self.fake_build)
        self.assertEqual(json.loads((self.out / 'finalize.json').read_text())['status'], 'failed')
        self.assertNotIn('quota-race', [s['name'] for s in json.loads((self.out / 'manifest.json').read_text())['steps']])

    def test_changed_frontend_is_rejected_before_any_job(self):
        (self.dist / 'index.html').write_text('unverified output')
        def unexpected(args): self.fail('Started work on unverified frontend')
        with self.assertRaises(DeliveryError): self.invoke(unexpected, unexpected, unexpected)

    def test_source_change_during_parallel_work_blocks_finalize(self):
        def browser(args):
            self.fake_browser(args)
            p = self.root / 'backend/changed.go'; p.write_text('new app source')
        with self.assertRaises(DeliveryError): self.invoke(self.fake_quota, browser, self.fake_build)
        self.assertNotEqual(json.loads((self.out / 'finalize.json').read_text())['status'], 'passed')

    def test_lint_native_cache_is_content_based_and_dependency_changes_invalidate_it(self):
        frontend = self.root / 'frontend'; frontend.mkdir()
        lock = frontend / 'pnpm-lock.yaml'; lock.write_text('first')
        first = frontend_lint_command(self.root, 'pnpm', {'node': 'test'})
        self.assertIn('content', first)
        self.assertNotIn('--fix', first)
        self.assertEqual(first, frontend_lint_command(self.root, 'pnpm', {'node': 'test'}))
        lock.write_text('second')
        self.assertNotEqual(first, frontend_lint_command(self.root, 'pnpm', {'node': 'test'}))
        self.assertEqual(frontend_lint_command(self.root, 'pnpm', {}, fresh=True), ['pnpm', 'run', 'lint:check'])
