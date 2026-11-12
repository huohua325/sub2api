"""Single-server program replacement. Defaults to preflight; never restores a database."""
import argparse
import gzip
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time
import urllib.request

from delivery_core import DeliveryError, contained, digest, exclusive, json_hash, utcnow, write_json


def stage_release(root, release):
    root = Path(root).resolve(); release = Path(release).resolve()
    if not release.is_relative_to(root / 'releases') or release == root / 'releases':
        raise DeliveryError('Release must be a child of releases/')
    manifest = json.loads((release / 'manifest.json').read_text())
    if digest(root / 'bin/sub2api') != manifest['baseline_sha256']:
        raise DeliveryError('Live baseline changed before reconstruction')
    payload = contained(release, manifest['payload'])
    if digest(payload) != manifest['payload_sha256']:
        raise DeliveryError('Uploaded payload checksum mismatch')
    target = release / 'sub2api'
    if target.exists():
        if digest(target) == manifest['sha256']: return
        raise DeliveryError('Candidate exists with a different checksum')
    if manifest['payload'] == 'app.bsdiff':
        subprocess.run([sys.executable, str(Path(__file__).with_name('apply_delta.py')),
                        str(root / 'bin/sub2api'), str(payload), str(target),
                        manifest['baseline_sha256'], manifest['sha256']], check=True, timeout=180)
    elif manifest['payload'] == 'sub2api.gz':
        temporary = release / 'sub2api.staged'
        try:
            with gzip.open(payload, 'rb') as src, temporary.open('xb') as out:
                while chunk := src.read(1024 * 1024):
                    out.write(chunk)
                    if out.tell() > manifest['bytes']: raise DeliveryError('Decompressed program exceeds expected size')
            if digest(temporary) != manifest['sha256']: raise DeliveryError('Reconstructed checksum mismatch')
            os.replace(temporary, target)
        finally:
            temporary.unlink(missing_ok=True)
    else:
        raise DeliveryError('Unsupported payload')


def assert_preserved(before, after):
    for key in ['as_of', 'clocks', 'migrations']:
        if before[key] != after[key]:
            raise DeliveryError(key + ' changed at the fixed reference time')
    for name, rows in before['immutable'].items():
        if not {json_hash(row) for row in rows}.issubset({json_hash(row) for row in after['immutable'].get(name, [])}):
            raise DeliveryError('Persistent audit/wallet data changed: ' + name)
    for name, ids in before['durable_ids'].items():
        if not set(ids).issubset(after['durable_ids'].get(name, [])):
            raise DeliveryError('Persistent tasks disappeared: ' + name)
    requests = {row['id']: row for row in after['requests']}
    for row in before['requests']:
        if row['id'] not in requests:
            raise DeliveryError('Original request disappeared')
        if row['state'] in ('settled', 'imported') and requests[row['id']] != row:
            raise DeliveryError('Original settled/imported charge changed')


