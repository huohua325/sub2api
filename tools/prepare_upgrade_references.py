#!/usr/bin/env python3
"""Download pinned reference sources; never build or execute upstream code."""

from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
import hashlib
import json
from pathlib import Path, PurePosixPath
import shutil
import tempfile
import urllib.request
import zipfile
import zlib


ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / "docs/engineering-upgrade/references.lock.json"


def archive_members(archive: zipfile.ZipFile):
    """Remove GitHub's top-level directory and reject unsafe member paths."""
    roots = set()
    for entry in archive.infolist():
        path = PurePosixPath(entry.filename)
        if path.is_absolute() or ".." in path.parts or "\\" in entry.filename:
            raise ValueError(f"Unsafe archive member: {entry.filename}")
        roots.add(path.parts[0])
        if len(roots) > 1:
            raise ValueError("Expected one GitHub archive root")
        if entry.is_dir() or len(path.parts) < 2:
            continue
        relative = Path(*path.parts[1:])
        if any(":" in part for part in relative.parts):
            raise ValueError(f"Unsupported archive path: {entry.filename}")
        yield entry, relative


def digest(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def verify_tree(archive: zipfile.ZipFile, destination: Path) -> int:
    count = 0
    for entry, relative in archive_members(archive):
        path = destination / relative
        if not path.is_file() or path.stat().st_size != entry.file_size:
            raise ValueError(f"Missing or changed reference: {path}")
        crc = 0
        with path.open("rb") as stream:
            while chunk := stream.read(1024 * 1024):
                crc = zlib.crc32(chunk, crc)
        if crc != entry.CRC:
            raise ValueError(f"Changed reference: {path}")
        count += 1
    return count


def prepare(project: dict, base: Path, verify_only: bool) -> dict:
    repo = project["repo"]
    commit = project["commit"]
    directory = project["directory"]
    if directory != repo.replace("/", "--") or len(commit) != 40:
        raise ValueError(f"Invalid lock entry: {repo}")
    if any(character not in "0123456789abcdef" for character in commit):
        raise ValueError(f"Invalid commit: {repo}")
    url = f"https://codeload.github.com/{repo}/zip/{commit}"
    if project["archive_url"] != url:
        raise ValueError(f"Archive URL does not match pinned commit: {repo}")
    archive_path = base / "archives" / f"{directory}--{commit}.zip"
    destination = base / "projects" / directory
    if not archive_path.exists():
        if verify_only:
            raise ValueError(f"Archive not downloaded: {repo}")
        archive_path.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile(dir=archive_path.parent, delete=False) as stream:
            temporary = Path(stream.name)
            try:
                request = urllib.request.Request(url, headers={"User-Agent": "sub2api-upgrade-references"})
                with urllib.request.urlopen(request, timeout=120) as response:
                    shutil.copyfileobj(response, stream)
            except BaseException:
                stream.close()
                temporary.unlink(missing_ok=True)
                raise
        temporary.replace(archive_path)
    checksum = digest(archive_path)
    expected = project.get("archive_sha256")
    if expected and checksum != expected:
        raise ValueError(f"Archive checksum mismatch: {repo}")
    with zipfile.ZipFile(archive_path) as archive:
        if not destination.exists():
            if verify_only:
                raise ValueError(f"Sources not extracted: {repo}")
            destination.parent.mkdir(parents=True, exist_ok=True)
            with tempfile.TemporaryDirectory(dir=destination.parent) as temporary:
                staging = Path(temporary) / "source"
                staging.mkdir()
                for entry, relative in archive_members(archive):
                    target = staging / relative
                    target.parent.mkdir(parents=True, exist_ok=True)
                    with archive.open(entry) as source, target.open("wb") as output:
                        shutil.copyfileobj(source, output)
                staging.replace(destination)
        count = verify_tree(archive, destination)
    return {"repo": repo, "archive_sha256": checksum, "archive_bytes": archive_path.stat().st_size, "source_files": count}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verify", action="store_true", help="Verify downloaded archives and every extracted source file without downloading")
    parser.add_argument("--record-checksums", action="store_true", help="Record checksums after every project succeeds; existing checksums must still match")
    args = parser.parse_args()
    lock = json.loads(LOCK.read_text(encoding="utf-8"))
    base = (ROOT / lock["source_directory"]).resolve()
    if not base.is_relative_to(ROOT / "docs-local"):
        parser.error("Reference sources must stay inside docs-local")
    results = {}
    errors = []
    with ThreadPoolExecutor(max_workers=3) as pool:
        pending = {pool.submit(prepare, project, base, args.verify): project["repo"] for project in lock["projects"]}
        for future in as_completed(pending):
            repo = pending[future]
            try:
                result = future.result()
                results[repo] = result
                print(f"OK {repo}: {result['source_files']} files, {result['archive_bytes'] / 1024 / 1024:.1f} MiB", flush=True)
            except Exception as error:
                errors.append(repo)
                print(f"ERROR {repo}: {error}", flush=True)
    if errors:
        return 1
    if args.record_checksums:
        for project in lock["projects"]:
            project.update({key: value for key, value in results[project["repo"]].items() if key != "repo"})
        LOCK.write_text(json.dumps(lock, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Verified {len(results)} pinned reference projects.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
