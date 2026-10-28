# 历史额度对账与追加纠正

## 目标与边界

对已证明重复扣记的历史增量追加负差额，不删除或修改 `coding_quota_requests` 的原始扣点、请求时间、期初记录及已确认重置事件。有效用量为原始扣点加对应纠正差额，纠正始终归属原请求所在个人周期。不会因为执行对账而重启七天周期，也不改变 Grok／API 实验的计费规则。

第一版只自动处理**同一官方窗口内，百分比回退后恢复到原最高值以内**的完整错误结算批次。要求：

- 三段官方读数已处理，窗口结束时间处于原有五分钟容差内。
- 没有重叠的已确认全体重置、本地重置或同秒冲突读数。
- 能找齐该恢复段的原已结算请求；原始扣点合计严格等于恢复增量，逐条符合既有权重分配及八位小数余数规则。
- 请求属于同一结算批次，没有改用导入期初、未知估算或后来的结算批次凑数。

恢复中同时含有真实新增消耗、证据不完整或批次不一致时只输出待复核原因。不会根据“涨得太快”主观退款，也不会伪造逐请求的官方扣点能力。

## 可追溯机制

256 迁移只增加通用结构，不包含生产账号、日期或退款种子：

- `coding_quota_reconciliations`：稳定事件 ID、算法版本、母账号、审阅清单摘要、真实操作人标识、原因、三段读数及逐条差额证据。
- `coding_quota_corrections`：关联原请求行，保留原点数和负差额。第一版只允许取消被完整证明重复扣记的点数；请求唯一约束阻止同一条记录重复退款。
- 两表通过触发器禁止 UPDATE/DELETE。业务恢复必须保留两表及其关联请求，不能只恢复旧钱包。

学员卡、管理员用量监控及鉴权继续共用 `ReadCodingQuotas`。累计样本倍率也扣除已审计差额，避免错误历史继续放大新估算。原始历史统计和费用日志仍保留原值，点数纠正不冒充美元账单退款。纠正执行后，回滚程序也必须支持纠正表；不能换回忽略差额的旧程序，让已纠正的扣点重新出现。

## 操作流程

`backend/cmd/quota-audit` 默认只读，不进入应用启动、后台轮询或定时任务。数据库连接从 `SUB2API_QUOTA_AUDIT_DSN` 环境变量读取，禁止把连接口令放进文档、清单或版本库。

```text
quota-audit -account <母账号ID> -from <RFC3339起点> -to <RFC3339终点> -out preview.json
quota-audit -snapshot snapshot.json -account <母账号ID> -from <起点> -to <终点> -out preview.json
quota-audit -apply -plan preview.json -digest <输出的SHA256> -actor <实际操作人> -reason <原因与证据来源> -out applied.json
```

窗口最长 31 天，终点须至少早于现在 30 秒；数据库导出采用只读、可重复读事务，并对单类记录设置 200,000 行上限。超出时缩小范围，不能通过截断结果产生退款。

执行前备份数据库、保存只读快照和清单。写入时取得与正常计费／结算相同的母账号事务锁，再从当前数据库重建完整清单并比较摘要；证据变化、清单被编辑或缺少操作人／原因时拒绝。整个批次原子提交；重试同一清单返回新增 0 条，不重新计算一个以部署时间为起点的事件。

## 本轮只读发现

2026-10-28 13:11 北京时间的快照包含 1,253 条官方观察及 2,109 条请求记录：

- Codex 母账号 5 的账本后基线为 57%，当前读数 73%，净增 16 点；原已结算记录合计 17 点。
- 同窗 `69 → 68 → 69` 的错误恢复段对应原请求行 1435–1441，均属于用户 3，同一结算事务，共计重复 1 点。
- 只读重放生成 1 个可证明事件、7 条纠正分录、0 个待复核区间，摘要为 `4febc987033d02e44431e580e8c391c8167546ae1ed0abe8bfa7c2b3e26c43bd`。摘要包含逐条原请求的时间、权重、批次及原扣点，执行前会重新从数据库核对。
- Grok 母账号 14 的读数 25% → 26%，已结算合计 1 点；本轮没有生成纠正。账本启用前的 imported 期初记录不在本轮自动修正范围。

## 回归要求

配置隔离 PostgreSQL 的 `SUB2API_QUOTA_TEST_DSN` 后实际执行：

```text
go test -race ./internal/repository -run TestCodingQuotaDB -count=1
go test -race ./internal/quotaudit -count=1
```

覆盖原账本不变、多用户退款、当前窗口倍率、真实耗尽拦截、证据变化、篡改清单、重复／并发执行、审计记录不可改写、迁移重放和新连接重启后的有效用量，以及纠正不跨原个人周期生效。另运行全量后端测试、Go vet、增量 lint 与生产构建；已有全量测试失败需按名称报告，不能描述为全量通过。

2026-10-28 本轮验证：隔离 PostgreSQL 下 `go test -p 2 -race ./internal/quotaudit ./internal/repository -run 'TestPlanRecovery|TestCodingQuotaDB' -count=1 -v` 共 39 个测试／子测试通过、0 跳过，其中数据库测试／子测试 29 个。全量 `go test -p 2 ./...` 仍有 [并行计量修复记录](CONCURRENT-QUOTA-METERING.md#验证) 列出的 9 个既有失败，本轮未新增失败：

- `TestAuthIdentityFoundationSchemas`
- `TestAllPlatformsIncludesEveryConcretePlatform`
- `TestPgDumperHoldsMigrationLockThroughReaderClose`
- `TestPgDumperReleasesMigrationLockWhenProcessFails`
- `TestPgDumperReportsUnlockFailureAndDiscardsConnection`
- `TestUsageLogRepositoryCreateSyncRequestTypeAndLegacyFields`
- `TestUsageLogRepositoryCreate_PersistsServiceTier`
- `TestPrepareUsageLogInsert_PersistsNativeCompactionV2WithoutChangingRequestType`
- `TestCompositeGroupSchedulerHasAllCanonicalPlatformBuckets`