def deploy(root, release, runtime, *, apply=False, upgrade=None):
    root = Path(root).resolve(); release = Path(release).resolve()
    if root == Path(root.anchor) or not release.is_relative_to(root / 'releases') or release == root / 'releases':
        raise DeliveryError('Release must be a child of the deployment releases directory')
    active = contained(root, 'bin/sub2api'); candidate = contained(release, 'sub2api')
    manifest = json.loads((release / 'manifest.json').read_text())
    quota = manifest.get('quota_tests', {})
    if manifest.get('schema') != 1 or not quota.get('passed') or quota.get('skipped') != 0 or quota.get('failed') != 0:
        raise DeliveryError('Frontend-only release and real zero-skip quota proof required')
    if upgrade is None:
        if not manifest.get('backend_unchanged'):
            raise DeliveryError('Backend changes require the explicit full upgrade entry point')
    else:
        upgrade.validate(manifest)
    if not re.fullmatch(r'[0-9a-f]{40}', manifest.get('source_commit', '')):
        raise DeliveryError('Fixed source commit required')
    if digest(active) != manifest['baseline_sha256'] or digest(candidate) != manifest['sha256']:
        raise DeliveryError('Live baseline or uploaded candidate checksum changed')
    if active.samefile(candidate):
        raise DeliveryError('Candidate must be a separate immutable file')
    preflight = runtime.preflight()
    if preflight.get('modes') != ['shadow', 'shadow']:
        raise DeliveryError('Codex/Grok must remain shadow')
    if not apply:
        return {'status': 'planned', 'version': manifest['version'], 'sha256': manifest['sha256'], **preflight}
    with exclusive(root):
        # Fail instead of deleting any previous release or backup to make space.
        backup = root / 'backups' / ('before-' + release.name + '-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ'))
        backup.mkdir(parents=True, mode=0o700)
        os.link(active, backup / 'sub2api.previous')
        runtime.backup(backup)  # dump, verify archive and config/data before stopping traffic
        if digest(active) != manifest['baseline_sha256']:
            raise DeliveryError('Another release changed the live baseline during backup')
        if digest(candidate) != manifest['sha256']:
            raise DeliveryError('Candidate changed during backup')
        if runtime.preflight() != preflight:
            raise DeliveryError('Runtime configuration changed during backup')
        stopped = switched = False
        record = {'version': manifest['version'], 'source_commit': manifest['source_commit'],
                  'sha256': manifest['sha256'], 'backup': str(backup), 'started_at': utcnow()}
        def replace(source):
            temporary = root / 'bin' / ('sub2api.next-' + str(os.getpid()))
            os.link(source, temporary)
            try:
                os.replace(temporary, active)
            finally:
                temporary.unlink(missing_ok=True)
        try:
            stopped = True
            runtime.stop()
            as_of = utcnow()
            before = runtime.snapshot(as_of)
            write_json(backup / 'before.json', before)
            os.chmod(candidate, 0o755)
            replace(candidate); switched = True
            runtime.start(); runtime.health()
            after = runtime.snapshot(as_of)
            write_json(backup / 'after.json', after)
            if upgrade is None:
                assert_preserved(before, after)
            else:
                upgrade.assert_preserved(before, after)
            if runtime.preflight() != preflight:
                raise DeliveryError('Runtime configuration changed during deployment')
            record.update(status='deployed', as_of=as_of, finished_at=utcnow())
            write_json(release / 'deployment.json', record)
            return record
        except BaseException as error:
            record.update(status='failed', error=type(error).__name__, finished_at=utcnow())
            try:
                if switched:
                    if upgrade is not None and not upgrade.can_rollback(runtime):
                        record['program_rollback_blocked'] = 'New personal resets exist; old program cannot interpret them. Keep the database and fix forward.'
                        raise DeliveryError(record['program_rollback_blocked']) from error
                    replace(backup / 'sub2api.previous')
                    runtime.start(); runtime.health()
                    record['program_rolled_back'] = True
                elif stopped:
                    runtime.start(); runtime.health()
            finally:
                write_json(release / 'deployment.json', record)
            raise


def verify_unchanged_compose_plan(plan, app):
    # Compose 2.40 can give config --hash a different model hash from up's
    # container label even when up's actual change detector reports no changes.
    lines = [line.strip() for line in plan.splitlines() if line.strip()]
    pattern = r'(?:DRY-RUN MODE -\s*)?Container\s+' + re.escape(app) + r'\s+Running'
    if len(lines) != 1 or not re.fullmatch(pattern, lines[0]):
        raise DeliveryError('Effective Compose configuration would change the running container')


