"""Package and stage the reviewed migration-free native T3 workspace release."""
import argparse
import json
from pathlib import Path
import re
import subprocess

from delivery_core import DeliveryError
from release_server import deploy, stage_release
from upstream_v0225_upgrade import UpstreamV0225Runtime, UpstreamV0225Upgrade

BASELINE = '189f3c233116e8aca713ef0e6b6d3554a9c9d326'
BASELINE_SHA256 = '35cc2f9da82b3c492d107c1078d66d831b44119eb4089cb17a81cc764ada3400'
CONTRACT = 't3-workspace-no-migrations-v1'
FEATURE = 't3-workspace'
ADDED = {
    'backend/internal/handler/admin/development_workspace_handler.go',
    'backend/internal/pkg/developmentworkspace/client.go',
    'backend/internal/pkg/developmentworkspace/client_test.go',
    'backend/internal/pkg/developmentworkspace/mcp.go',
    'backend/internal/pkg/t3gateway/gateway.go',
    'backend/internal/pkg/t3gateway/gateway_test.go',
    'backend/internal/pkg/t3gateway/unix_test.go',
    'backend/internal/server/routes/development_workspace.go',
    'backend/internal/server/routes/development_workspace_test.go',
    'backend/internal/server/t3_native.go',
    'backend/internal/server/t3_native_test.go',
    'backend/internal/service/t3_browser_session.go',
    'backend/internal/service/t3_browser_session_test.go',
    'frontend/src/api/admin/developmentWorkspace.ts',
    'frontend/src/i18n/locales/en/admin/developmentWorkspace.ts',
    'frontend/src/i18n/locales/zh/admin/developmentWorkspace.ts',
    'frontend/src/router/__tests__/nativeWorkspace.spec.ts',
    'frontend/src/router/nativeWorkspace.ts',
    'frontend/src/views/admin/DevelopmentWorkspaceView.vue',
    'frontend/src/views/admin/developmentWorkspace/WorkspaceTranscript.vue',
    'frontend/src/views/admin/developmentWorkspace/useWorkspaceTasks.ts',
    'frontend/src/views/admin/developmentWorkspace/workspaceAttempts.spec.ts',
    'frontend/src/views/admin/developmentWorkspace/workspaceAttempts.ts',
}
MODIFIED = {
    'backend/cmd/server/wire_gen.go',
    'backend/internal/server/http.go',
    'backend/internal/server/middleware/audit_log.go',
    'backend/internal/server/middleware/security_headers.go',
    'backend/internal/server/router.go',
    'backend/internal/server/routes/admin.go',
    'frontend/src/components/layout/AppHeader.vue',
    'frontend/src/components/layout/AppLayout.vue',
    'frontend/src/components/layout/AppSidebar.vue',
    'frontend/src/i18n/locales/en/admin/index.ts',
    'frontend/src/i18n/locales/zh/admin/index.ts',
    'frontend/src/router/index.ts',
    'frontend/src/views/auth/LoginView.vue',
    'frontend/vite.config.ts',
}


def contract_for_release(source):
    if not isinstance(source, str) or not re.fullmatch(r'[0-9a-f]{40}', source):
        raise DeliveryError('Fixed reviewed T3 workspace commit required')
    return dict(name=CONTRACT, baseline_commit=BASELINE, source_commit=source,
                migrations={}, database_migrations={})


class T3WorkspaceUpgrade(UpstreamV0225Upgrade):
    baseline = BASELINE

    def __init__(self, contract):
        if contract != contract_for_release(contract.get('source_commit', '')):
            raise DeliveryError('Explicit migration-free T3 workspace manifest required')
        self.contract = contract

    def validate_sources(self, root, baseline, source):
        def git(*args):
            try:
                return subprocess.check_output(['git', *args], cwd=root, text=True,
                                               stderr=subprocess.PIPE, timeout=30).strip()
            except subprocess.CalledProcessError:
                raise DeliveryError('T3 workspace source ancestry verification failed') from None

        if baseline != BASELINE:
            raise DeliveryError('Unexpected live T3 workspace baseline')
        reviewed = self.contract['source_commit']
        git('merge-base', '--is-ancestor', BASELINE, reviewed)
        git('merge-base', '--is-ancestor', reviewed, source)
        if git('diff', '--name-only', BASELINE, source, '--', 'backend/migrations'):
            raise DeliveryError('T3 workspace release must preserve every deployed migration')
        if git('diff', '--name-only', reviewed, source, '--', 'backend', 'frontend'):
            raise DeliveryError('Application changed after the reviewed T3 workspace commit')
        changes = set(git('diff', '--name-status', '--no-renames', BASELINE, reviewed,
                          '--', 'backend', 'frontend').splitlines())
        expected = {'A\t' + path for path in ADDED} | {'M\t' + path for path in MODIFIED}
        if changes != expected:
            extra = sorted(changes - expected)
            missing = sorted(expected - changes)
            raise DeliveryError(f'T3 workspace application scope differs from review (extra={extra}, missing={missing})')

    def validate(self, manifest):
        super().validate(manifest)
        if (manifest.get('baseline_sha256') != BASELINE_SHA256
                or manifest.get('source_commit') != self.contract['source_commit']):
            raise DeliveryError('Expected the pinned T3 baseline binary and reviewed application commit')

    def can_rollback(self, runtime):
        # The live baseline already supports the same migrations and pricing.
        return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    package = commands.add_parser('package')
    for name in ('output', 'destination', 'baseline-binary'):
        package.add_argument('--' + name, type=Path, required=True)
    package.add_argument('--version', required=True)
    package.add_argument('--source-commit', required=True)
    server = commands.add_parser('deploy')
    server.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    server.add_argument('--release', type=Path, required=True)
    actions = server.add_mutually_exclusive_group()
    actions.add_argument('--stage', action='store_true')
    actions.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    if args.command == 'package':
        from release_package import package_release
        result = package_release(Path(__file__).resolve().parents[1], args.output, args.destination,
                                 args.output / 'sub2api-linux-amd64', args.version, BASELINE_SHA256,
                                 BASELINE, baseline_binary=args.baseline_binary,
                                 upgrade_contract=contract_for_release(args.source_commit))
    else:
        manifest = json.loads((args.release / 'manifest.json').read_text(encoding='utf-8'))
        policy = T3WorkspaceUpgrade(manifest.get('upgrade_contract', {}))
        policy.validate(manifest)
        if args.stage:
            stage_release(args.root, args.release)
            return
        result = deploy(args.root, args.release, UpstreamV0225Runtime(args.root, args.release),
                        apply=args.apply, upgrade=policy)
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
