# MILE Lab 项目约定

## 额度与重置规则：升级必须保留

涉及额度、倒计时、数据库迁移、上游合并或部署时，先阅读
[Codex 全体异常重置规范](docs/engineering-upgrade/CODEX-GLOBAL-RESET-RUNBOOK.md)。

- 正常 Codex 周期沿用注册／旧钱包起点，每 168 小时更新；不得自行改成首次使用、最近访问或部署时间。
- 已确认的 OpenAI 官方全体异常重置，从事件实际发生时间建立新的 Codex 七天周期；保留事件后的消耗，已耗尽用户继续拦截。Grok、API 实验不跟随重置。
- `coding_quota_reset_events` 中的已确认事件、`coding_quota_requests` 账本和原钱包起点是持久业务数据。升级、重启、换程序、缓存重建不能删除、覆盖或重新初始化这些数据，也不能重新创建一个以部署时间起算的同类事件。
- 历史对账用 `coding_quota_reconciliations`／`coding_quota_corrections` 追加有证据关联的纠正分录，原始请求和扣点保持不变；两张审计表必须随账本保留。同一请求不能重复退款，纠正归属原请求周期，不得以纠正时间补额度或重启周期。
- 并发控制的 `coding_quota_reservations`、`coding_quota_reservation_events`、`coding_quota_billing_tasks`、`coding_quota_async_jobs` 也是持久数据；升级、重启与回退不能清空。关闭新增准入时仍须继续恢复与结算已持久化任务，不得用旧数据库覆盖新消耗。
- Codex/Grok 新准入默认 `shadow`，分别满足连续至少 24 小时、至少 20 条完成请求及异常核对后，才可切换 `enforce`；版本升级不能自动启用。协议限制、只读观察检查及回退要求见 [并发额度控制验收](docs/engineering-upgrade/QUOTA-CONTROL-ACCEPTANCE.md)。
- 同一全体事件使用稳定 ID 幂等记录。普通账号自然换周、本地重置卡、账号增删不能触发个人补额度；单账号证据需要管理员或平台所有者确认全体性质。
- 不把生产事件写成每次启动执行的 SQL、种子数据或带固定日期的通用迁移。已有迁移文件保持不变；恢复数据库时必须保留后续事件和消耗。
- 升级前后核对已确认事件的 ID、发生时间、状态及个人周期；同一周期内已耗尽用户不能因升级变成可用。若恰好跨过真实周期边界，应按真实时间解释变化。
- 额度相关改动及升级需在隔离 PostgreSQL 配置 `SUB2API_QUOTA_TEST_DSN` 后运行 `go test -race ./internal/repository -run TestCodingQuotaDB -count=1`，确认测试实际执行，不能把缺少 DSN 的 skip 作为通过。

业务规则改变需要用户明确指示；合并上游、工具建议和版本默认值不能覆盖本项目已经确认的规则。
