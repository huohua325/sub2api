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

## 线上执行与验收

2026-10-28 13:39 北京时间已部署到 `172.104.117.114`，版本 `0.2.18-mile.20261028.history1`，源码提交 `7b1fb0a5ac95f3ea66f47b4ec793ad02e5b862b6`。生产构建 SHA-256 为 `f36cffbfa11b36e2946675ee270e52b67c6e7c6235abad49c4784c968cbd3c16`；Go vet 通过，repository／quotaudit／操作工具的增量 golangci-lint 为 0 issues。

先在生产库只读重建清单，确认摘要与审阅版本完全相同，再执行一次追加纠正：

- 生成 1 个审计事件、7 条纠正分录，有效差额合计 `-1.00000000`，归属原请求用户 3 的原个人周期。
- 原请求 1435–1441 保持原始点数、权重、时间和状态；合并对应差额后，该批次有效扣点为 0。
- 对同一清单重试，新增分录为 0；没有重复返还。
- 保留全部 2,389 条原请求，2,245 条已结算／期初记录逐字段保持原值。校准只更新当前母账号的待结算估算，不改历史结算。
- 全部重置事件、原钱包起点、个人周期及其他用户已结算用量保持不变。用户 6 已消耗 `51.29750000`，仍超过 50 点拦截阈值；没有因升级恢复可用。
- Grok 用量及其他母账号计量状态保持不变；没有调整 API 实验账单。个人 Codex 下一周期仍为 `2026-11-02 17:29:05 UTC+8`。
- 共 310 个迁移，256 已实际安装。应用重新启动后再次查询，7 条差额及原请求、事件、起点仍保留。
- 18 个公网前端资源逐一与原生产资源匹配；`/health`、`/login`、公开设置均返回 200，MemSlides 中继健康。此次没有新增前端面板，也没有使用学员 Key 发送付费模型请求。

备份均留在服务器的 root 私有目录，并已通过 `pg_restore --list` 检查；未进行完整恢复演练：

| 阶段 | 目录 | 数据库备份字节 |
| --- | --- | --- |
| 程序切换前，含原程序与配置 | `/opt/sub2api/backups/before-history-quota-20261028T053921Z` | 3,698,785 |
| 追加纠正前，含审阅清单与前后快照 | `/opt/sub2api/backups/before-history-credit-20261028T053952Z` | 3,706,293 |
| 纠正后，含已生效的纠正账本 | `/opt/sub2api/backups/after-history-credit-20261028T054122Z` | 3,707,741 |

发布清单、只读计划、部署检查、纠正结果、重启核对及公网资源检查保存在本地忽略目录 `output/releases/mile-20261028-history1/`；服务器发布目录为 `/opt/sub2api/releases/mile-20261028-history1/`。旧备份不包含之后产生的消耗及纠正，不能覆盖当前库来回退程序；恢复必须保留后续业务账本，并使用支持纠正表的程序。
