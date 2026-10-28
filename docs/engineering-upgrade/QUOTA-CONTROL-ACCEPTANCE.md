# 并发额度控制验收与运行说明

本次默认部署 shadow。观察不拦截原本允许的请求；管理员现有「用量监控」增加预留、结算及恢复诊断，学员只增加状态说明，百分比仍为已入账用量加待结算估算，不显示点数。

## 模式与启用

分别配置 `SUB2API_CODING_QUOTA_OPENAI_MODE` 和 `SUB2API_CODING_QUOTA_GROK_MODE`，支持 `off`、`shadow`、`enforce`，未设置时为 `shadow`。`off` 仅关闭新预留，恢复及持久计费任务继续处理。

`shadow`/`off` 沿用原有准入与拦截口径；切换 `enforce` 后才启用本轮的原子预留及仅按已结算用量确认耗尽的规则。观察记录中的 `would_allow` 用于比较，不代表当前已经拦截。

以部署记录 `deployment.json` 的 `shadow_started_at` 为观察起点。每个用途须连续至少 24 小时、完成至少 20 条请求，且没有重复占用、重复结算或不可恢复记录，才可分别启用。执行 [只读检查](quota-control-readiness.sql) 并人工核对审计和计费证据；查询结果不是自动启用授权，也不会修改模式。未达标继续 shadow。

默认倍率门槛为同母账号官方窗口 20 条已结算样本、可信观察不超过 1200 秒，参数为 `SUB2API_CODING_QUOTA_MIN_SAMPLES` 与 `SUB2API_CODING_QUOTA_MAX_AGE_SECONDS`。预留保存 `window-cost-v1` 估算版本。Codex OAuth 会移除客户端输出上限，按未知估算进入单条模式；不会把极小客户端上限当成实际上限。

## 协议覆盖与裁定

| 入口 | 本轮处理 |
| --- | --- |
| Responses、Chat、Messages、流式与非流式 | 每个 HTTP 逻辑请求一个预留，内部重试复用；实际 HTTP 发送点记录已发送 |
| HTTP 转上游 WebSocket | 包括预热前的实际发送记录；中断保留待核实证据 |
| Codex 原生与透传 WebSocket | 每轮独立预留；后续轮次拒绝使用协议错误，不截断执行中的轮次 |
| Alpha Search，包括 PAT Responses 回退 | 选定母账号后准入，同一逻辑请求复用预留 |
| Grok HTTP 音频、媒体、视频 | HTTP 传递预留上下文；异步视频保留 PostgreSQL 定价证据，持久计费命令作为权威去重依据 |
| Grok 原生语音 | 每个活跃轮次预留，空闲不占槽；保持原有会话总时长的一笔账单，原子关闭关联预留 |
| 图片与 embeddings | 既有适用入口统一准入，不能用不可定价负载绕过；未知估算进入单条模式 |
| Gemini 批量生图 | 调用方平台检查仅允许 Gemini，使用原有实验计费，不纳入 Codex/Grok 预留 |
| OpenAI Live/WebRTC | 上游现有实现只写零费用日志，音频直连上游，无法在网关逐轮控制。本轮不伪造点数或新增计价。shadow/off 保持原行为；enforce 对新增 Codex Live 创建返回 503 `CODING_QUOTA_UNAVAILABLE`，已有会话不截断。要启用 Codex enforce 必须接受此限制或先完成 Live 真实计量升级 |

首轮 WebSocket 在连接建立后被拒绝时可按既有协议关闭尚未执行的连接；后续轮次拒绝保持连接。已发送但未知消耗的请求保留待核实证据，可能需要管理员核实，不按超时退款。单条模式允许有限暂时超额。

核心计费在返回前使用脱离客户端取消的 10 秒上下文执行，先持久交接再应用；代价是有界的完成延迟。数据库 JIT 仅在个人协调事务中关闭，避免复杂额度查询的编译开销。

## 测试与性能证据

