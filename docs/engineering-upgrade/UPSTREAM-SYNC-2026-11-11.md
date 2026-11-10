# 2026-11-11 接入地址说明与上游同步

本次根据平台所有者指示，补充学员接入说明、可复制的 API Base URL，并同步固定上游提交。获授权推送 fork 和部署 `172.104.117.114`；不向官方仓库推送。

## 固定范围

- 合并前 fork：`406880d1396d1bb9747b90b8618760ea2cc9f39a`。
- 上次上游：`6b4b37db43443b5e88e424fe3c69602cc3810dfb`，v0.2.20。
- 本次上游：`e2c9812190dd71bb309c8ad020a1211da1e0d3b4`，77 条尚未纳入的提交；包括正式版本后的 main 提交。
- 独立工作区：`.worktrees/baseurl-upstream-20261111`。使用真正的双亲 merge，不 squash 上游历史。

上游主要改动涉及 Key 列表异步加载用量、refresh token 原子轮转、Codex 网页上下文和 WebSocket 重试、兼容接口错误策略、OpenAI 缓存写入时长、新模型价格和管理页稳定性。

## 接入页面

「开始使用」的准备密钥区域增加本站实际 Base URL、一键复制、通用格式、客户端字段示例及完整 HTTP 请求地址。三个用途切换时都能看到地址。

「API 密钥」筛选区域上方增加紧凑的地址框。地址读取现有公开配置，未配置时沿用当前站点 origin，通过既有函数仅补一次 `/v1`，保留反向代理前缀。复制失败不显示成功，配置变化时清除旧反馈；支持键盘、移动端和深色模式。其他自定义线路继续显示。

说明限定 OpenAI 兼容用法；Claude / Anthropic、Gemini 等仍使用对应密钥的配置面板，避免错误地为所有协议强制附加 `/v1`。新增组件不请求接口，不改变 Key、权限、模型或额度逻辑。

## 冲突与保留

冲突为六个 repository 文件。官方额度的七个字段与上游两个缓存时长字段全部保留，INSERT 类型、列、参数、SELECT 扫描顺序一致，共 71 个参数；两条静态 INSERT 的占位符同步。保留官方窗口索引和新 Key 成本索引的无效索引恢复分支。

相关测试中三项已有失败来自缺失官方字段的 SQL 预期和错误的尾部偏移，本次修正测试夹具，未修改生产字段含义或原始扣量。其余合并交集逐项检查，保留 Grok 脱敏诊断、个人周期、容量投影、纠正、预留和计费交接。

新增迁移仅为 `249_add_usage_logs_api_key_cost_index_notx.sql` 和 `250_openai_cache_write_ttl.sql`。既有迁移内容不改，额度相关表不重建、不删账、不回填重置。生产预期迁移总数 314 → 316。

## 验证记录

证据目录为 `output/baseurl-upstream-20261111/`，发布目录为 `output/releases/mile-20261111-upstream1/`。

- 前端全量：404 文件、3,233 项通过；类型检查、只读 lint、国际化检查和生产构建通过。
- Chromium 和 WebKit 在真实生产构建上检查两个页面的 375、768、1440px 布局；无页面脚本异常、无文档横向溢出。Chromium 实际检查剪贴板内容和键盘复制。全部业务 API 被 mock，未提交生产修改或付费调用。
- Firefox 当前版本未安装，未计为通过。WebKit 显式使用已安装的运行时，两个页面验证通过。
- 隔离 PostgreSQL 的 `TestCodingQuotaDB -race -count=1` 实际 66 项通过、0 跳过。
- Go vet 和相对合并前 fork 的增量 golangci-lint 通过，0 issues。
- 相关 11 个包的 unit 竞态回归：3,150 项通过、1 项需要外部 OpenAI API 的既有条件测试未执行、0 失败。另行执行不适合进程混合分配统计的测试并通过。
- 两项新 SQL 在隔离 PostgreSQL 中各执行两次，覆盖索引有效，已有用量行与价格保持不变。
- 默认 Go 全量：合并前 15,019 项通过、15 跳过、9 个失败根；最终 15,490 项通过、15 跳过。修正三个相关 SQL 夹具后，剩余六个既有失败根，另有 Windows symlink 权限限制；对应 Linux 原测试 12 项通过。未把默认全量标成全绿。

剩余六个既有默认失败为 `TestAuthIdentityFoundationSchemas`、`TestAllPlatformsIncludesEveryConcretePlatform`、`TestCompositeGroupSchedulerHasAllCanonicalPlatformBuckets`、`TestPgDumperHoldsMigrationLockThroughReaderClose`、`TestPgDumperReleasesMigrationLockWhenProcessFails`、`TestPgDumperReportsUnlockFailureAndDiscardsConnection`。均在合并前 SHA 的独立工作区实际重现。

unit 标签额外发现旧管理员响应夹具漏了五个 MILE 字段、旧邮件缓存替身返回共享指针导致竞态，以及上游新缓存计费测试错误地期望再次扣旧余额。本次分别修正夹具、使缓存替身返回加锁快照、保持真实计价断言但验证不重复扣旧余额。原失败日志保留，修正后的完整相关竞态均实际执行通过。

最终另做一次独立于编码过程的自审，未使用独立审查者；检查记录在 `final-review.md`。新增上游迁移原有的文件末尾空行保持原样，因此全量 `git diff --check` 的该项空白提示保留，不为消除提示改写迁移校验和。自有改动的空白检查通过。

初次本机竞态构建遇到临时磁盘空间不足；随后将本任务编译临时目录移到 C 盘。恢复本地隔离 PostgreSQL 后重新实际执行，前面的链接失败和连接拒绝不计为通过。首次前端全量运行与编辑重叠造成 locale 缺项，冻结修改后重跑全量通过。

上游新增的 cgroup 逃逸测试在 Windows 上无法创建 symlink，作为环境失败单独记录；保留测试原断言，在 Linux 上执行原测试，不增加 skip 或弱化断言。

## 发布

待验证完成后填写运行时源码、程序哈希、备份目录和固定参考时刻核对结果。回退只退程序，保留所有新旧账本与持久任务，不以旧数据库覆盖新消耗。
