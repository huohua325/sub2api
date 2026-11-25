# Native T3 workspace deployment

The native workspace opens from the MILE administrator sidebar. A dedicated HTTPS
origin serves the original T3 interface. The MILE backend grants browser access
and proxies the page, API, assets and WebSocket connections to the runner.

## Linux runner

1. Provision a Linux runner with the system account `t3workspace` and home
   `/var/lib/t3workspace`. Install T3 Code `0.1.11` from the official release.
   Verify its archive against the release SHA256, then install the complete
   runtime under `/opt/t3code/0.1.11` with root ownership. Confirm
   `/opt/t3code/0.1.11/t3 --version` before enabling the service.
2. As `t3workspace`, clone the authorized fork into
   `/var/lib/t3workspace/project`. Configure a repository-scoped identity with
   permission to create branches, commit and push. Keep worktrees, tool caches
   and development credentials under this account's home.
3. Install the required provider CLIs on the service's `PATH`. Sign in as
   `t3workspace` or configure the provider credentials for that account.
   Codex, Claude Code, Grok Build, OpenCode, Cursor and supported ACP agents
   each require their own runtime and valid provider credentials. For provider
   entries that need launch settings, use the native T3 provider controls.
   Keep any service-provided variables in the protected workspace environment file.
4. Install [the systemd unit](../../deploy/t3code-workspace.service) in
   `/etc/systemd/system`. Install [the managed policy](../../deploy/t3code-policy.example.json)
   as `/etc/t3code/policy.json` and the optional
   [workspace environment](../../deploy/t3code-workspace.env.example) as
   `/etc/t3code/workspace.env`. Set both files to root ownership and mode `0600`
   for the environment file. Run `systemctl daemon-reload` and
   `systemctl enable --now t3code-workspace.service`. Check that T3 listens on
   `127.0.0.1:3773`. The policy enables Full access for new threads in the
   development account and turns off T3 Connect and public preview links.

A separate release identity owns deployment credentials. An Agent can run the
project's normal checks, build candidates, push to authorized branches and submit
an immutable release candidate. Grant the Agent staging deployment access
through a staging-specific identity if that environment is available. The existing
release identity may apply a production candidate after its commit, artifact hash,
baseline and required evidence have been verified. A release job can then execute
the fixed `stage`, preflight and `apply` sequence unattended. Give the development
account only the release submission permission. Keep the production SSH key,
database credentials, Docker socket and privilege escalation with the release
identity. See [the delivery workflow](../../tools/DELIVERY.md).

## Private connection and pairing

### Same MileLab host

The current MileLab host has one logical CPU, approximately 2 GiB of RAM,
cgroup v2, systemd 255 and `systemd-socket-proxyd`. Its Sub2API container uses
a bridge network and maps the web port to host loopback. The runner uses the
separate `t3workspace` system account with its own state and credentials.
Install the [colocated limits](../../deploy/t3code-workspace-colocated.conf) to
`/etc/systemd/system/t3code-workspace.service.d/limits.conf`. The cgroup caps
CPU at 25%, memory at 768 MiB with a 512 MiB reclaim threshold, and excludes the
runner from host swap. Begin with one light task at a time. Use the fork's CI
for full builds and release artifact generation. The drop-in also blocks
access to production mounts and the production container bridge. Update the
network CIDR and production paths alongside any later infrastructure change.

1. Create the system group `mile-t3-proxy`. Install the
   [private relay socket](../../deploy/mile-t3-gateway.socket) and its
   [relay service](../../deploy/mile-t3-gateway.service) under
   `/etc/systemd/system`. Reload systemd and enable the socket with
   `systemctl enable --now mile-t3-gateway.socket`.
2. Store the pairing issuer token on the host in a restricted directory,
   readable by the app container. Resolve the numerical GID of `mile-t3-proxy`
   on this host. Set that GID and the token path as
   `DEV_T3_NATIVE_SOCKET_GID` and `DEV_T3_NATIVE_PAIR_TOKEN_HOST_FILE` when
   preparing the [Compose overlay](../../deploy/docker-compose.t3-native.example.yml).
   Start the socket unit before creating the app container so its directory
   exists. The app mounts the directory read-only to follow socket recreation
   after a relay restart. The development account has no membership in the
   relay group and the runner TCP listener stays on host loopback.
3. Review the complete Compose configuration, the token's mode and the
   container's socket access before switching the service. The existing
   release preflight requires verified configuration and an approved baseline.

### Separate runner host

Use an authenticated, encrypted private transport from MILE to the runner
loopback listener. For example, a private OpenSSH tunnel can forward port
`43773` in the backend network namespace to the runner's `127.0.0.1:3773`.
Pin the runner host key and use a dedicated SSH identity constrained to this
forwarding destination. Set `DEV_T3_NATIVE_UPSTREAM_URL=http://127.0.0.1:43773`
and omit the Unix socket setting. Keep the native listener and private
transport endpoint off public DNS and ports.

### Pairing issuer

From the runner, issue the pairing issuer credential using the same account and
state directory as the service. Store and transfer the token as a secret.

```sh
umask 077
/opt/t3code/0.1.11/t3 auth session issue \
  --base-dir /var/lib/t3workspace/.t3 \
  --label "MILE native pairing issuer" --ttl 30d --token-only \
  --scope access:write \
  --scope orchestration:read --scope orchestration:operate \
  --scope settings:write --scope providers:manage \
  --scope preview:operate --scope diagnostics:read \
  --scope terminal:read --scope terminal:operate \
  --scope source-control:write --scope filesystem:read --scope filesystem:write \
  > /var/lib/t3workspace/mile-pair-token
```

Install the token as a read-only secret for the MILE service. Browser pair grants
carry project, file, terminal and provider scopes. The pairing issuer also has
`access:write`, which remains server-side. Rotate the issuer before expiry and
revoke the old session after checking a new browser login.

## MILE gateway and edge

1. Configure the [native gateway environment](../../deploy/t3code-native-gateway.env.example)
   on the backend. Apply it through the deployment configuration procedure so
   Docker mounts and secret paths match the service's effective configuration.
2. Assign a dedicated public HTTPS origin to the workspace. Adapt
   [the Caddy site example](../../deploy/t3code-workspace.Caddyfile.example)
   for the existing edge, using the MILE backend as upstream and preserving the
   original Host. Route the entire workspace origin through this handler so the
   native page, API and WebSocket share the MILE access check.
3. Verify the staged backend and authenticated tunnel before publishing the edge
   route. Confirm provider readiness inside T3 with small tasks for every claimed
   provider, then exercise the existing verified artifact deployment channel.

## Acceptance

- From an external network, visit the workspace origin directly as a guest.
  Repeat with a regular MILE account and issue a WebSocket handshake.
- Log in as a MILE administrator, open a native T3 thread, then test a later
  request and an existing WebSocket after MILE logout or administrator
  permission revocation. Retry an already-used browser launch ticket.
- Launch the same development-only task from phone and desktop browser
  viewports. Run a shell command, edit a file, run the test, commit to a branch,
  and verify that each configured provider can complete a task unattended.
- Exercise staging release submission and confirm the release worker accepts
  only the prepared artifact for the approved commit and deployment baseline.

Server preparation requires a workspace HTTPS hostname, authorized provider
logins, the dedicated release identity and a reachable runner through the
selected private transport. Availability and successful task execution depend
on the configured host and credentials.
