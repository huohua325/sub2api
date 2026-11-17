import copy
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from delivery_core import DeliveryError, digest, json_hash, tree_hash, write_json
from delivery_workflow import verify_record
import adopt_frontend_evidence as promotion


class AdoptFrontendTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name)
        self.source, self.root = self.base / 'source', self.base / 'destination'
        self.source.mkdir()
        self.exe = self.base / 'pnpm.cmd'; self.exe.write_text('fixture frontend command executable')
        self.exe.chmod(0o755)
        self.tools = {'node': 'v22.17.1', 'pnpm': '10.13.1', 'python': sys.version, 'platform': sys.platform}
        self.env_hash = json_hash({'test_environment': 'identity only'})
        self.write(self.source, '.gitignore', '/output/\n/frontend/node_modules/\n/backend/internal/web/dist/\n')
        for name in promotion.INPUTS:
            if name == 'frontend': continue
            self.write(self.source, name, 'identical bytes for ' + name)
        for name in ['frontend/package.json', 'frontend/pnpm-lock.yaml', 'frontend/src/App.vue']:
            self.write(self.source, name, 'committed frontend ' + name)
        self.write(self.source, promotion.ARTIFACT + '/index.html', '<html>tested frontend</html>')
        self.git(self.source, 'init', '-q')
        self.commit(self.source)
        subprocess.check_call(['git', 'clone', '-q', str(self.source), str(self.root)], stderr=subprocess.DEVNULL)
        self.write(self.root, 'frontend/node_modules/.modules.yaml', (self.source / 'frontend/node_modules/.modules.yaml').read_text())
        shutil.copytree(self.source / promotion.ARTIFACT, self.root / promotion.ARTIFACT)
        self.source_output, self.output = self.source / 'output/validated', self.root / 'output/promoted'
        self.source_output.mkdir(parents=True)
        self.inputs = tree_hash(self.source, promotion.INPUTS, inputs=True)
        self.artifacts = tree_hash(self.source, [promotion.ARTIFACT])
        commands = {'lint': ['run', 'lint:check'], 'tests': ['run', 'test:run'],
                    'types': ['exec', 'vue-tsc', '-b'], 'frontend-build': ['exec', 'vite', 'build', '--config', 'vite.config.ts']}
        self.manifest = {'schema': 1, 'root': str(self.source), 'source_commit': self.git(self.source, 'rev-parse', 'HEAD'),
                         'validated_inputs': self.inputs, 'frontend_artifacts': self.artifacts, 'steps': []}
        for name in promotion.STEPS:
            log = self.source_output / (name + '-123.log'); log.write_bytes((name + ': full checks passed\r\n').encode())
            identity = dict(inputs=self.inputs, argv=[str(self.exe), *commands[name]], cwd=str(self.source / 'frontend'),
                            tools=self.tools, executable=digest(self.exe), environment_hash=self.env_hash)
            record = dict(name=name, key=json_hash(identity), inputs=self.inputs, command=identity['argv'],
                          cwd=identity['cwd'], tools=self.tools, environment_hash=self.env_hash,
                          status='passed', exit_code=0, log=log.name, log_sha256=digest(log),
                          artifacts=self.artifacts if name == 'frontend-build' else {},
                          started_at='2026-11-17T01:00:00+00:00', finished_at='2026-11-17T01:01:00+00:00', elapsed_seconds=60)
            self.manifest['steps'].append(record)
        self.manifest['steps'].append({'name': 'quota-race', 'status': 'failed', 'log': 'do-not-import.log'})
        self.save()
        self.mock_tools = patch('adopt_frontend_evidence._runtime_tools', return_value=self.tools)
        self.mock_tools.start(); self.addCleanup(self.mock_tools.stop)

    def write(self, root, name, text):
        path = root / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_text(text)

    def git(self, root, *args):
        return subprocess.check_output(['git', *args], cwd=root, text=True, stderr=subprocess.PIPE).strip()

    def commit(self, root):
        self.git(root, 'add', '-A')
        self.git(root, '-c', 'user.name=Delivery test', '-c', 'user.email=delivery@example.invalid', 'commit', '-qm', 'test inputs')

    def save(self): write_json(self.source_output / 'manifest.json', self.manifest)

    def adopt(self):
        return promotion.adopt_frontend_evidence(self.root, self.source, self.source_output, self.output)

    def rejected(self):
        with self.assertRaises(DeliveryError): self.adopt()
        self.assertFalse((self.output / 'manifest.json').exists())

    def test_identical_checkout_imports_only_four_records_and_keeps_source_unchanged(self):
        original = (self.source_output / 'manifest.json').read_bytes()
        logs = {record['log']: (self.source_output / record['log']).read_bytes() for record in self.manifest['steps'][:4]}
        result = self.adopt()
        self.assertEqual(result['root'], str(self.root))
        self.assertEqual([record['name'] for record in result['steps']], list(promotion.STEPS))
        self.assertEqual(result['validated_inputs'], self.inputs)
        self.assertEqual(result['frontend_artifacts'], self.artifacts)
        self.assertFalse(result['promotion']['destination_checks_executed'])
        self.assertFalse(result['promotion']['destination_dependency_installation_claimed'])
        for original_record, record in zip(self.manifest['steps'][:4], result['steps']):
            self.assertEqual(record['status'], 'reused')
            for key in ['key', 'cwd', 'command', 'tools', 'environment_hash', 'started_at', 'finished_at']:
                self.assertEqual(record[key], original_record[key])
            self.assertEqual(record['origin']['root'], str(self.source))
            self.assertEqual(record['origin']['manifest_sha256'], digest(self.source_output / 'manifest.json'))
            self.assertEqual((self.output / record['log']).read_bytes(), logs[original_record['log']])
            verify_record(self.root, self.output, record)
        self.assertEqual((self.source_output / 'manifest.json').read_bytes(), original)
        self.assertFalse((self.output / 'freeze.json').exists())
        self.assertEqual(len(list(self.output.rglob('*.log'))), 4)

    def test_latest_failure_cannot_fall_back_to_older_success(self):
        failed = copy.deepcopy(self.manifest['steps'][1]); failed.update(status='failed', exit_code=1)
        self.manifest['steps'].append(failed); self.save(); self.rejected()

    def test_source_log_tampering_is_rejected(self):
        (self.source_output / self.manifest['steps'][0]['log']).write_text('tampered')
        self.rejected()

    def test_source_artifact_tampering_is_rejected(self):
        self.write(self.source, promotion.ARTIFACT + '/index.html', 'tampered output')
        self.rejected()

    def test_destination_artifact_tampering_is_rejected(self):
        self.write(self.root, promotion.ARTIFACT + '/index.html', 'different promoted bytes')
        self.rejected()

    def test_changed_source_or_delivery_tool_is_rejected(self):
        for path in ['frontend/src/App.vue', 'tools/delivery_core.py']:
            original = (self.source / path).read_bytes()
            (self.source / path).write_bytes(b'changed')
            self.rejected()
            (self.source / path).write_bytes(original)

    def test_committed_destination_frontend_change_is_rejected(self):
        self.write(self.root, 'frontend/src/App.vue', 'new committed frontend')
        self.commit(self.root); self.rejected()

    def test_committed_destination_tool_change_is_rejected(self):
        self.write(self.root, 'tools/delivery.py', 'new committed tool')
        self.commit(self.root); self.rejected()

    def test_nonignored_untracked_frontend_input_is_rejected(self):
        self.write(self.root, 'frontend/src/untracked.ts', 'unreviewed frontend')
        self.rejected()

    def test_existing_untracked_backend_user_files_are_neither_imported_nor_changed(self):
        self.write(self.source, 'backend/docs/user.md', 'user-owned notes')
        self.write(self.source, 'backend/internal/user_test.go', 'user-owned test')
        self.adopt()
        self.assertEqual((self.source / 'backend/docs/user.md').read_text(), 'user-owned notes')
        self.assertFalse((self.root / 'backend/docs/user.md').exists())

    def test_dependency_metadata_mismatch_is_rejected(self):
        self.write(self.root, 'frontend/node_modules/.modules.yaml', 'different installed dependency metadata')
        self.rejected()

    def test_different_toolchain_and_changed_executable_are_rejected(self):
        with patch('adopt_frontend_evidence._runtime_tools', return_value={**self.tools, 'node': 'v0.0.0'}):
            self.rejected()
        self.exe.write_text('different executable bytes')
        self.rejected()

    def test_incomplete_schema_scope_and_filtered_command_are_rejected(self):
        original = copy.deepcopy(self.manifest)
        for field, value in [('schema', 9), ('root', str(self.root)), ('validated_inputs', {'frontend': self.inputs['frontend']})]:
            self.manifest = {**copy.deepcopy(original), field: value}; self.save(); self.rejected()
        self.manifest = copy.deepcopy(original)
        self.manifest['steps'][1]['command'] += ['one-test-only']
        record = self.manifest['steps'][1]
        record['key'] = json_hash(dict(inputs=record['inputs'], argv=record['command'], cwd=record['cwd'],
                                      tools=record['tools'], executable=digest(self.exe), environment_hash=record['environment_hash']))
        self.save(); self.rejected()

    def test_log_path_escape_and_existing_manifest_are_rejected(self):
        original = self.manifest['steps'][0]['log']
        self.manifest['steps'][0]['log'] = '../private.log'; self.save(); self.rejected()
        self.manifest['steps'][0]['log'] = original; self.save()
        self.adopt()
        with self.assertRaises(DeliveryError): self.adopt()

    def test_destination_must_stay_in_output(self):
        self.output = self.root / 'frontend/evidence'
        self.rejected()


if __name__ == '__main__': unittest.main()
