# 2026-11-05 官方 main 增量同步

用户明确要求安全合并、推送 fork 远端并部署至 `172.104.117.114`。本次按合并流程在独立 worktree 操作，原工作区的未跟踪文件保持原样。

## 固定范围与合并取舍

- 合并前 fork：`a7f80183003582dfda0ca8659a8f6551d282d3cc`。
- 上次官方基线：`4a4a83ee5ec86b19d3a2dd9b6471de5128963bec`。
- 本次官方 main：`8e92c892b47f3f934f18ce63da4c4dcb75f0b6bd`，23 条提交（14 条非合并提交），最近可达正式标签仍为 `v0.2.19`。
- 上游改动 187 个文件；以固定 SHA 执行 `git merge --no-ff --no-commit`，保留真实双亲历史。

11 月 6 日续行发布时重新核对远端，官方新增 `25608d216bb7be04e4a646830cb065cd09b7d7e9`，仅更新两份 README 的赞助信息及一张根目录赞助图片。以第二个真实合并纳入，最终相对原基线共 24 条提交；两次合并之间没有新增业务代码或迁移。实际部署时间以本文后续发布记录为准，版本中的 `20261105` 是本次集成批次名称。

同步内容包括网关内存／流式／后台任务资源边界、Grok Responses 输入兼容、OpenAI instructions 回显、生图尺寸选号、邮箱黑名单及批量禁用确认、CSV 导出限流重试、Kimi 冷却和 Antigravity 兼容。

472 个定制路径中有 42 个与上游交叉，430 个非交集路径保持原 blob；另核对 476 个受保护文件。原数据库迁移、额度／容量折算／个人周期实现、MILE 品牌与公共布局、学员仪表盘和动态模型目录未变。本轮不增加数据库迁移，不启用 enforce，不改生产事件或余额。

七处冲突逐块组合，没有整文件选 ours/theirs：

| 文件 | 保留与合入 |
| --- | --- |
| `grok_audio.go` | 保留结果上的 CodingReservation 传入必要计费任务；合入在闭包前快照渠道字段，避免使用已复用的 Gin context。 |
| `http_upstream.go` | 在真实发送前继续 MarkSent，再创建上游新的单次请求／有限收尾 context；保持失败取消和 Body.Close 所有权。 |
| `auth_email_oauth_auto.go` | 保留带 email 的邀请校验及邀请额度覆盖；先检查新黑名单。 |
| `auth_service.go` | 黑名单优先拒绝；学员精确白名单仍先于后备域名规则，未批准学员继续拒绝，批准学员保持原豁免。 |
| `gateway_usage_billing.go` | 合入有界持久化与通知 worker；保留 `!requestHasPurposeWallet(p)`，避免对用途钱包再累计旧平台金额计费。 |
| `RegisterView.vue`、`EmailVerifyView.vue` | 保留 EMAIL_NOT_APPROVED、EMAIL_EXISTS 中文反馈，同时加入 EMAIL_BLOCKED。 |

注册合并回归测试先证实简单采用上游后备策略会丢失精确白名单（RED），组合规则后通过（GREEN）；覆盖批准、未批准、同时命中黑名单及策略／配额两入口。上游把 CC Switch 默认模型改为 `gpt-5.6-sol`，原测试仍期望 `gpt-5.5` 的失败已修正，18 项专项通过；没有改用户显式模型选择。

首次增量 lint 还发现上游 `body_budget_test.go` 的失败分支未读取错误通道值；已将错误放入原有无条件失败断言的诊断信息，没有放宽断言或改动生产代码。该包完整 `-race` 复跑通过，独立审查附记确认此改动不改变生产行为。首次静态分析同时触及超时；最终检查记录单独保留，不能把超时或进程中断当作通过。

## 验证记录

完整日志与独立审查在本地忽略目录 `output/upstream-20261105/`。

- 前端基线：399 文件、3,183 项通过。合并后首次全量：3,208 通过、1 项旧模型默认值断言失败；修正后全量 403 文件、3,209 项通过。
- 前端类型检查、只读 ESLint、国际化 3 项及生产构建通过。Browserslist 资料过期及大 chunk 提示保留为构建警告，没有改动工具链或锁文件。
- 基线第一次全量 Go：14,121 项通过、15 跳过，包含测试库未运行导致的额度连接失败。启动既有隔离 PostgreSQL 后，基线 `TestCodingQuotaDB -race -count=1` 实际 66 项通过、0 跳过；连接失败没有被记作通过。
- 合并后默认 Go 全量：14,576 项通过、15 跳过；9 项既有固定失败和 1 项 server-timing 计时失败。计时项在真实基线、合并后分别重复 3 次，均通过。不能将默认全量报告为全绿。
- 合并后必需的 `TestCodingQuotaDB -race -count=1` 实际 66 项通过、0 跳过，覆盖并发、交接恢复、原周期、全体重置、容量折算和审计记录保持。
- 11 月 6 日发布前再次启动隔离 PostgreSQL 并实际复跑同一额度竞态命令，66 项通过、0 跳过。
- 受影响后端 `-tags=unit -race` 专项：1,406 项通过、0 失败、0 跳过；包含资源边界、协议处理、计费、认证、额度及新增合并回归。额外 repository 竞态：29 项通过、0 跳过，包含上游有限收尾、晚到终止用量及管理员相关测试。发布前全量 Go vet 通过；相对原 fork SHA 的最终增量 lint 为 0 issues，完成状态已验证，未将此前超时或中断当成通过。
- Chromium、WebKit 在真实生产构建检查黑名单编辑／校验／保存后禁用确认、管理员 Key 权限流程、动态模型筛选／复制／失败保留、用量监控及详情按需加载；覆盖 375/768/1440px 和模型页 720 CSS px 的 200% 等效视口，无意外横向滚动或脚本错误。模型等效视口不是浏览器原生缩放。未重新运行 Firefox。
- 所有浏览器 API 均显式 mock；没有向生产提交黑名单、禁用账号、确认重置或付费模型调用。
- 独立整分支审查未发现可确认的 P0–P2 合并回归。审查边界和上游有限取消收尾的取舍均记录在 `independent-review.md`。

