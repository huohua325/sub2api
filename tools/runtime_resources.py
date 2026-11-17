"""Plan or apply application resource limits without replacing the program or database."""
import argparse
import copy
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tempfile

from delivery_core import DeliveryError, digest, exclusive, utcnow
from release_server import DockerRuntime, assert_preserved

MIB = 1024 * 1024


@dataclass(frozen=True)
class Settings:
    hard_mib: int = 768
    swap_mib: int = 1024  # Docker's combined memory + swap limit.
    go_mib: int = 512
    gogc: int = 50
    body_mib: int | None = None

    def validate(self, host_bytes):
        if any(type(v) is not int or v <= 0 for v in [self.hard_mib, self.swap_mib, self.go_mib, self.gogc]):
            raise DeliveryError('Positive integer resource limits required')
        if host_bytes - self.hard_mib * MIB < 1024 * MIB:
            raise DeliveryError('At least 1 GiB of host RAM must remain outside the application limit')
        if self.swap_mib < self.hard_mib:
            raise DeliveryError('Combined memory + swap limit cannot be below the memory limit')
        if self.go_mib + 128 > self.hard_mib:
            raise DeliveryError('Go memory target needs at least 128 MiB below the hard limit')
        if self.body_mib is not None and (type(self.body_mib) is not int or not 0 < self.body_mib <= self.go_mib // 2):
            raise DeliveryError('Explicit request-body budget must be positive and at most half the Go target')

    def environment(self):
        values = {'GOMEMLIMIT': str(self.go_mib) + 'MiB', 'GOGC': str(self.gogc)}
        if self.body_mib is not None:
            values['GATEWAY_REQUEST_BODY_MEMORY_BUDGET_BYTES'] = str(self.body_mib * MIB)
        return values


def memory_bytes(value):
    if type(value) is int: return value
    match = re.fullmatch(r'(\d+)(b|k|kb|kib|m|mb|mib|g|gb|gib)?', str(value), re.I)
    if not match: raise DeliveryError('Unrecognized Compose memory value')
    suffix = (match[2] or '').lower()
    return int(match[1]) * (1024 ** ({'k': 1, 'm': 2, 'g': 3}.get(suffix[:1], 0)))


def candidate_override(current, original, settings):
    result = copy.deepcopy(original)
    app = result.setdefault('services', {}).setdefault('sub2api', {})
    if not isinstance(app.get('environment', {}), dict):
        raise DeliveryError('Compose must expose environment as a mapping')
    app.setdefault('environment', {}).update(settings.environment())
    app.update(mem_limit=settings.hard_mib * MIB, memswap_limit=settings.swap_mib * MIB)
    limits = current['services']['sub2api'].get('deploy', {}).get('resources', {}).get('limits', {})
    if 'memory' in limits:
        app.setdefault('deploy', {}).setdefault('resources', {}).setdefault('limits', {})['memory'] = settings.hard_mib * MIB
    return result


def assert_allowed_change(before, after, settings):
    old, new = copy.deepcopy(before), copy.deepcopy(after)
    target = new.get('services', {}).get('sub2api', {})
    if (memory_bytes(target.get('mem_limit')) != settings.hard_mib * MIB
            or memory_bytes(target.get('memswap_limit')) != settings.swap_mib * MIB
            or any(target.get('environment', {}).get(k) != v for k, v in settings.environment().items())):
        raise DeliveryError('Candidate does not implement the requested resource limits')
    deploy_memory = target.get('deploy', {}).get('resources', {}).get('limits', {}).get('memory')
    if deploy_memory is not None and memory_bytes(deploy_memory) != settings.hard_mib * MIB:
        raise DeliveryError('Compose memory limits disagree')
    for model in [old, new]:
        app = model['services']['sub2api']
        app.pop('mem_limit', None)
        app.pop('memswap_limit', None)
        for key in settings.environment(): app.setdefault('environment', {}).pop(key, None)
        if not app['environment']: app.pop('environment')
        limits = app.get('deploy', {}).get('resources', {}).get('limits', {})
        limits.pop('memory', None)
        for parent, key in [(app.get('deploy', {}).get('resources', {}), 'limits'),
                            (app.get('deploy', {}), 'resources'), (app, 'deploy')]:
            if key in parent and not parent[key]: parent.pop(key)
    if old != new:
        raise DeliveryError('Candidate changes configuration outside the explicitly allowed resource fields')


def atomic_bytes(path, content, mode=0o600, owner=None):
    path = Path(path)
    descriptor, temporary = tempfile.mkstemp(prefix='.' + path.name + '-', dir=path.parent)
    try:
        os.chmod(temporary, mode)
        if owner is not None and hasattr(os, 'chown'): os.chown(temporary, *owner)
        with os.fdopen(descriptor, 'wb') as stream:
            descriptor = None
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if descriptor is not None: os.close(descriptor)
        Path(temporary).unlink(missing_ok=True)


def private_json(path, value):
    atomic_bytes(path, (json.dumps(value, indent=2, ensure_ascii=True) + '\n').encode())


class ResourceRuntime(DockerRuntime):
    def host_memory_bytes(self):
        text = Path('/proc/meminfo').read_text()
        match = re.search(r'^MemTotal:\s+(\d+)\s+kB$', text, re.M)
        if not match: raise DeliveryError('Cannot verify physical host memory')
        return int(match[1]) * 1024

    def config(self, paths, flags=()):
        command = ['docker', 'compose', '--project-name', self.project]
        for path in paths: command += ['-f', str(path)]
        command += ['config', *flags, '--format', 'json']
        try:
            return json.loads(subprocess.check_output(command, cwd=self.root, text=True,
                                                      stderr=subprocess.PIPE, timeout=30))
        except (subprocess.SubprocessError, ValueError):
            raise DeliveryError('Compose resource configuration could not be verified') from None

    def read_override(self, path):
        # Preserve partial overrides and ${...} references without a YAML dependency.
        return self.config([path], ['--no-consistency', '--no-normalize', '--no-path-resolution',
                                    '--no-env-resolution', '--no-interpolate'])

    def candidate_model(self, path): return self.config([self.root / 'docker-compose.yml', path])

    def effective_model(self): return self.config([])

    def frozen_model(self, path): return self.config([path])


def verify_running(before, after, settings):
    old_host, new_host = copy.deepcopy(before['HostConfig']), copy.deepcopy(after['HostConfig'])
    if new_host.pop('Memory') != settings.hard_mib * MIB or new_host.pop('MemorySwap') != settings.swap_mib * MIB:
        raise DeliveryError('Running Docker limits differ from the reviewed candidate')
    old_host.pop('Memory', None)
    old_host.pop('MemorySwap', None)
    old_env = dict(v.split('=', 1) for v in before['Config']['Env'] if '=' in v)
    new_env = dict(v.split('=', 1) for v in after['Config']['Env'] if '=' in v)
    if any(new_env.get(k) != v for k, v in settings.environment().items()):
        raise DeliveryError('Running Go or body-budget setting differs from the candidate')
    for values in [old_env, new_env]:
        for key in settings.environment(): values.pop(key, None)
    if old_host != new_host or old_env != new_env or before['Image'] != after['Image']:
        raise DeliveryError('Unrelated running container configuration changed')


def change_resources(root, runtime, settings, expected_sha256, *, apply=False, output=None):
    root = Path(root).resolve()
    if not root.is_dir() or root == Path(root.anchor): raise DeliveryError('Existing deployment root required')
    if not re.fullmatch('[0-9a-f]{64}', expected_sha256): raise DeliveryError('Fixed expected program SHA-256 required')
    report = Path(output).resolve() if output is not None else None
    if report is not None and (report.exists() or report.suffix != '.json' or not report.is_relative_to(root)):
        raise DeliveryError('Select a new JSON report path inside the deployment root')
    override = root / 'docker-compose.override.yml'
    if not override.is_file() or override.is_symlink(): raise DeliveryError('Existing regular override file required')
    active = root / 'bin/sub2api'
    if digest(active) != expected_sha256: raise DeliveryError('Running program baseline changed')
    settings.validate(runtime.host_memory_bytes())
    original_bytes = override.read_bytes()
    original_stat = override.stat()
    mode, owner = stat.S_IMODE(original_stat.st_mode), (original_stat.st_uid, original_stat.st_gid)
    preflight = runtime.preflight()
    if preflight.get('modes') != ['shadow', 'shadow']: raise DeliveryError('Codex/Grok must remain shadow')
    runtime.health()
    before_model = copy.deepcopy(runtime.resolved_compose)
    original_info = runtime.inspect(runtime.app)
    partial = candidate_override(before_model, runtime.read_override(override), settings)
    partial_bytes = (json.dumps(partial, indent=2, ensure_ascii=True) + '\n').encode()
    with tempfile.TemporaryDirectory(prefix='mile-resource-plan-') as folder:
        candidate_path = Path(folder) / 'override.json'
        atomic_bytes(candidate_path, partial_bytes)
        candidate = runtime.candidate_model(candidate_path)
    assert_allowed_change(before_model, candidate, settings)
    record = {'status': 'planned', 'started_at': utcnow(), 'baseline_sha256': expected_sha256,
              'host_memory_bytes': runtime.host_memory_bytes(), 'settings': asdict(settings),
              'modes': preflight['modes']}
    if not apply:
        if report is not None:
            report.parent.mkdir(parents=True, exist_ok=True)
            private_json(report, record)
        return record
    with exclusive(root):
        if digest(active) != expected_sha256 or runtime.preflight() != preflight or runtime.resolved_compose != before_model:
            raise DeliveryError('Runtime changed before the resource operation')
        backup = root / 'backups' / ('before-runtime-resources-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ'))
        backup.mkdir(mode=0o700, parents=True)
        record['backup'] = str(backup)
        os.link(active, backup / 'sub2api.previous')
        atomic_bytes(backup / override.name, original_bytes, mode, owner)
        runtime.backup(backup)  # Includes pg_dump and successful pg_restore --list, before stop.
        old_frozen = runtime.frozen_file
        frozen = backup / 'candidate-compose.json'
        private_json(frozen, candidate)
        if runtime.frozen_model(frozen) != candidate: raise DeliveryError('Frozen candidate changes the effective model')
        if (digest(active) != expected_sha256 or override.read_bytes() != original_bytes
                or stat.S_IMODE(override.stat().st_mode) != mode or runtime.preflight() != preflight
                or runtime.resolved_compose != before_model):
            raise DeliveryError('Program or configuration changed during verified backup')
        stopped = installed = False
        try:
            stopped = True
            runtime.stop()
            as_of = utcnow()
            before = runtime.snapshot(as_of)
            private_json(backup / 'before.json', before)
            atomic_bytes(override, partial_bytes, mode, owner)
            installed = True
            if runtime.effective_model() != candidate: raise DeliveryError('Installed override differs from the candidate')
            runtime.frozen_file = frozen
            runtime.start()
            runtime.health()
            after = runtime.snapshot(as_of)
            private_json(backup / 'after.json', after)
            assert_preserved(before, after)
            verify_running(original_info, runtime.inspect(runtime.app), settings)
            post = runtime.preflight()
            if post.get('modes') != ['shadow', 'shadow'] or runtime.resolved_compose != candidate:
                raise DeliveryError('Running Compose or quota modes changed')
            if digest(active) != expected_sha256: raise DeliveryError('Program changed during the resource operation')
            record.update(status='applied', as_of=as_of, business_snapshot_preserved=True,
                          request_rows=len(before['requests']), finished_at=utcnow())
        except BaseException as error:
            record.update(status='failed', error=type(error).__name__, finished_at=utcnow())
            try:
                file_rollback_blocked = False
                if installed:
                    if not override.exists() or override.read_bytes() != partial_bytes:
                        record['override_rollback_blocked'] = file_rollback_blocked = True
                    else:
                        atomic_bytes(override, original_bytes, mode, owner)
                if stopped:
                    runtime.frozen_file = old_frozen
                    runtime.start()
                    runtime.health()
                    record['original_frozen_runtime_restored'] = True
                    record['configuration_rolled_back'] = not file_rollback_blocked
                if file_rollback_blocked:
                    raise DeliveryError('Original runtime restored; external override edits preserved for review') from error
            finally:
                private_json(backup / 'runtime-resources.json', record)
                if report is not None:
                    report.parent.mkdir(parents=True, exist_ok=True)
                    private_json(report, record)
            raise
        private_json(backup / 'runtime-resources.json', record)
        if report is not None:
            report.parent.mkdir(parents=True, exist_ok=True)
            private_json(report, record)
        return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    parser.add_argument('--expected-sha256', required=True)
    for name, default in [('hard-mib', 768), ('swap-mib', 1024), ('go-mib', 512), ('gogc', 50), ('body-mib', None)]:
        parser.add_argument('--' + name, type=int, default=default)
    parser.add_argument('--output', type=Path, help='New sanitized JSON report inside the deployment root')
    parser.add_argument('--apply', action='store_true', help='Explicitly back up and apply; default only plans')
    args = parser.parse_args()
    try:
        settings = Settings(args.hard_mib, args.swap_mib, args.go_mib, args.gogc, args.body_mib)
        runtime = ResourceRuntime(args.root, args.root / 'releases')
        result = change_resources(args.root, runtime, settings, args.expected_sha256, apply=args.apply, output=args.output)
        print(json.dumps(result, indent=2))
    except (DeliveryError, OSError, subprocess.SubprocessError, ValueError) as error:
        detail = str(error) if isinstance(error, DeliveryError) else type(error).__name__
        print('Resource operation failed: ' + detail, file=__import__('sys').stderr)
        return 1
    return 0


if __name__ == '__main__': raise SystemExit(main())
