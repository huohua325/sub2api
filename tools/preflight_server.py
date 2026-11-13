"""Read-only preparation, with temporary private configuration and no service stop."""
from pathlib import Path
import shutil
import tempfile

from delivery_core import DeliveryError, digest, utcnow
from release_server import DockerRuntime


def check(root, expected_sha256=None):
    root = Path(root)
    if not root.is_absolute() or not root.is_dir():
        raise DeliveryError('Existing absolute deployment root required')
    baseline = digest(root / 'bin/sub2api')
    if expected_sha256 is not None and baseline != expected_sha256:
        raise DeliveryError('Running baseline changed; refresh deployment metadata before building')
    runtime = DockerRuntime(root, root / 'releases')
    result = runtime.preflight()
    if result['modes'] != ['shadow', 'shadow']:
        raise DeliveryError('Codex/Grok must remain shadow')
    runtime.check_health('http://127.0.0.1:8080/health')
    with tempfile.TemporaryDirectory(prefix='mile-readonly-preflight-') as folder:
        runtime.verify_frozen_configuration(Path(folder))
    return {'ok': True, 'checked_at': utcnow(), 'root': str(root), 'baseline_sha256': baseline,
            'free_bytes': shutil.disk_usage(root).free, 'frozen_compose_verified': True,
            'app_healthy': True, 'memslides_healthy': True, **result}
