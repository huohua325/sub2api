"""Keep recent complete deployment backups; dry run unless --apply is explicit."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import tarfile


def prune(root, keep=2, *, apply=False):
    root = Path(root)
    if root.is_symlink() or not root.is_absolute() or root.name != 'backups' or keep < 2:
        raise ValueError('An absolute real backups directory and at least two retained backups are required')
    root = root.resolve(strict=True)
    children = list(root.iterdir())
    if any(p.is_symlink() for p in children):
        raise ValueError('Backup symlinks require manual review')
    complete = sorted((p for p in children if p.is_dir() and (p / 'database.dump').is_file()),
                      key=lambda p: (p / 'database.dump').stat().st_mtime, reverse=True)
    if len(complete) < keep:
        raise ValueError('Not enough complete recent backups to retain')
    retained = complete[:keep]
    for folder in retained:
        if not (folder / 'sub2api.previous').is_file():
            raise ValueError('Recent backup is missing the previous program')
        archive = folder / 'config-and-data.tar.gz'
        if not archive.is_file():
            archive = folder / 'data.tar.gz'
            if not all((folder / name).is_file() for name in ['.env', 'docker-compose.yml']):
                raise ValueError('Recent backup is missing configuration')
        with tarfile.open(archive, 'r:gz') as stream:
            if not stream.getnames():
                raise ValueError('Empty data/config archive')
        with (folder / 'database.dump').open('rb') as stream:
            subprocess.run(['docker', 'exec', '-i', 'sub2api-postgres', 'pg_restore', '--list'],
                           stdin=stream, stdout=subprocess.DEVNULL, check=True, timeout=60)
    cutoff = (retained[-1] / 'database.dump').stat().st_mtime
    targets = []
    for path in children:
        if path in retained:
            continue
        if path.parent.resolve() != root or path.resolve().parent != root:
            raise ValueError('Backup target escaped the reviewed directory')
        if path.is_dir() and path.name.startswith(('before-', 'after-')):
            stamp = (path / 'database.dump').stat().st_mtime if (path / 'database.dump').is_file() else path.stat().st_mtime
            if stamp < cutoff:
                targets.append(path)
        elif path.is_file() and path.suffix == '.dump' and path.stat().st_mtime < cutoff:
            targets.append(path)
    plan = {'root': str(root), 'retained': [str(p) for p in retained],
            'removed' if apply else 'would_remove': [str(p) for p in targets]}
    if apply:
        for path in targets:
            if path.is_dir():
                shutil.rmtree(path)
            else:
                path.unlink()
    return plan


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--keep', type=int, default=2)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    print(json.dumps(prune(args.root, args.keep, apply=args.apply), indent=2))
