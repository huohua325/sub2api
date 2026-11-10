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

已于 **2026-11-11 02:36 北京时间**部署到 `172.104.117.114`，并同步 fork 的 `lab/0.2.17` 分支。

- 合并提交：`c7297838b1b3bf0e0a0e64340d52bf112f32dc9a`。
- 运行时源码：`4bbdb4685b991a02731262a74f4110ccad93bfbe`。
- 版本：`0.2.20-mile.20261111.upstream1`。
- 程序 SHA-256：`5f6ac9ad3300a47cbbf5b3a52de8ffac4291369bd15350531fb82fe662f08660`。
- 备份目录：`/opt/sub2api/backups/before-upstream-20261111-20261110T183608Z`；数据库 dump 为 16,676,032 字节，已验证 `pg_restore --list`，程序与配置备份留在服务器。
- 固定参考时刻：`2026-11-10T18:36:24.359457+00:00`，即北京时间 **11 月 11 日 02:36:24**。

程序通过约 4.09 MB 的差量包传输，在本地和服务器分别重建后验证完整程序哈希，再原子替换。只重建 `sub2api` 容器，保留原镜像、Caddy、数据库、Redis、MemSlides 和环境配置。

同一参考时刻保留全部 **23,349** 条原请求、**21,509** 条 settled/imported 记录及 **1,406** 条未知计量证据。已确认及候选重置事件、扩容事件、历史纠正／对账、原钱包起点、个人周期、持久预留／事件／计费任务／异步任务与管理员 Key 策略均通过核对。容量归一后的已结算消耗未减少。Codex/Grok 仍为 `shadow`，观察起点继续为 `2026-10-28T09:00:11.898991Z`；容器环境逐项一致。

迁移总数 **314 → 316**，新增 Key 成本覆盖索引和既有校准索引均有效。另核对管理员／母账号／控制／学员接口的 **12 条钱包读数**及 **10 条学员读数**，个人倒计时一致，未向学员增加点数或管理员诊断字段。

线上 **30 个公开资源哈希一致**，应用、登录和公开设置均为 200，MemSlides 健康为 200，启动 ERROR/FATAL 为 0；实际容器版本、源码 SHA 和主机程序哈希匹配。线上真实前端资源在 Chromium 再次验证两个页面的复制、键盘操作及 375／768／1440px 布局，无脚本异常或文档溢出；业务数据全部 mock，没有生产修改或推理测试调用。深色地址框另行检查，无溢出。Grok 运维诊断入口可读，未制造生产错误进行验证。

原有定制与上游无交集的路径中，最终 **447 个 Git blob 未改动**；既有 migrations 目录的 **338 个路径**均保留原 blob。完整证据见 `preservation-final.json`、`deployment.json`、`runtime-verification.json`、`startup-verification.json`、`quota-api-verification.json` 和 `public-verification.json`。发布文档提交可以晚于运行时源码提交。

回退只退程序，保留所有新旧账本与持久任务，继续处理已持久化计费任务，不以旧数据库覆盖新消耗。新增列和索引保留，不能为回退重置账本、重建个人周期或删除审计事件。
