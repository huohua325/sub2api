# Codex CLI / VS Code 统一编程入口

本版仅本地开发、验证和提交；不推送、不部署、不修改生产分组、密钥或额度。

## 设计

管理员显式创建 `platform=composite, usage_kind=coding` 分组，绑定获准的 OpenAI / Grok 账号及模型路由，并按现有分组授权规则授权用户。用户创建或将原有 Key 绑定到该分组后，同一 Key、同一 Base URL 可在 Codex CLI 和官方 VS Code Codex 扩展的模型菜单选择实际 OpenAI / Grok 模型。不自动迁移已有 Key，不引入 CLIProxyAPI 服务。

本版统一入口只调度订阅 OAuth / SetupToken 账号。若分组错误绑定 API-key 上游账号、实际调度平台与解析平台不符，或计量服务／存储未配置，转发前拒绝；不把无法取得订阅计量的请求作为免费调用。管理员应使用现有订阅账号并设置清晰、不重名的模型映射；别名的计费平台仍以实际路由和账号为准。

请求先执行分组模型白名单和现有 Composite 路由解析，再按实际目标平台检查个人编程额度。调度后的预留和最终计费使用实际账号平台；不按客户端名称或模型前缀猜钱包。OpenAI 耗尽不阻止仍有额度的 Grok，反之亦然。未知、歧义或其他平台请求拒绝，不降级成 API 实验或共享余额。

首版统一入口限 HTTP Responses（含 compact / input_tokens）及只读模型、用量和倍率查询；统一配置关闭 WebSocket。其他协议及实时/媒体入口拒绝，避免未经验证的额度旁路。原有单平台分组、Composite 实验分组行为保持原样。所有账本、周期、容量、纠正、个人重置和持久任务沿用原实现；Codex / Grok 模式保持 shadow。

新用户复制或下载一份用户级 `~/.codex/config.toml`，直接使用 Key，无终端环境变量前置步骤；CLI 和 VS Code 在同一运行环境读取这份配置。已有配置需合并保留自定义项，不能直接覆盖；重启 CLI / 重载扩展后刷新模型。WSL / Remote SSH 各自配置其运行环境。不支持桌面客户端。

## 实施与验证记录

- 开始：2026-11-13 11:19:24（北京时间）；基线 `6f0b8a36ec4bce4e794cdcb2e5a74a0e8330eaef`。
- 阶段 1：实际平台用途解析、准入与持久计费回归。
- 阶段 2：模型目录、管理员入口、统一 Key 分类及 CLI / VS Code 配置。
- 阶段 3：分支审查、前端完整验证与产物浏览器检查、真实隔离 PostgreSQL 竞态。
- 证据位于忽略目录 `output/delivery/codex-grok-unified/`；模拟上游不等同真实订阅端到端验证。

### 复用验证入口

```powershell
& D:/Programs/anaconda3/python.exe tools/review_unified_coding.py --base-url http://127.0.0.1:5189 --output output/delivery/codex-grok-unified/early-feature-browser
& D:/Programs/anaconda3/python.exe tools/review_unified_coding.py --artifact --output output/delivery/codex-grok-unified/artifact-feature-browser
```

两种浏览器使用合成 Key 和完全拦截的业务接口，检查创建 Key 的分组快照、一份共用配置、密钥遮罩、授权目录读取、下载和 375/768/1440px。脚本不会产生真实推理或生产业务写入。

### 审查

独立审查覆盖路由先于额度判断、未知／歧义模型拒绝、计量缺失和账号平台不符、最终持久计费用途、共享余额旁路、模型目录及单文件配置，未发现 Critical / Important 问题。发现统一入口仍展示旧环境变量提示，已补回归断言、复现失败并修正为统一配置说明；复查用例通过。

本版没有远端联调。尚需以获授权的真实订阅账号在 CLI / VS Code 中核对实际模型能力、流式工具回合及上游额度，模拟测试不能替代这项验证；本次不为验证创建生产分组或发起付费请求。

### 最终结果与耗时

- 从开始到产物验证：**32 分 39 秒**（11:19:24 → 11:52:03，北京时间）。设计、开发、专项测试和审查至源码冻结约 25 分 44 秒；最终验证约 6 分 55 秒。详细起止与各命令耗时见 `timing.json`；命令并行执行，不能直接相加当作总耗时。
- 前端全量：406 个文件、3257 项全部通过；只读 lint、类型、国际化和生产构建通过。只构建一次前端产物。
- 后端相关检查：860 项实际通过、零跳过，覆盖 Composite 路由、额度读取／预留／结算、Grok 转发、模型目录、API Key 鉴权与计费。未重跑已知存在无关基线失败的整个 Go 全量。
- 最终隔离 PostgreSQL `-race`：74 项实际通过，零跳过、零失败，证据 `quota-final/manifest.json`。初次连接串因测试追加 search_path 而失败，仅将本地测试 DSN 改为关键字格式后重跑；未改业务代码迁就测试，失败原日志保留。
- Chromium / WebKit 对最终构建产物均通过统一 Key 创建、合成凭据遮罩、两家模型目录、单文件下载及 375/768/1440px；既有指南导航产物回归也通过。产物 SHA256 指纹为 `0ac20d98ed948039503cdc18d7f0b3699fce667d9ea982010fde18fe706c8dc7`。
- 无新增依赖、无数据库迁移，迁移文件总数仍为 317。未创建或修改任何生产业务记录，未改变 Codex / Grok 默认 shadow。

客户端配置依据：[共享配置](https://developers.openai.com/codex/config-basic/)、[自定义 provider](https://developers.openai.com/codex/config-advanced/)、[API Key 认证](https://developers.openai.com/codex/auth/)。
