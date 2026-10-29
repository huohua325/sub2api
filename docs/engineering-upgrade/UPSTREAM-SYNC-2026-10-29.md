# 上游同步：2026-10-29

合并前 fork 为 `21bace0f4bc8c607b4154845f77ee39513e95f72`，上次上游基线为 `f178e5ae32659e8901644a9f2378a0439ffa36ae`。此次固定合入 `Wei-Shaw/sub2api/main` 的 `a3298b46ebe239fb0720b068a1eb3383955bdac2`，共 21 个提交（包含合并提交），39 个文件。目标包含正式版 `v0.2.19` 以及其后 OpenAI 非流式响应修复。

独立工作树为 `.worktrees/upstream-20261029`，分支 `lab/sync-upstream-20261029`；保留主工作区所有未提交文件。使用真实 merge commit 保留上游祖先关系，便于下次增量合并。长期流程见 [合并规范](UPSTREAM-MERGE-RUNBOOK.md)，机器可读基线见 [upstream-sync.json](upstream-sync.json)。

## 本次纳入

- OpenAI OAuth 非流式请求正确聚合为 JSON，包括 incomplete/cancelled 终态，以及上游缺少 Content-Type 的 SSE；流式请求仍保持流式。
- OpenAI 模型列表进入 singleflight 后复查缓存，避免并发冷启动重复请求上游。
- `golang.org/x/net` 从 0.60.0 升至 0.62.0，纳入 GO-2026-6860 修复及相关 x/* 依赖更新。
- Gemini 同账号重试次数配置、显式缓存绑定重试、模型映射目录与默认模型列表更新。
- 管理员保护规则返回正确的 400；渠道设置保存保留 web_search_emulation 的其他平台维度。

GPT-6.1 Terra、管理员分范围 Key 和图片按张计费等已在前次合并，不重复宣称为本次新增。

## 定制保留与合并取舍

没有文本冲突。Git 自动合并了 `backend/go.sum` 和 `backend/internal/service/admin_user.go`；逐项检查管理员保护错误码更新与 MILE 自定义额度操作能同时保留。

本次上游没有修改数据库迁移、Ent schema/生成代码、额度仓库、个人周期与全体重置算法、预留及持久计费恢复代码，也没有修改 MILE 公共设计系统、学员页面、品牌素材、导航和 Grok config.toml 模板。验证时仍检查这些范围的 Git blob 与合并前一致，并执行现有业务回归。

生产事件不写入迁移；不创建或确认额外重置事件；Codex/Grok 不因合并自动启用 enforce。

## 独立审查补充修复

上游把 OAuth `stream=false` 请求转为 JSON 聚合后，原 `io.ReadAll` 会等待 EOF：完整终态后连接空闲会延迟返回，终态后出现 `io.ErrUnexpectedEOF` 会丢失回复和计费用量。此次增加有界的 SSE 终态读取，两个非流式入口共用；完整 completed / incomplete / cancelled 事件可以立即结束，终态前错误继续报错，超大响应仍拒绝。兼容 OAuth 上游缺失 Content-Type，普通 JSON 路径保持原逻辑。

先加入回归复现失败，再实现修复；不是改变额度规则或重新计算历史消耗。

复查同时保留了现有裸 `error` 后可能恢复为 `response.completed` 的协议语义：裸错误不会提前截断后续响应终态；`error → completed` 和 `error → response.failed` 均补充先失败再通过的非流式测试。

## 验证与发布

本次执行证据保存在本地 `output/upstream-20261029/`，不会把含生产数据的快照提交到 Git。

| 检查 | 本次结果 |
| --- | --- |
| 前端全量 Vitest | 394 个文件、3126 项测试通过 |
| 前端类型、只读 lint、国际化、生产构建 | 通过；国际化 3 项通过 |
| 隔离 PostgreSQL 额度竞态 | 实际设置 DSN，`TestCodingQuotaDB` 48 个测试/子测试通过，0 跳过 |
| 新增上游与补充回复回归 | `-race -tags unit` 最终 31 个测试、55 个测试/子测试通过，0 跳过；两项回复边界均已先复现失败再验证修复 |
| 默认 Go 全量，合并前 / 合并后 | 13850 / 13884 个通过事件，均有 60 个跳过、相同 9 个原有失败；没有新增失败 |
| 扩展相关 unit/race | 1333 个通过事件、0 跳过；2 项 Gemini 定价失败已在合并前版本重新复现 |
| Go vet | 通过 |
| 增量 Go lint | 最终代码通过，0 issues；修正了首轮 bytes.Buffer 返回值未显式忽略的问题 |
| 学员生产构建交互 | Chromium、WebKit 各 45 项通过，无页面错误；未声称本轮验证 Firefox |
| 管理员渠道设置 | 实际页面 mock 验证 OpenAI 搜索开关可编辑，保存时保留 Anthropic 维度；未写生产数据 |

9 项默认失败分类如下，均已在合并前 SHA 本次重跑复现，不以 skip 代替通过：

- `TestAuthIdentityFoundationSchemas`：身份基础 schema 断言与现有模型不一致。
- `TestAllPlatformsIncludesEveryConcretePlatform`、`TestCompositeGroupSchedulerHasAllCanonicalPlatformBuckets`：平台枚举/调度桶覆盖基线。
- `TestPgDumperHoldsMigrationLockThroughReaderClose`、`TestPgDumperReleasesMigrationLockWhenProcessFails`、`TestPgDumperReportsUnlockFailureAndDiscardsConnection`：Windows 上备份进程/锁测试环境基线。
- `TestUsageLogRepositoryCreateSyncRequestTypeAndLegacyFields`、`TestUsageLogRepositoryCreate_PersistsServiceTier`、`TestPrepareUsageLogInsert_PersistsNativeCompactionV2WithoutChangingRequestType`：用量写入 SQL/参数断言基线。

扩展 unit 的 `TestGatewayServiceRecordUsage_GeminiFlashThinkingTierUsesCatalogPrice` 和 `TestGeminiNativeReasoningPricingUsesExplicitForwardedLevel` 属已有 Gemini 定价基线；本次未把旧缺陷伪称修复，也未扩大定价改动。

截图、交互录像见 `output/playwright/upstream-20261029/`，Ryugi 本地画板为 `verify-upstream-20261029`，人工视觉结论待用户审阅。375/1440px 检查正常；768px 管理员页面横向溢出已在合并前线上旧版复现，属于原有布局限制，本次未修改公共布局。

独立只读审查最终未发现新增阻塞项。最后一轮曾因本机 C 盘 Go 临时编译空间不足中断；将编译缓存与临时目录迁至 D 盘后重跑，环境失败的日志保留，不作为通过证据。发布前再次查询远端 main，目标仍为上述固定 SHA。

上线前先备份数据库、程序、配置；按固定参考时刻核对所有重置、纠正、钱包锚点、周期和原账本，保持 Codex/Grok shadow 及原观察起点。具体源码提交、备份路径、健康和公开资源结果在发布后追加。
