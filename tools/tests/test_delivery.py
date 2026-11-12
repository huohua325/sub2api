import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
try:
    from delivery_core import Runner, DeliveryError
except ImportError:
    Runner = None
    DeliveryError = RuntimeError


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'source.txt').write_text('first')
        self.out = self.root / 'evidence'

    def runner(self, **kwargs):
        self.assertIsNotNone(Runner, 'Timed content-bound runner is not implemented')
        return Runner(self.root, self.out, tools={'python': sys.version}, **kwargs)

    def command(self, extra=''):
        return [sys.executable, '-c',
                "from pathlib import Path; p=Path('count'); "
                "p.write_text(str(int(p.read_text())+1) if p.exists() else '1'); "
                "Path('artifact').write_text('result'); " + extra]

    def test_unchanged_inputs_reuse_real_command_and_verify_artifact(self):
        run = self.runner()
        a = run.run('build', self.command(), ['source.txt'], artifacts=['artifact'])
        b = run.run('build', self.command(), ['source.txt'], artifacts=['artifact'])
        self.assertEqual((a['status'], b['status']), ('passed', 'reused'))
        self.assertEqual((self.root / 'count').read_text(), '1')
        self.assertEqual(len(b['artifacts']['artifact']), 64)
        self.assertGreaterEqual(a['elapsed_seconds'], 0)
        self.assertEqual(json.loads((self.out / 'manifest.json').read_text())['steps'][-1]['status'], 'reused')

    def test_edit_command_environment_and_corrupt_artifact_invalidate(self):
        run = self.runner(env={**os.environ, 'MILE_TEST_VALUE': 'one'})
        for action in ['first', 'source', 'command', 'artifact', 'environment']:
            if action == 'source': (self.root / 'source.txt').write_text('second')
            if action == 'artifact': (self.root / 'artifact').write_text('corrupt')
            if action == 'environment': run.env['MILE_TEST_VALUE'] = 'two'
            suffix = "print('new-command')" if action in ['command', 'artifact', 'environment'] else ''
            self.assertEqual(run.run('build', self.command(suffix), ['source.txt'], artifacts=['artifact'])['status'], 'passed')
        self.assertEqual((self.root / 'count').read_text(), '5')
        self.assertNotIn('MILE_TEST_VALUE', (self.out / 'manifest.json').read_text())

    def test_failed_command_and_missing_artifact_never_cached(self):
        run = self.runner()
        for _ in range(2):
            with self.assertRaises(DeliveryError):
                run.run('bad', self.command('raise SystemExit(7)'), ['source.txt'])
        self.assertEqual((self.root / 'count').read_text(), '2')
        with self.assertRaises(DeliveryError):
            run.run('missing', self.command(), ['source.txt'], artifacts=['missing'])

    def test_edit_during_run_fails_and_preserves_failure_evidence(self):
        run = self.runner()
        with self.assertRaisesRegex(DeliveryError, 'changed'):
            run.run('editing', [sys.executable, '-c', "from pathlib import Path; Path('source.txt').write_text('edited')"], ['source.txt'])
        self.assertEqual(run.manifest['steps'][-1]['status'], 'failed')
        self.assertTrue((self.out / run.manifest['steps'][-1]['log']).exists())

    def test_artifact_deletion_and_new_source_file_invalidate_directory_inputs(self):
        run = self.runner()
        src = self.root / 'src'; src.mkdir(); (src / 'one').write_text('a')
        run.run('build', self.command(), ['src'], artifacts=['artifact'])
        (src / 'two').write_text('b')
        run.run('build', self.command(), ['src'], artifacts=['artifact'])
        (self.root / 'artifact').unlink()
        run.run('build', self.command(), ['src'], artifacts=['artifact'])
        self.assertEqual((self.root / 'count').read_text(), '3')

    def test_timeout_is_recorded_as_failure(self):
        run = self.runner()
        with self.assertRaises(DeliveryError):
            run.run('timeout', [sys.executable, '-c', 'import time; time.sleep(3)'], ['source.txt'], timeout=.1)
        self.assertEqual(run.manifest['steps'][-1]['status'], 'failed')

    def test_explicit_dependency_metadata_is_hashed_even_inside_ignored_directory(self):
        folder = self.root / 'node_modules'; folder.mkdir()
        metadata = folder / '.modules.yaml'; metadata.write_text('version: 1')
        run = self.runner()
        run.run('build', self.command(), ['node_modules/.modules.yaml'])
        metadata.write_text('version: 2')
        run.run('build', self.command(), ['node_modules/.modules.yaml'])
        self.assertEqual((self.root / 'count').read_text(), '2')


if __name__ == '__main__':
    unittest.main()