保留的 9 项固定失败：

1. `TestAuthIdentityFoundationSchemas`：既有 schema 断言。
2. `TestAllPlatformsIncludesEveryConcretePlatform`、`TestCompositeGroupSchedulerHasAllCanonicalPlatformBuckets`：既有平台枚举预期。
3. `TestPgDumperHoldsMigrationLockThroughReaderClose`、`TestPgDumperReleasesMigrationLockWhenProcessFails`、`TestPgDumperReportsUnlockFailureAndDiscardsConnection`：既有 pg_dump／迁移锁测试。
4. `TestPrepareUsageLogInsert_PersistsNativeCompactionV2WithoutChangingRequestType`、`TestUsageLogRepositoryCreateSyncRequestTypeAndLegacyFields`、`TestUsageLogRepositoryCreate_PersistsServiceTier`：既有用量日志 SQL 参数断言。

实际发布核对如下。回退仅回退程序并保留当前数据库、账本、预留和持久计费任务；不使用旧数据库覆盖线上新消耗。


## 实际发布：2026-11-06

- 业务双亲 merge：`1b6bb9724ab1581346b3f9c3ae8d0c53de6d5c29`；赞助资料双亲 merge／运行源码：`3347375e4b26a86c5dbb08f931b0018d5e23a0c7`。后者的 backend/frontend tree 与已验证业务合并相同。
- 服务器 `172.104.117.114`，程序版本 `0.2.19-mile.20261105.upstream1`；批次名称沿用 11 月 5 日，实际构建及部署均在 11 月 6 日。
- 二进制 SHA-256：`3ce1021bb4e061acdc6c8c1154640bf716365a93253a5d42753e76c027c62683`，127340704 字节。前端冻结 197 个文件，构建后逐项哈希核验；上传差分本地及服务器重建均与完整二进制一致，没有原地修改正在运行的文件。
- 已验证数据库备份：`/opt/sub2api/backups/before-upstream-20261105-20261106T014655Z`，数据库 dump 11788360 字节，`pg_restore --list` 成功；同时保留原程序、容器／配置和数据归档。
- 固定参考时刻：`2026-11-06T01:47:29.085251+00:00`（北京时间 09:47:29）。原 15744 条请求 ID 保留，14331 条 settled/imported 记录逐字段保持；1406 条原待核实计量记录证据保留。
- 所有重置／扩容事件、纠正／对账、钱包起点、预留／预留事件／计费任务／异步任务通过发布前后核对；相同参考时刻下个人周期一致，容量折算后的已结算用量没有下降。管理员 Key 策略保持原样，没有新增授权。
- 当前既有用户的 Codex 周期结束于 11 月 11 日 14:35:59（北京时间），来自原已确认事件的自然七天序列；不是部署建立的新起点。Grok 按各自原周期；没有确认候选或创建生产重置事件。
- Codex/Grok 继续 `shadow`，观察起点仍为 `2026-10-28T09:00:11.898991Z`；迁移总数仍为 314，校准索引有效。
- 新程序启动错误 0；应用与 MemSlides 均健康。公网 25 个关键资源哈希匹配；登录页、健康与公开设置接口均 200。
- 在实际公网新版页面拦截全部 API 后复测黑名单编辑、校验、保存后禁用确认及移动布局，无脚本错误；没有向生产写入黑名单或禁用账号。
- 登录后只读接口核对 12 个用户／用途钱包，个人汇总与母账号归属、控制汇总一致，学员周期一致。动态模型目录本次实际返回 178 个，`partial=false`，包含 `gpt-6.1-terra`；模型数量来自当前账号能力，不把此前 193 个视为固定要求。

本地发布证据：`output/releases/mile-20261105-upstream1/`；浏览器证据：`output/playwright/upstream-20261105/`。回退只还原程序，继续恢复持久计费任务并保留所有新旧账本，不能恢复旧数据库覆盖新增消耗。
