"""Content-bound command evidence. Standard library only; no shell interpolation."""
from contextlib import contextmanager
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import threading
import time


class DeliveryError(RuntimeError):
    pass


def utcnow():
    return datetime.now(timezone.utc).isoformat()


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def json_hash(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=True).encode()).hexdigest()


def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + '.tmp')
    temporary.write_text(json.dumps(value, indent=2, ensure_ascii=True) + '\n', encoding='utf-8')
    os.replace(temporary, path)


def contained(root, relative):
    root = Path(root).resolve()
    path = (root / relative).resolve()
    if not path.is_relative_to(root) or path == root:
        raise DeliveryError('Path must stay inside the selected directory: ' + str(relative))
    return path


def tree_hash(root, paths, *, inputs=False):
    """Hash names and bytes, including absent inputs. No mtime-only reuse."""
    root = Path(root).resolve()
    ignored = {'node_modules', '.git', '.ryugi', '.worktrees', '__pycache__', '.vite'}
    result = {}
    for relative in paths:
        path = contained(root, relative)
        if not path.exists():
            if not inputs:
                raise DeliveryError('Missing artifact: ' + str(relative))
            result[str(relative)] = None
            continue
        files = sorted(path.rglob('*')) if path.is_dir() else [path]
        entries = {}
        for file in files:
            rel = file.relative_to(root)
            if inputs and (set(rel.parts) & ignored or file.name.endswith('.tsbuildinfo')):
                continue
            if file.is_symlink():
                raise DeliveryError('Symlink is not an immutable input/artifact: ' + str(rel))
            if file.is_file():
                entries[rel.as_posix()] = digest(file)
        result[str(relative)] = json_hash(entries) if path.is_dir() else entries[path.relative_to(root).as_posix()]
    return result


@contextmanager
def exclusive(directory):
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True)
    lock = directory / '.delivery-lock'
    try:
        lock.mkdir()
    except FileExistsError:
        raise DeliveryError('Another delivery owns ' + str(lock)) from None
    try:
        yield
    finally:
        lock.rmdir()


class Runner:
    def __init__(self, root, output, *, tools=None, env=None):
        self.root = Path(root).resolve()
        self.output = Path(output).resolve()
        self.output.mkdir(parents=True, exist_ok=True)
        self.env = dict(os.environ if env is None else env)
        self.tools = tools or {}
        self.lock = threading.Lock()
        path = self.output / 'manifest.json'
        self.manifest = json.loads(path.read_text()) if path.exists() else {
            'schema': 1, 'created_at': utcnow(), 'root': str(self.root), 'steps': []}
        if self.manifest.get('schema') != 1 or self.manifest.get('root') != str(self.root):
            raise DeliveryError('Manifest belongs to a different checkout or format')

    def run(self, name, argv, inputs, *, artifacts=(), cwd='.', cache=True, timeout=1200):
        if not re.fullmatch(r'[a-zA-Z0-9_-]+', name):
            raise DeliveryError('Invalid step name')
        argv = [str(arg) for arg in argv]
        work = self.root if cwd == '.' else contained(self.root, cwd)
        executable = shutil.which(argv[0], path=self.env.get('PATH'))
        if not executable:
            raise DeliveryError('Executable not found: ' + argv[0])
        before = tree_hash(self.root, inputs, inputs=True)
        identity = {'inputs': before, 'argv': argv, 'cwd': str(work), 'tools': self.tools,
                    'executable': digest(executable), 'environment_hash': json_hash(self.env)}
        key = json_hash(identity)
        with self.lock:
            previous = next((s for s in reversed(self.manifest['steps']) if s['name'] == name), None)
        record = {'name': name, 'key': key, 'inputs': before, 'command': argv,
                  'cwd': str(work), 'tools': self.tools, 'started_at': utcnow()}
        if cache and previous and previous['status'] in ('passed', 'reused') and previous['key'] == key:
            try:
                intact = tree_hash(self.root, artifacts) == previous['artifacts']
                intact = intact and digest(contained(self.output, previous['log'])) == previous['log_sha256']
            except (OSError, DeliveryError):
                intact = False
            if intact:
                record.update(status='reused', elapsed_seconds=0, exit_code=0,
                              artifacts=previous['artifacts'], log=previous['log'],
                              log_sha256=previous['log_sha256'], reused_from=previous['started_at'])
                return self._record(record)
        log = self.output / f'{name}-{time.time_ns()}.log'
        record['log'] = log.name
        start = time.monotonic()
        error = None
        try:
            with log.open('wb') as stream:
                process = subprocess.run(argv, cwd=work, env=self.env, stdout=stream,
                                         stderr=subprocess.STDOUT, timeout=timeout, check=False)
            record['exit_code'] = process.returncode
            if process.returncode:
                raise DeliveryError(f'{name} exited {process.returncode}; see {log}')
            if tree_hash(self.root, inputs, inputs=True) != before:
                raise DeliveryError(name + ': inputs changed while command was running')
            record['artifacts'] = tree_hash(self.root, artifacts)
            record['status'] = 'passed'
        except (OSError, subprocess.TimeoutExpired, DeliveryError) as exc:
            error = DeliveryError(f'{name}: {type(exc).__name__}: {exc}')
            record.update(status='failed', exit_code=record.get('exit_code', -1), error=str(error))
        record.update(elapsed_seconds=round(time.monotonic() - start, 3), log_sha256=digest(log))
        self._record(record)
        if error:
            raise error
        return record

    def _record(self, record):
        record['finished_at'] = utcnow()
        with self.lock:
            self.manifest['steps'].append(record)
            self.manifest['updated_at'] = utcnow()
            write_json(self.output / 'manifest.json', self.manifest)
        print(f"{record['name']}: {record['status']} ({record['elapsed_seconds']}s)", flush=True)
        return record