class DockerRuntime:
    def __init__(self, root, release, app='sub2api', database='sub2api-postgres'):
        self.root = Path(root); self.release = Path(release); self.app = app; self.database = database
        self.budget_checked = False
        info = self.inspect(database)
        self.db_env = dict(value.split('=', 1) for value in info['Config']['Env'] if '=' in value)
    def command(self, argv, **kwargs):
        return subprocess.run(argv, cwd=self.root, check=True, timeout=kwargs.pop('timeout', 180), **kwargs)
    def inspect(self, name):
        return json.loads(subprocess.check_output(['docker', 'inspect', name], timeout=20))[0]
    def preflight(self):
        info = self.inspect(self.app)
        env = dict(value.split('=', 1) for value in info['Config']['Env'] if '=' in value)
        labels = info['Config'].get('Labels') or {}
        configured_hash = subprocess.check_output(['docker', 'compose', 'config', '--hash', self.app],
                                                  cwd=self.root, timeout=20, text=True).strip().split()
        if len(configured_hash) != 2 or configured_hash[0] != self.app:
            raise DeliveryError('Effective Compose configuration differs from the running container')
        if configured_hash[1] != labels.get('com.docker.compose.config-hash'):
            plan = subprocess.check_output(['docker', 'compose', '--dry-run', 'up', '-d', '--no-deps',
                                            '--no-build', '--pull', 'never', self.app], cwd=self.root,
                                           stderr=subprocess.STDOUT, timeout=30, text=True)
            verify_unchanged_compose_plan(plan, self.app)
        self.configuration_hash = configured_hash[1]
        self.project = labels.get('com.docker.compose.project')
        if not self.project: raise DeliveryError('Running container is not managed by the expected Compose project')
        self.resolved_compose = json.loads(subprocess.check_output(['docker', 'compose', 'config', '--format', 'json'],
                                                                  cwd=self.root, timeout=20, text=True))
        self.destination = next((m['Destination'] for m in info['Mounts'] if m['Source'] == str(self.root / 'bin/sub2api')), None)
        if not self.destination: raise DeliveryError('Active program bind mount not found')
        self.check_health('http://127.0.0.1:8790/healthz')
        pg = ['docker', 'exec', self.database, 'psql', '-X', '-qAt', '-U', self.db_env['POSTGRES_USER'],
              '-d', self.db_env['POSTGRES_DB'], '-c', 'SELECT pg_database_size(current_database())']
        database_bytes = int(subprocess.check_output(pg, timeout=20).strip())
        data_bytes = sum(p.stat().st_size for p in (self.root / 'data').rglob('*') if p.is_file())
        required = 128 * 1024 * 1024 + (0 if self.budget_checked else 2 * (database_bytes + data_bytes))
        if shutil.disk_usage(self.root).free < required:
            raise DeliveryError('Insufficient backup headroom; existing backups will not be deleted')
        self.budget_checked = True
        self.command(['docker', 'compose', 'config', '--quiet'], timeout=20)
        return {'environment_hash': json_hash(env), 'compose_sha256': digest(self.root / 'docker-compose.yml'),
                'env_file_sha256': digest(self.root / '.env'),
                'modes': [env.get('SUB2API_CODING_QUOTA_' + purpose + '_MODE', 'shadow') for purpose in ['OPENAI', 'GROK']]}
    def backup(self, folder):
        # Recovery uses the exact resolved configuration verified against the original container.
        self.frozen_file = folder / 'frozen-compose.json'
        write_json(self.frozen_file, self.resolved_compose)
        os.chmod(self.frozen_file, 0o600)
        frozen_hash = subprocess.check_output(self.compose() + ['config', '--hash', self.app],
                                             cwd=self.root, text=True, timeout=20).strip().split()
        if frozen_hash != [self.app, self.configuration_hash]:
            raise DeliveryError('Frozen configuration does not reproduce the running configuration')
        for name in ['docker-compose.yml', '.env']:
            shutil.copy2(self.root / name, folder / name)
        db = ['-U', self.db_env['POSTGRES_USER'], '-d', self.db_env['POSTGRES_DB']]
        with (folder / 'database.dump').open('wb') as out:
            self.command(['docker', 'exec', self.database, 'pg_dump', *db, '-Fc'], stdout=out)
        if not (folder / 'database.dump').stat().st_size: raise DeliveryError('Empty database backup')
        with (folder / 'database.dump').open('rb') as src, (folder / 'database-contents.txt').open('wb') as out:
            self.command(['docker', 'exec', '-i', self.database, 'pg_restore', '--list'], stdin=src, stdout=out)
        self.command(['tar', '-czf', str(folder / 'data.tar.gz'), 'data'])
    def compose(self):
        return ['docker', 'compose', '-f', str(self.frozen_file), '--project-name', self.project]
    def stop(self): self.command(self.compose() + ['stop', '-t', '120', self.app], timeout=150)
    def start(self): self.command(self.compose() + ['up', '-d', '--no-deps', '--no-build', '--pull', 'never', '--force-recreate', self.app])
    @staticmethod
    def check_health(url):
        with urllib.request.urlopen(url, timeout=3) as response:
            if response.status != 200: raise DeliveryError('Health endpoint failed')
    def health(self):
        deadline = time.monotonic() + 70
        while True:
            try:
                self.check_health('http://127.0.0.1:8080/health')
                self.check_health('http://127.0.0.1:8790/healthz')
                return
            except (OSError, DeliveryError):
                if time.monotonic() >= deadline: raise DeliveryError('Program or protected service unhealthy')
                time.sleep(1)
    def snapshot(self, as_of):
        from quota_snapshot import snapshot
        return snapshot(self.database, self.db_env, as_of)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    parser.add_argument('--release', type=Path, required=True)
    parser.add_argument('--stage', action='store_true', help='verify/reconstruct payload without replacing the running program')
    parser.add_argument('--apply', action='store_true', help='perform the reviewed deployment; otherwise preflight only')
    args = parser.parse_args()
    try:
        if args.stage:
            stage_release(args.root, args.release)
            print('Candidate reconstructed and verified; running program unchanged')
            return 0
        result = deploy(args.root, args.release, DockerRuntime(args.root, args.release), apply=args.apply)
        print(json.dumps(result, indent=2)); return 0
    except (DeliveryError, OSError, ValueError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr); return 1


if __name__ == '__main__': raise SystemExit(main())
