# 额度观测、摘要查询与平滑退出升级

本轮落实服务检查中确认的三个后端问题。沿用 Go、PostgreSQL 及既有前端，不增加新面板，不修改个人周期、官方增量分摊、历史账本及实验计费。Codex/Grok 继续 shadow；历史待核实记录不通过删除、释放或退款来消除。

## 官方观察新鲜度

`coding_quota_meters.observed_at` 是结算区间起点；百分比持平时推进该时间会遗漏尚未观察到增量的请求。因此保留这个字段原有语义，准入和监控另外从持久观察表读取同账号、同用途、同窗口、已处理且等于当前高水位的最新观察时间。未处理、低于高水位、其他窗口和超出未来一分钟的观察均不能刷新可信度。

这次没有新增迁移，也没有回写计量基线或原始请求。官方读取仍由已有工作进程执行，页面读取不增加上游探测。

准入原因细分为 `window_unknown`、`observation_stale`、`rate_unknown`、`insufficient_samples`、`estimate_unknown`、`uncertain_inflight`；待核实和真正耗尽继续优先表达。配置 `SUB2API_CODING_QUOTA_MIN_SAMPLES`、`SUB2API_CODING_QUOTA_MAX_AGE_SECONDS` 在公共预留入口应用，避免 WebSocket/语音绕过自定义可信度门槛。

监控里的倍率、样本、模式仍表示最近一次准入的证据；`observed_at` 使用当前可信观察。新观察不会把当时未知输出或样本不足的准入追认为可靠。Codex OAuth 输出上限未知的兼容限制保留。

## 摘要批量查询

所有用户、用途的预留、计费任务、纠正及最近准入在一条 SQL 中批量聚合。单独调用摘要接口时为一次个人账本查询加一次摘要查询；监控与学员卡已经读取账本时，使用可选接口直接复用，同一参考时刻完成状态组装。旧接口与第三方实现保持兼容。

保留原统计范围：在途估算与待核实数量按原个人周期，活跃执行槽及未处理计费任务跨周期保留；纠正仍归原请求周期。预留不加入学员百分比，学员响应不增加点数或管理员诊断。

查询次数回归覆盖 1 / 100 名用户；实际 PostgreSQL 回归覆盖新鲜度、原周期计数、监控不改变学员百分比及明细分页。

2026-10-29 对线上 6 名用户、12 个用途窗口执行 12 次只读 `EXPLAIN ANALYZE`，新批量 SQL 执行时间 15.753–23.530ms，规划时间 8.416–12.639ms，没有触发 JIT。这是数据库查询测量，不是 API 或公网模型延迟，也不是固定条件下的新旧端到端对照。

## 平滑退出

SIGINT/SIGTERM 后进入进程级排空：

1. 新 HTTP 请求返回 `503 SERVER_DRAINING` 与 `Retry-After: 5`，监听端口在等待期间保持开放。
2. 已有 HTTP 请求继续执行；已升级的 WebSocket 按逻辑轮次跟踪，覆盖 Codex 原生／桥接与 Grok 语音。拒绝新轮次独立于 quota 的 off/shadow/enforce 模式。
3. 活跃轮次允许完成并交接计费，随后关闭空闲连接；尚未接入轮次管理的其他升级协议使用完整等待期限，不直接当作空闲关闭。
4. 正常等待默认 90 秒，可用 `SUB2API_SHUTDOWN_GRACE_SECONDS` 设置 1–3600 秒。到期后取消请求上下文并关闭连接，再留 15 秒给脱离客户端取消的计费交接及请求收尾。
5. 请求收尾后才停止额度恢复工作进程，随后执行现有依赖清理。已持久任务仍可重启后幂等恢复，未知消耗仍为待核实，不按超时伪造零消耗。

Compose 模板停止期限设为 120 秒，覆盖默认 90 秒排空、15 秒交接和最多 10 秒依赖清理；自定义更长排空时必须相应延长 Docker/发布脚本期限。单实例发布仍有重启窗口，不承诺零停机。

Grok `response.done` 在转发给客户端之后才结束该轮，避免排空关连接抢在终止帧之前。通用响应包装保留 streaming、Hijack、ResponseController、CloseNotify 与 HTTP/2 Push 能力。

首次由旧程序升级到此版本时，旧进程仍然采用原来的五秒退出逻辑；发布前应观察在途请求，不能声称新程序已经保护了旧进程的退出。

## 验证

- 先复现失败，再修复：持平观察被判陈旧、摘要引用旧时间、学员卡重复读取账本、协议入口未应用可信度配置、关闭监听导致排空期间连接失败，以及禁用额度时语音新轮次未被排空拒绝。
- 隔离 PostgreSQL `go test -race ./internal/repository -run TestCodingQuotaDB -count=1`：56 项测试／子测试通过，0 跳过，包含补充的跨周期执行槽／计费任务与金额隔离用例。
- `-race -tags unit` 的排空、HTTP、额度、Grok 语音和 WebSocket 专项：101 项测试／子测试通过，0 跳过。
- 默认全量 Go 改动前 13,887 个通过事件、60 个跳过；改动后配置本地额度 DSN，13,956 个通过事件、15 个跳过。两次均为相同 9 个失败根测试，无新增失败。跳过数量变化来自实际运行额度数据库测试，没有用跳过代替额度通过。
- Go vet 与所有本轮变更包的增量 lint 通过，0 issues；全仓 lint 的新报错已逐项修正。生产构建和发布核对另据实际执行结果补记。
- 本轮没有修改前端源代码，也没有重复运行前端全量测试或浏览器全矩阵；发布保留已验证的同一组前端构建资源。

9 项既有失败为：`TestAuthIdentityFoundationSchemas`；`TestAllPlatformsIncludesEveryConcretePlatform`、`TestCompositeGroupSchedulerHasAllCanonicalPlatformBuckets`；`TestPgDumperHoldsMigrationLockThroughReaderClose`、`TestPgDumperReleasesMigrationLockWhenProcessFails`、`TestPgDumperReportsUnlockFailureAndDiscardsConnection`；`TestUsageLogRepositoryCreateSyncRequestTypeAndLegacyFields`、`TestUsageLogRepositoryCreate_PersistsServiceTier`、`TestPrepareUsageLogInsert_PersistsNativeCompactionV2WithoutChangingRequestType`。分类及既有基线见 [并发额度验收](QUOTA-CONTROL-ACCEPTANCE.md)。

本地检查、失败复现、竞态结果、SQL 测量保存在忽略目录 `output/service-audit-20261029/`。发布记录另附；不能把本地检查等同于已部署。

## 回退

保留当前数据库、全部新旧账本、重置、纠正和持久任务，回退仅替换程序及相应配置。此版本没有数据库迁移。关闭新准入仍需继续运行恢复和结算工作进程；不可恢复旧备份覆盖上线后的消耗。业务规则以 [重置规范](CODEX-GLOBAL-RESET-RUNBOOK.md) 为准。
