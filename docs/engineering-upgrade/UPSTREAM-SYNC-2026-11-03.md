# 2026-11-03 上游安全合并

用户要求将已经核对的官方 main 安全合并到 fork。本次只进行代码合并、验证与推送；
没有部署程序、访问生产数据库、修改线上配置或确认额度事件。

## 固定范围与历史

- 合并前 fork：`eb4289dcb35d45554ad9253ea6fd8233b21e8aed`，分支 `lab/0.2.17`。
- 上次已纳入上游：`d851a38477597157e78450d30af2d8504764d085`，已验证为本次 fork 的祖先。
- 本次固定目标：`4a4a83ee5ec86b19d3a2dd9b6471de5128963bec`；fresh fetch 后与官方 main 一致。
- 新增 4 个上游提交（3 个功能／修复提交、1 个 PR 合并提交），涉及 65 个文件。
- 最新已获取正式标签仍为 `v0.2.19`，目标包括该标签之后的主线提交。
- 在独立 worktree 使用 `git merge --no-ff --no-commit <固定目标>`；无文本冲突。
  最终双亲 merge commit 为 `1069204ebfc2313e5bd45f926ce24bf6beb43f4a`，父提交分别为上述 fork 和固定目标；未 squash 上游历史。

## 纳入的更新

来自官方 [PR #8476](https://github.com/Wei-Shaw/sub2api/pull/8476)：
补充管理员 Key 的账户身份、用量、统计、分组、OpenAI 额度／邀请等细分权限；
新增需显式确认的全权兼容模式，默认仍关闭；补齐中英文权限展示并修正 ES2020 类型兼容；
将 `follow-redirects` 从 1.16.1 更新至 1.16.2。
没有新增模型定义，没有新增数据库迁移或 Ent schema 改动。

## 定制保留与自动合并审查

相对上次上游基线，共 467 个定制路径；其中 462 个路径的 Git blob 原样保留。
额外核对的 522 个重点文件全部保持原 blob，包括迁移、额度账本／容量投影、官方点数服务、
学员页面、用户组件、公共布局、品牌资源、样式以及现有管理员用量监控。
既有迁移内容和文件名均未变化，没有重新生成 Ent。

仅有以下 5 个重叠路径，由 Git 自动合并后逐项核对：

| 路径 | 合并结果 |
| --- | --- |
| `backend/internal/handler/admin/user_handler.go` | 纳入新的受限／全权权限判断；保留手动周重置仍对齐注册／原设置的个人周起点逻辑。 |
| `backend/internal/server/middleware/audit_log.go` | 新增管理员 Key 全权状态审计白名单；保留三种周导出敏感读取审计。 |
| `backend/internal/service/openai_quota_service.go` | 纳入账户范围及影子母账号权限检查；保留 `codex_local_reset_at` 标记，避免本地用卡被误当全体重置。 |
| `frontend/package.json` | 保留 MILE 的现有工程配置，更新指定传递依赖约束。 |
| `frontend/pnpm-lock.yaml` | 冻结安装验证，跟随 `follow-redirects` 1.16.2，不做大版本工具链迁移。 |

原 Codex 50／Grok 30 默认上限、个人七天和 API 实验月周期、已确认全体重置、容量统一口径、
纠正、预留与持久计费任务均未改动。新版本也不自动开启 enforce。
本次没有通过整文件 ours/theirs 或复制官方目录覆盖定制。

## 合并前失败基线

前端 398 个文件、3,175 项测试通过。
默认 Go 全量在固定合并前 SHA 实际运行：14,183 个测试／子测试通过、15 个跳过。
以下 9 个既有确定性失败根测试再次复现，分类沿用
[可靠性升级](RELIABILITY-UPGRADE-2026-10-29.md)和[并发额度验收](QUOTA-CONTROL-ACCEPTANCE.md)：

- schema 结构断言：`TestAuthIdentityFoundationSchemas`。
- 平台目录／调度桶覆盖：`TestAllPlatformsIncludesEveryConcretePlatform`、`TestCompositeGroupSchedulerHasAllCanonicalPlatformBuckets`。
- pg_dump 子进程／迁移锁测试：`TestPgDumperHoldsMigrationLockThroughReaderClose`、`TestPgDumperReleasesMigrationLockWhenProcessFails`、`TestPgDumperReportsUnlockFailureAndDiscardsConnection`。
- usage_logs SQL 预期：`TestUsageLogRepositoryCreateSyncRequestTypeAndLegacyFields`、`TestUsageLogRepositoryCreate_PersistsServiceTier`、`TestPrepareUsageLogInsert_PersistsNativeCompactionV2WithoutChangingRequestType`。

另外在同一次合并前全量高负载中，`TestGuardEvaluatorFlagSharedDeadlineFailClosedAndContextCancel` 的
`all_failovers_share_first_endpoint_deadline` 子测试期望两次尝试而实际一次；
同一未合并 SHA 随后单独重复三次，12 个测试／子测试通过、无失败或跳过。
这是另记的既有时序波动，不能把合并前结果写成全绿，也不通过放宽安全检查来消除它。

## 验证与独立复核

| 检查 | 实际结果 |
| --- | --- |
| 前端全量 Vitest | 合并后 399 个文件、3,183 项通过；合并前 398 个文件、3,175 项通过。 |
| 类型／国际化／构建 | vue-tsc、只读 ESLint、国际化完整性、生产构建全部通过；等待当前构建进程实际退出 0 后才使用产物。 |
| Go vet／增量 lint | `go vet ./...` 通过；相对合并前 fork 的增量 golangci-lint：0 issues。 |
| 相关 Go unit／race | 管理员权限、路由、处理中间件、账户范围、OpenAI/Grok 额度、用量监控和并发额度专项 532 项通过；初次因缺少 `SCOPED_ADMIN_TEST_DSN` 跳过的 18 个根测试均在下述补跑中实际执行。 |
| 管理员 PostgreSQL 补跑 | 显式配置本地隔离 `SCOPED_ADMIN_TEST_DSN`；仓库 42 项、服务与处理器 15 项通过，零失败、零跳过，覆盖全部初始跳过根测试。 |
| 额度 PostgreSQL 竞态 | 显式配置 `SUB2API_QUOTA_TEST_DSN`，独立运行要求的 `go test -race ./internal/repository -run TestCodingQuotaDB -count=1`：66 项通过，零失败、零跳过。 |
| 默认 Go 全量 | 合并后 14,185 项通过、15 项既有跳过；仍有下述已分类失败，不记为全绿。 |
| 独立代码审查 | 一次 fresh-context 只读分支审查；没有 Critical、Important 或需修改源码的 Minor 问题；后端未额外修改，也未改生产代码掩盖基线失败。移动布局反馈通过下述独立样式提交修复。 |

合并后默认全量再次复现原 9 个确定性失败，另有两项未被本次改动的时序测试：
`TestServerTimingConnectorRecordsDriverCallsWithoutRowLifetime`（线程调度使测量时间断言失败）和
`TestAntigravityCompatHandlerRepeatsPreContentKeepalive`（窗口内保活计数 2，小于期望 3）。
随后使用合并前固定 SHA 的 Go overlay 完整还原 55 个变更 Go 路径，并在合并前、合并后源码下分别单独重复三次：
两边各 6 项通过，零失败、零跳过。合并前出现的 securityaudit 超时波动在合并后全量中未失败。
这些是明确保留的测试时序不稳定性；没有新增确定性失败，没有跳过失败测试来换取通过。

在实际生产前端构建中，用 Chromium、WebKit 检查 375/768/1440px：
全权默认关闭；空／错误确认词不能确认；取消不增加授权；确认后资源规则禁用，关闭全权可恢复规则；
新增读取权限不自动勾选，权限名称使用本地化展示，页面没有整体横向溢出。
另验证动态模型目录的搜索、复制、家族筛选、刷新失败保留列表、76 模型增量展示，
以及容量统一口径的管理员监控和 720 CSS px 等效 200% 布局。未将等效视口当作原生缩放认证；本次未跑 Firefox。

浏览器所有 API 均由只读 mock 拦截，没有保存真实管理员密钥或执行生产操作。
初版夹具漏了支付供应商列表、邮件事件和图片存储配置的返回形状，并未正确标记既有新手引导已读；
这些导致的提示和遮挡已修正夹具后重跑，没有为测试夹具修改生产代码。
运行截图与短录屏位于 `output/playwright/upstream-20261103/`，视觉核对页为 `verify-upstream-20261103`。
独立审查不替代人工视觉判定，页面的最终视觉判定单独保留。

完整执行记录在 `output/upstream-20261103/`，包含固定前后 SHA、默认失败逐项比较、单独重复、实际 PostgreSQL 结果和 Git blob 保留记录。

## 人工移动布局反馈与修正

核对页中，桌面权限组通过；移动组被平台所有者指出“移动布局这里问题太大了”。因此暂停推送，
保留原拒绝截图，重现并区分了两类问题：

- 浏览器夹具在桌面切换到手机宽度后立即截图，抓到了侧栏和主内容 200ms 过渡的中间状态。
  修正夹具，等待侧栏完全离开视口、顶栏回到左边界后再截图；没有为截图修改全站导航行为。
- 真实的窄屏权限行将名称和权限 ID 放在同一行争抢宽度，名称仅有 96px，造成中文碎片化换行。
  独立提交 `22d109355aee093ad7269a0b81c66e86677cf7a5` 调整 `AdminAccessKeysSettings.vue` 的 1024px 以下布局：
  名称与 ID 分行、减少表单层层留白、账户类型两列排布、扩大点击区域，并使保存／取消按钮均分宽度。

没有修改授权逻辑、确认词校验、数据结构、网络请求或已通过的桌面布局。
桌面组件修正前后同为 1120×1870；像素差异只在鼠标悬停的创建按钮区域，权限表单几何保持一致。
375px 实际回归先观察名称宽度 96px、ID 与名称同排的失败，再验证修正后名称宽 154px、ID 置于下方、
触控行高 55.5px；768px 同样通过。两种浏览器均在真实生产构建中验证。

修正后的完整前端再次运行：399 个文件、3,183 项测试通过，类型／只读 lint／国际化／生产构建通过。
这是纯布局修正，以实际浏览器几何和截图完成失败到通过的验证，没有增加仅镜像 CSS 实现的单元测试。
后端没有变化，不重复无关 Go 全量。已更新被拒绝组的证据并回复原线程，桌面通过组文件保持原样。

## 实施取舍与发布边界

- 合并采用固定 SHA，主分支后续继续前进时不悄悄扩大范围；下一次增量合并从本次目标继续。
- 全权兼容模式是用户主动选择的新增管理能力；既有 Key 不自动加权，二次验证、按 Key 限流和审计继续保留。
- 授权策略变更或升级前后，使用同一幂等键重试可能返回冲突，须由调用方处理；不能为兼容成功响应而重复执行财务操作。
- 幂等协调器先按操作范围和键哈希查找，再核对包含调用者／策略的指纹；上述情况返回 `409 IDEMPOTENCY_KEY_CONFLICT`，不会因指纹变化再执行一次余额变更。提现仍沿用自己的持久操作键。
- 全权 Key 有意进入既有自定义管理路由；受限 Key 对这些未登记路由仍默认拒绝。处理器自己的显式确认和持久幂等继续生效。
- 全权 Key 的周导出使用 `admin.api_key.request` 审计，包含准确路径、创建者、Key ID 和全权状态；JWT 仍记录具名周导出动作。审计消费者如仅筛选具名动作，应结合路径／认证方式识别机器请求。
- 本次没有改动旧失败涉及的业务范围，明确保留基线；相关新增失败或回归必须修复后再合并。
- 本次请求是合并到 fork；生产运行态继续使用上次发布。后续上线必须备份并按固定参考时刻核对全部持久数据，不能直接替换为官方镜像。

最后一次已有线上发布记录为 [容量折算发布](QUOTA-CAPACITY-RELEASE-2026-11-02.md)，
`0.2.19-mile.20261102.capacity3`／源码 `e68dd32127dcc4732bef1b4d38d0b56105231f14`。
这段来自已有发布证据，本次没有再次连接生产数据库认证该运行态。
