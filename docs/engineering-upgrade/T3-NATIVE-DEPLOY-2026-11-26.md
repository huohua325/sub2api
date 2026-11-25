# MILE Lab 远程开发工作台部署与全阶段验收记录（2026-11-26）

## 1. 部署坐标与产物指纹

- **生产主机**：`172.104.117.114`（`/opt/sub2api`）
- **生产管理入口**：`https://sub2api.172.104.117.114.sslip.io`
- **工作台独立 HTTPS 入口**：`https://studio.172.104.117.114.sslip.io`（中性域名，无 T3 字样暴露）
- **线上部署版本**：`0.2.25-mile.20261126.t3native2`
- **发布源码提交**：`dabf8d680e996c00c6c41645396b098f1bd0cce1`（分支 `lab/native-t3-release-20261126`；主维护分支 `lab/0.2.17` 已同步）
- **前一生产基线**：`189f3c233116e8aca713ef0e6b6d3554a9c9d326`（`35cc2f9da82b3c492d107c1078d66d831b44119eb4089cb17a81cc764ada3400`）
- **新程序 SHA256**：`b91bcaad07d6ad581383b7c0d9080d9e1e0bab8e892d4ac57f814d98b7c9eaf7`（`129,511,584` 字节）
- **差量包**：`app.bsdiff`（`3,128,788` 字节，SHA256 `5985298c97b664f0987bf4800499704badd945b8cca5db261da0f057b42ca689`），经私有中转传输 11.0 秒并自动清理草稿
- **数据库迁移**：0 新增，原有迁移、额度账本、审计、容量折算、预留、异步作业与实验结算完整保留
- **备份目录**：`/opt/sub2api/backups/before-mile-20261126-t3native2-20261125T172954694379Z`（主切换）、`/opt/sub2api/backups/before-ws-gateway-override-20261125T173153Z`（Compose overlay）
- **准入模式**：Codex / Grok 保持 `shadow`

## 2. 阶段 1（路线 A 网关与体验增强）

- **Redis 持久化会话 + Refresh Family 活性校验**：`t3gateway` 支持可插拔 `Store`，生产接入 Redis（`mile:devws:ticket:` / `mile:devws:session:`），仅存非敏感身份快照（`UserID` / `SessionID` / `TokenVersion` / `BindingHash`），不存储原始 JWT；Sub2API 重启不掉线、不受单张 Access Token 过期影响，MILE 登出或撤权仍实时断连。
- **深链接无感回跳**：未持有有效网关 Cookie 的浏览器 HTML `GET` 导航自动 `303` 跳至 `https://sub2api.172.104.117.114.sslip.io/admin/development-workspace?next=<sanitized_path>`，认证并自动配对后由 `GET /` 消费一次性 `__Host-mile_ws_next` Cookie 还原原线程地址；API / 静态资源 / WebSocket 请求仍严格返回 `401`。
- **Pairing Token 热重载与 14 天自动轮换**：`t3_native.go` 按文件 `mtime`/`size` 自动热重载，容器通过目录只读挂载 `/etc/sub2api/secrets:/run/secrets:ro` 感知原子替换；宿主机 `mile-t3-pair-rotate.timer` 每 14 天自动续签 30 天 issuer 凭据。
- **中性命名与健康探测**：Cookie 更名为 `__Host-mile_ws`，对外文案去 T3 化；新增管理员只读接口 `GET /api/v1/admin/development-workspace/native-status`。

## 3. 阶段 2（同机隔离 Runner 与多 Coding Agent 实跑）

