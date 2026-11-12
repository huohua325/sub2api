import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError, Runner, digest
try:
    from release_package import package_release
except ImportError:
    package_release = None


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        for name, content in [('src.txt', 'source'), ('backend/source.go', 'original'),
                              ('.gitignore', 'out/\nbackend/internal/web/dist/\n')]:
            p = self.root / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_text(content)
        def git(*args):
            return subprocess.check_output(['git', *args], cwd=self.root, stderr=subprocess.DEVNULL, text=True).strip()
        self.git = git
        git('init'); git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'baseline')
        self.commit = git('rev-parse', 'HEAD')
        self.out = self.root / 'out'; self.runner = Runner(self.root, self.out)
        self.binary = self.out / 'app'; self.binary.write_bytes(b'\x7fELF-new-program')
        dist = self.root / 'backend/internal/web/dist'; dist.mkdir(parents=True); (dist / 'index.html').write_text('<html/>')
        review = self.out / 'artifact-browser/result.json'; review.parent.mkdir()
        review.write_text(json.dumps({'ok': True, 'engines': {'chromium': {}, 'webkit': {}}}))
        for name in ['lint', 'tests', 'types', 'frontend-build', 'artifact-browser', 'quota-race', 'backend-build']:
            artifacts = ['out/app'] if name == 'backend-build' else ['backend/internal/web/dist'] if name == 'frontend-build' else ['out/artifact-browser/result.json'] if name == 'artifact-browser' else []
            record = self.runner.run(name, [sys.executable, '-c', "print('verified')"], ['src.txt'], artifacts=artifacts)
            if name == 'quota-race': record['quota_tests'] = {'passed': 1, 'skipped': 0, 'failed': 0}
            if name == 'backend-build': record['version'] = 'test1'
        from delivery_core import write_json
        write_json(self.out / 'manifest.json', self.runner.manifest)
    def package(self):
        self.assertIsNotNone(package_release, 'Verified release packaging is not implemented')
        return package_release(self.root, self.out, self.out / 'release', self.binary, 'test1', 'b'*64, self.commit, full_upload=True)
    def test_package_binds_program_frontend_source_and_evidence(self):
        manifest = self.package()
        self.assertEqual(manifest['source_commit'], self.commit)
        self.assertEqual(manifest['sha256'], digest(self.binary))
        self.assertTrue(manifest['backend_unchanged'])
        self.assertEqual(manifest['quota_tests']['passed'], 1)
        self.assertTrue((self.out / 'release/sub2api.gz').exists())
    def test_changed_input_and_all_skipped_quota_are_rejected(self):
        data = json.loads((self.out / 'manifest.json').read_text())
        next(s for s in data['steps'] if s['name'] == 'quota-race')['quota_tests'] = {'passed': 0, 'skipped': 1, 'failed': 0}
        (self.out / 'manifest.json').write_text(json.dumps(data))
        with self.assertRaises(DeliveryError): self.package()
    def test_changed_artifact_is_rejected(self):
        self.binary.write_bytes(b'changed-unverified-program')
        with self.assertRaises(DeliveryError): self.package()
    def test_package_version_must_match_compiled_version(self):
        data = json.loads((self.out / 'manifest.json').read_text())
        next(s for s in data['steps'] if s['name'] == 'backend-build')['version'] = 'different'
        (self.out / 'manifest.json').write_text(json.dumps(data))
        with self.assertRaises(DeliveryError): self.package()


if __name__ == '__main__': unittest.main()