隔离 PostgreSQL 使用 `SUB2API_QUOTA_TEST_DSN`，实际执行全部 `TestCodingQuotaDB -race`，不以 skip 作为通过。专项包含跨 Key/母账号准入、未知估算、过期租约与迟到续期、计费交接超时、迟到结果、幂等重放、缓存丢失、历史纠正、跨周期与补记全体重置。

最终审查的 8 个重要问题均以失败用例复现后修复：伪造 Upgrade 头、未知活跃估算遗漏、Codex 输出上限被移除、两处发送记录遗漏、过期租约恢复发送、计费交接永久占槽、Redis 视频认领崩溃空窗，以及 Alpha Search 入口遗漏。另验证 Live enforce 防绕过。

同一设备、同一隔离数据库的 200 次准入测量：事务关闭 JIT 前 p95 114.7ms / p99 152.0ms；关闭后 p95 9.32ms / p99 10.45ms。竞态及其他检查同时运行的一次测量为 17.57ms / 36.92ms。均为本地数据库准入耗时，不代表公网模型延迟。

修复后的最近一次 PostgreSQL 竞态测试为 48 个额度测试/子测试通过、0 跳过；200 次准入 p95 19.30ms、p99 28.25ms。最终协议专项的 3 个包全部通过，共 44 个通过事件；Go vet、增量 lint 通过。

前端 393 个测试文件、3118 项测试通过，类型检查、只读 lint、国际化检查及生产构建通过。Chromium、WebKit 使用生产预览和固定 mock 数据，检查 375/768/1440px、200% 缩放、明细按需加载及学员无点数。Firefox 的截图与快照已取得，完整自动化脚本遇到挂起，不能将完整矩阵记为通过。视觉验证板的人工判定仍待反馈。

## 全量测试基线

全量 Go 测试没有全绿，以下失败在起始提交 `ddbec984f` 复现。本轮专项测试、vet 与增量 lint 独立报告，不能用全量已有失败掩盖新失败。

| 原有默认测试的 9 项失败 | 分类 |
| --- | --- |
| `TestAuthIdentityFoundationSchemas` | 认证身份 schema 契约 |
| `TestAllPlatformsIncludesEveryConcretePlatform` | 上游新增平台集合 |
| `TestCompositeGroupSchedulerHasAllCanonicalPlatformBuckets` | Composite 平台集合 |
| `TestPgDumperHoldsMigrationLockThroughReaderClose` | Windows 子进程/迁移备份锁 |
| `TestPgDumperReleasesMigrationLockWhenProcessFails` | Windows 子进程/迁移备份锁 |
| `TestPgDumperReportsUnlockFailureAndDiscardsConnection` | Windows 子进程/迁移备份锁 |
| `TestUsageLogRepositoryCreateSyncRequestTypeAndLegacyFields` | 现有用量 SQL/mock 字段不一致 |
| `TestUsageLogRepositoryCreate_PersistsServiceTier` | 现有用量 SQL/mock 字段不一致 |
| `TestPrepareUsageLogInsert_PersistsNativeCompactionV2WithoutChangingRequestType` | 现有用量 SQL/mock 字段不一致 |

扩大至 `go test -tags unit ./...` 后共识别 22 个失败根测试，另 13 项同样已在起始提交复现：

- 契约/平台：`TestAPIContracts`、`TestCommandCodeGatewayPassesThroughCatalogProtocols`、`TestMatchingPlatforms`、`TestPlatformListDerivedListsMatchLegacy`。
- 定价夹具：`TestGatewayServiceRecordUsage_GeminiFlashThinkingTierUsesCatalogPrice`、`TestGeminiNativeReasoningPricingUsesExplicitForwardedLevel`、`TestOpenAIGatewayServiceRecordUsage_AggregatorClaudeUsesDefaultPricing`、`TestOpenAIGatewayServiceRecordUsage_CNAndOpenCodeClaudeHonorExplicitPricing`、`TestOpenAIGatewayServiceRecordUsage_GPT61SolExemptionSurvivesRepricing`、`TestRecordUsage_ImageReasoningPricing`、`TestRecordUsage_ReasoningPricingUsesForwardedEffort`、`TestResolveContextPricingSchedule_Scenarios`。
- 已有时序敏感测试：`TestOllamaProbeCallback_StaleLongDoesNotOverrideNewShort`。

