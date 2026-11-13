import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

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
            if name == 'backend-build': record.update(version='test1', source_commit=self.commit)
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

    def test_verified_delta_is_reused_across_release_tool_repackaging(self):
        import bsdiff4
        old = self.out / 'previous'; old.write_bytes(b'old program')
        from delivery_core import digest
        args = [self.root, self.out, None, self.binary, 'test1', digest(old), self.commit]
        with patch.object(bsdiff4, 'diff', wraps=bsdiff4.diff) as diff:
            args[2] = self.out / 'release-one'
            first = package_release(*args, baseline_binary=old)
            args[2] = self.out / 'release-two'
            second = package_release(*args, baseline_binary=old)
            self.assertEqual(diff.call_count, 1)
        self.assertEqual(first['payload_sha256'], second['payload_sha256'])
        self.assertEqual(bsdiff4.patch(old.read_bytes(), (args[2] / 'app.bsdiff').read_bytes()), self.binary.read_bytes())

    def test_corrupt_cached_delta_is_recomputed_and_target_is_reverified(self):
        import bsdiff4
        from release_package import verified_delta
        old = self.out / 'previous'; old.write_bytes(b'old program')
        with patch.object(bsdiff4, 'diff', wraps=bsdiff4.diff) as diff:
            verified_delta(self.root, old, self.binary)
            cache = next((self.root / 'output/delivery-cache/deltas').rglob('app.bsdiff'))
            cache.write_bytes(b'corrupt')
            delta = verified_delta(self.root, old, self.binary)
            self.assertEqual(diff.call_count, 2)
        self.assertEqual(bsdiff4.patch(old.read_bytes(), delta), self.binary.read_bytes())
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

    def test_untracked_backend_production_file_cannot_enter_frontend_lane(self):
        (self.root / 'backend/extra.go').write_text('package main')
        with self.assertRaises(DeliveryError): self.package()

    def test_untracked_migration_cannot_enter_frontend_lane(self):
        folder = self.root / 'backend/migrations'; folder.mkdir()
        (folder / '999.sql').write_text('SELECT 1;')
        with self.assertRaises(DeliveryError): self.package()

    def test_build_provenance_cannot_reference_another_source_tree(self):
        (self.root / 'frontend').mkdir()
        (self.root / 'frontend/source.ts').write_text('new source')
        self.git('add', 'frontend/source.ts')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'new source')
        with self.assertRaises(DeliveryError): self.package()

    def test_documentation_followup_can_preserve_original_runtime_source(self):
        (self.root / 'notes.md').write_text('release record')
        self.git('add', 'notes.md')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'documentation only')
        self.assertEqual(self.package()['source_commit'], self.commit)

    def test_build_identity_survives_docs_but_changes_with_app_or_version(self):
        from delivery_workflow import select_build_source
        previous = {'source_commit': self.commit, 'version': 'test1'}
        (self.root / 'notes.md').write_text('release notes')
        self.git('add', 'notes.md')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'notes')
        current = self.git('rev-parse', 'HEAD')
        self.assertEqual(select_build_source(self.root, current, 'test1', previous), self.commit)
        self.assertEqual(select_build_source(self.root, current, 'test2', previous), current)
        (self.root / 'backend/source.go').write_text('application changed')
        self.git('add', 'backend/source.go')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'app')
        current = self.git('rev-parse', 'HEAD')
        self.assertEqual(select_build_source(self.root, current, 'test1', previous), current)

    def test_committed_backend_test_only_change_does_not_change_production_lane(self):
        (self.root / 'backend/source_test.go').write_text('package example')
        self.git('add', 'backend/source_test.go')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'test fixture only')
        data = json.loads((self.out / 'manifest.json').read_text())
        next(s for s in data['steps'] if s['name']=='backend-build')['source_commit']=self.git('rev-parse','HEAD')
        (self.out / 'manifest.json').write_text(json.dumps(data))
        self.assertTrue(self.package()['backend_unchanged'])


if __name__ == '__main__': unittest.main()
