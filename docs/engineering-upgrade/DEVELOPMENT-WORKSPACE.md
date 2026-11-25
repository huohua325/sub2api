# Development workspace

The administrator workspace is available at `/admin/development-workspace`. It uses
the existing MILE browser session. T3 Code stores the threads and runs Codex in
worktrees on the development host. Open the same task URL on another device to
read the conversation and continue the task.

## Configure the development environment

1. Run T3 Code 0.1.11 on the development host and register the repository as a
   project. Use the workspace account that owns the repository and the Codex
   configuration. Give that account the permissions needed for development.
   Keep production credentials on the existing release infrastructure.
2. Verify that the chosen Codex provider and model can run a task in T3. Record
   the project ID, base branch, provider instance ID and model ID.
3. Connect the MILE backend to the T3 `/mcp` endpoint over private HTTPS, or
   use loopback HTTP if the processes share a network namespace.

On the T3 host, issue a five-minute pairing code under the account that runs T3.
Set `--base-dir` to the running instance's data directory if it uses a custom one.

```sh
t3 auth pairing create --ttl 5m --scope orchestration:read --scope orchestration:operate --scope settings:write --scope source-control:write
```

Use the code to grant the MILE workspace client Full access. The helper prompts
for the code and writes the issued MCP token to a private file. Create the
output directory with access limited to the operator and backend service.

```sh
python3 tools/t3_workspace_token.py --endpoint https://t3.internal.example/mcp --output /run/secrets/mile-t3.mcp
```

Pass these variables into the MILE backend process. For a container, mount the
token file read-only at the configured path.

```dotenv
DEV_WORKSPACE_ENABLED=true
DEV_WORKSPACE_T3_URL=https://t3.internal.example/mcp
DEV_WORKSPACE_T3_PROJECT_ID=<id from T3>
DEV_WORKSPACE_T3_DEFAULT_BRANCH=main
DEV_WORKSPACE_T3_PROVIDER=codex
DEV_WORKSPACE_T3_MODEL=<model id from the provider>
DEV_WORKSPACE_T3_TOKEN_FILE=/run/secrets/mile-t3.mcp
```

`DEV_WORKSPACE_T3_TOKEN` is available for secret injection by environment
variable. The file takes precedence when configured. T3 Code 0.1.11 issues
MCP tokens valid for 30 days. Rotate by issuing a new token before expiration,
reloading the MILE backend and checking the workspace. Revoke the previous
client in **Sharing & access** after confirming the new credential.

The workspace defaults to disabled and its API requires an administrator
browser session. Setting `DEV_WORKSPACE_ENABLED=false` disables the MILE
entry point on restart. T3 owns any already-running task and its worktree.

## Acceptance

Sign in as an administrator and launch a small Codex edit from a phone-sized
browser. Change devices using the task URL and send a follow-up after the
first turn completes. Check the changed files, completed test command and
commit SHA in the task. Verify stop with another running task. Use a normal
user session to verify that the same workspace API rejects access.