详细命令输出保存在本地 `output/control-*.log`、`output/control-*.jsonl`。失败根测试按名称去重，子测试和包失败不重复计数。

最后一次全量运行记录 24382 个通过事件、22 个失败根测试：其中 21 个属于上述已复现基线，另一个为 `TestValidateCreateParams_CheckModeMatrix` 的外网 DNS 校验失败。该测试以及 `validateCreateParams`/`validateEndpoint` 路径没有修改；原版本单独连续 3 次及整包中的该测试均通过，故不能声称已在原版本复现这次 DNS 故障。保留这项不稳定的环境失败记录，不将整套测试标成通过。前一次全量运行的同项通过，原有 `TestCommandCodeGatewayPassesThroughCatalogProtocols` 也并非每次复现。

## 发布与回退

服务器 `172.104.117.114`，程序目录 `/opt/sub2api`。替换前停止应用、固定参考时刻读取全部重置事件、注册/钱包起点、纠正与对账、原账本与个人周期，备份 PostgreSQL 并验证 `pg_restore --list`，保留旧程序及配置。上线后核对这些不变量、已耗尽用户、迁移及 Sub2API/MemSlides 健康。

迁移 257/258 只增加通用运行数据结构，不初始化生产事件，不改历史扣点。不能重新执行历史信用纠正，也不能把部署时刻写成重置事件。正式发布结果另记于部署记录。

回退优先将相应用途设为 `off` 并继续运行含恢复器的程序；不删除预留或任务，不用旧数据库覆盖新消耗。若要回退到完全不含恢复器的旧程序，必须先保持新版本恢复工作进程处理持久任务，否则不能宣称已安全回退。

## 2026-10-28 生产发布记录

- 版本：`0.2.18-mile.20261028.control1`，程序源码提交 `9c129e321da08b3b63583f0d88e955726b75efd6`。
- 程序 SHA256：`3d1367ec1c7cf4263e9860fa6bea873c43f1d582aa1c6dccf6d7bad5494e3d25`。
- 完整备份：`/opt/sub2api/backups/before-coding-control-20261028T090010Z`；数据库备份 3,872,059 字节，已通过 `pg_restore --list`，同时保留旧程序及配置。
- 迁移数量 310 → 312，只新增 257/258。保留原始请求 3095 条，其中 2902 条已结算/期初记录内容逐条一致；全部重置事件、注册/钱包起点、纠正与对账、固定参考时刻的个人周期均通过前后核对。
- 现场发现管理员已在 15:02 确认今天 **14:29** 的另一条全体重置。首轮额外核对仍假设用户 6 在当前周期已耗尽，因此在替换前中止并恢复原服务；核实后保留该事件与新周期，改为核对当前真实耗尽用户及原历史扣点，未修改任何业务记录。两条确认事件的详情已补充到 [重置规范](CODEX-GLOBAL-RESET-RUNBOOK.md)。
- Codex、Grok 均为 `shadow`；观察起点为 **2026-10-28 17:00:11.898991 北京时间**。最早可在 10 月 29 日同一时间后评估，仍必须分别满足至少 20 条完成请求及异常核对，不会自动启用。
- 上线后的首次只读检查：Codex 完成 2 条、Grok 完成 0 条；两者待核实、过期活跃、重试失败及待处理任务均为 0。两种用途的观察门槛均未满足，继续 shadow；这些是当次快照，后续使用应重新查询。
- Sub2API、MemSlides 健康；公网 `/health`、`/login`、`/api/v1/settings/public` 均为 200，16 个发布页面资源与本地生产构建 SHA256 一致。

实际部署、只读观察及公网资源核对文件保存在服务器 `/opt/sub2api/releases/mile-20261028-control1/` 及本地 `output/releases/mile-20261028-control1/`。没有发送人为付费模型请求来凑观察样本。完整 Firefox 自动化和验证板人工判定仍待完成。