- **隔离账户与资源护栏**：`t3workspace`（UID 993 / GID 984），官方 T3 `0.1.11`（SHA256 `2b691c07f5bcb5adea4a863d5324aae7db21bd6d82280cec7787836dcaa013e1`），systemd cgroup 限额 `CPUQuota=25%`、`MemoryHigh=512M`、`MemoryMax=768M`、`MemorySwapMax=0`、`IPAddressDeny=172.18.0.0/16`，监听 `127.0.0.1:3773`，经私有 Unix socket `/run/mile-t3-gateway/t3.sock`（`root:mile-sub2api-app` GID 1000、`0660`）供 Sub2API 容器访问。
- **Fork 仓库权限**：为 `t3workspace` 生成仓库级 ed25519 SSH Deploy Key 并注册至 `huohua325/sub2api`，检出路径 `/var/lib/t3workspace/project`。
- **已实跑验收的 Coding Agents（统一走 MILE 网关）**：
  - **Codex CLI `0.172.0`**（`gpt-6.2-sol`）：完成隔离目录内命令执行、编辑 `sample.txt` 并校验结果。
  - **Claude Code CLI `2.1.332`**（MILE Claude 渠道）：真实问答返回 `CLAUDE_VERIFIED`。
  - **OpenCode CLI `1.19.6`**（MILE Grok `grok-build-latest`）：真实执行仓库 `Glob` 工具调用并准确返回 `backend/cmd/server/main.go`；已预置 `mile-openai`、`mile-grok`、`mile-claude` 线上实际可用模型清单。

## 4. 阶段 3（CI 不可变候选构建 + 权限隔离发布作业）

- **GitHub Actions 工作流**：`.github/workflows/mile-release-candidate.yml`（支持 `workflow_dispatch` 与 `release-candidate/**` 分支推送），在 GitHub runner 上完成前端全量、隔离 PostgreSQL 18 `TestCodingQuotaDB -race`（强制零跳过）、定向网关测试与 Linux/amd64 嵌入式编译，产出不可变候选包与 `manifest.json`。
- **宿主机受控发布作业**：`tools/release_worker.py` 部署于 `/opt/mile-release-worker/`，配合 `mile-release-worker.path` / `mile-release-worker.service` 与 `/usr/local/bin/mile-submit-release`。
- **最小权限验证**：实测 `t3workspace` 对 `/opt/sub2api`、`/etc/mile-release/worker.env`、`/etc/sub2api/secrets/mile-ws-pair-token`、`/run/mile-t3-gateway/t3.sock`、`/run/docker.sock` 全部无读权限，仅可向 `/var/spool/mile-release/submissions/` 投递候选元数据；正式 `--apply` 必须由 root 显式 `--approve` 执行并沿用 `release_server.py` 备份与持久数据守卫。

## 5. 完整验收证据汇总

- **本地验证**：前端全量（lint / `vue-tsc -b` / 全量 Vitest + i18n / Vite build）通过；Go 竞态定向测试 575/575 通过（0 跳过）；交付工具单测 242/242 通过（含 `test_release_worker.py`）；隔离 PostgreSQL 17.6 `TestCodingQuotaDB -race` 166/166 通过（0 跳过）；Chromium + WebKit 通用场景与工作台 36/36 专项回归通过。
- **线上公网验收**：
  - `check_public_release.py` 核对 `https://sub2api.172.104.117.114.sslip.io` 全部静态资源与入口通过。
  - 游客访问 `studio.172.104.117.114.sslip.io`：`/api/threads` 返回 `401`、`/mcp` 与 `/api/auth/pairing-token` 返回 `404`、未认证 WebSocket `/ws` 返回 `401`、浏览器 HTML 深链接 `GET /threads/test-deep?tab=diff` 返回 `303` 重定向至 MILE 后台 `?next=...`。
  - 真实管理员端到端闭环：登录 → `native-status` 返回 `{configured: true, runner_reachable: true, token_loaded: true}` → 签发带 `next` 票据 → `/_mile/enter` 换取配对码并下发 `__Host-mile_ws` / `__Host-mile_ws_next` → 票据重放被拒 `401` → `GET /` 精准还原 `/threads/e2e-check` → 真实工作台 HTML 代理 `200` → MILE 登出后原 `__Host-mile_ws` 立即失效返回 `401`。
  - 宿主机健康：Sub2API、MemSlides、Caddy、`t3code-workspace`、`mile-t3-gateway.socket`、`mile-t3-pair-rotate.timer`、`mile-release-worker.path` 全部 `active/healthy`，Codex/Grok 维持 `shadow`，宿主机剩余可用内存约 1.16 GiB。
