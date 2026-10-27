# Sub2API 工程升级参考包

整理日期：2026-10-27。目标是支持 MILE Lab 版本的前端设计、后端响应速度与中转分发能力升级。

已筛选 9 个参考项目和 8 个 skills。完整项目索引、源码入口与 skill 来源见 [CATALOG.md](CATALOG.md)，提交版本与下载校验值见 [references.lock.json](references.lock.json)。这些是用于阅读和对比的源码快照，不是本项目的依赖升级版本。

浅色光谱设计系统实现见 [DESIGN-SYSTEM.md](DESIGN-SYSTEM.md)，视觉、业务与性能验收见 [DESIGN-ACCEPTANCE.md](DESIGN-ACCEPTANCE.md)。

当前学员额度公式、重置时钟及已确认问题见 [QUOTA-AUDIT.md](QUOTA-AUDIT.md)。

个人独立周期、Codex 50/Grok 30 点及全体异常重置的改造分析见 [QUOTA-REDESIGN.md](QUOTA-REDESIGN.md)。

工作区实现、迁移步骤、测试记录和计量边界见 [QUOTA-IMPLEMENTATION.md](QUOTA-IMPLEMENTATION.md)。

当前线上版本及历次同步检查见 [DEPLOYMENT-2026-10-27.md](DEPLOYMENT-2026-10-27.md)。下一轮学员接入流程与额度可信度设计见 [CONNECTION-AND-QUOTA-PLAN.md](CONNECTION-AND-QUOTA-PLAN.md)。

## 本地位置与使用

```text
docs/engineering-upgrade/              可随项目提交的索引、版本锁定与升级路线
tools/prepare_upgrade_references.py    下载与完整性验证工具，Python 3.11+
docs-local/upgrade-references/
  archives/                           固定提交版本的 GitHub 源码 ZIP
  projects/                           解压后的 9 个项目，可直接搜索代码
  skills/                             8 个已安装 skills 的本地备份
  skills-installed.json               安装目录、来源与文件指纹
  discovery.json                      GitHub 搜索与仓库元数据原始结果
```

`docs-local/` 已被现有 `.gitignore` 排除，外部源码不会混入业务代码提交。所有原始许可证保留在源码中。Skills 安装到 `C:/Users/huohua/.codex/skills/`，从下一轮对话起可使用；本次不执行参考项目的安装、构建、启动或压测命令。

在仓库根目录执行：

```powershell
# 下载缺失的参考项目，使用锁文件中的版本和 SHA-256
python tools/prepare_upgrade_references.py

# 离线验证 ZIP 校验值及每一个解压文件的大小、CRC
python tools/prepare_upgrade_references.py --verify
```

工具保留已有源码；发现本地修改会报错，不会覆盖。`--record-checksums` 用于首次准备新参考版本时记录校验值，已有校验值仍必须匹配。当前锁文件已经记录校验值，普通下载不需要该参数。

Skills 的锁文件记录上游仓库、路径和提交。迁移到新机器时，可以使用已有 `skill-installer` 的 `install-skill-from-github.py --repo <repo> --ref <commit> --path <path>` 安装；当前机器已安装，无需重复执行。安装器不会覆盖同名目录。`skills/` 备份保留完整参考文件和许可证，后续可离线阅读。

## 当前实现与可借鉴位置

| 方向 | 当前代码 | 参考项目与用途 |
| --- | --- | --- |
| 首页与学员界面 | `frontend/src/components/home/LabHome.vue`、`views/user/DashboardView.vue`、`components/user/dashboard/StudentQuotaRuler.vue` | shadcn-vue 的基础控件、Reka UI 的交互与可访问性、Vben 的控制台布局 |
| 前端工程 | `frontend/src/components/common/`、`composables/`、`stores/`、`vite.config.ts` | Vue skills 的状态归属、组件边界与异步测试；Vben 的主题变量和复杂表格组织 |
| 调度选择 | `backend/internal/service/openai_account_scheduler.go`、`scheduler_snapshot_service.go` | GPT Load 的无 IO 调度与公平性测试；Bifrost 的治理、路由和流式回退测试 |
| 通道与协议 | `backend/internal/server/routes/gateway.go`、`internal/service/*gateway*` | New API 的通道选择、协议适配与亲和性；GPT Load 的能力匹配和执行预算 |
| 连接复用 | `backend/internal/pkg/httpclient/pool.go`、`internal/repository/req_client_pool.go`、`internal/service/openai_ws_pool.go` | 对比 Bifrost/GPT Load 的客户端生命周期、取消传播与长连接资源管理 |
| 并发与过载 | `backend/internal/service/concurrency_service.go`、`internal/repository/concurrency_cache.go` | failsafe-go 的 bulkhead、重试预算、熔断和限流策略 |
| 用量写入 | `backend/internal/service/usage_record_worker_pool.go` | 结合 Go skill 检查队列等待、同步回退与退出时的计费一致性 |
| 可观测性 | `backend/internal/server/middleware/server_timing.go`、现有调度指标与 benchmark | Prometheus 的 Histogram 和指标注册；k6 的负载模型与统计 |

现有后端已经实现缓存、快照、Top-K、连接池及计费任务池。这里的入口是优化候选位置，不代表已经证实存在性能问题。

## 前端升级：先形成统一设计系统

建议优先做仪表盘、API Key 列表、模型列表三类页面，再推广到管理后台。MILE Lab 面向学员，额度、重置时间、可用模型与下一步操作应有清晰的信息层级。

1. 梳理当前公共组件与样式，定义颜色、字号、间距、圆角、焦点与状态色变量，统一亮暗主题。保留实验室品牌与中文内容的可读性。
2. 参考 shadcn-vue 的 Button、Input、Dialog、Table、Tabs、Tooltip，先完善已有公共组件。复杂弹层、键盘导航、焦点管理可以评估 Reka UI，避免同时引入多套组件体系。
3. 从 Vben 借鉴侧栏、导航、表单反馈、数据表格和主题配置，按当前路由及权限实现适配。它的 monorepo 和完整权限系统不需要整体迁入。
4. 补齐加载、空数据、失败重试、禁用、长文本、复制反馈等状态。页面展示服务端返回的额度语义，不能把未知官方读数渲染成 0%。
5. 用现有 Vitest/Vue Test Utils 验证行为，用已安装的 Playwright skill 做浏览器检查；至少检查 375、768、1440 宽度、亮暗模式、键盘操作与中文长标签。

**版本边界：** 当前项目是 Tailwind 3、Vite 5、Vitest 2；下载的 shadcn-vue v4 示例使用 Tailwind 4，Vben 快照使用 Tailwind 4 / Vite 8。先复用设计与组件职责，具体代码需按当前依赖调整。工具链迁移应独立安排，不能直接复制它们的 `package.json`。因此本次没有安装面向 Vite 8 / Vitest 4 的最新 antfu skills。

前端验收记录首屏资源、路由懒加载、长列表滚动、重复请求与视觉截图。外观调整不能导致列表筛选、权限、额度和错误反馈退化。

## 响应速度：建立基线后优化

分别观察管理接口、网关自身处理、上游首 token 与流式完成。上游模型生成时间不能全部计入网关开销；微基准也不能代表端到端吞吐。

| 测量对象 | 应记录的数据 |
| --- | --- |
| 管理接口 | p50/p95/p99、SQL 次数与耗时、Redis 次数与耗时、返回体积 |
| 网关调度 | 鉴权、候选过滤、快照读取、并发槽等待、选择耗时、候选规模 |
| 上游连接 | DNS/TCP/TLS、连接复用、等待响应头、上游首 token |
| 流式转发 | SSE/WS 吞吐、首 token、取消后的资源释放、慢客户端背压 |
| 写入与资源 | 计费队列长度与同步回退、CPU、内存、GC、goroutine、连接池等待 |

管理页面已有可配置 `ENABLE_SERVER_TIMING`，适合拆分 SQL/Redis 开销；该中间件有作用域限制，不能把它当成覆盖所有网关请求的链路追踪。网关需要结合现有 first-token 和调度指标补齐分阶段测量。

可以先复用仓库已有的微基准，不必新造同类测试。从 `backend/` 执行以下命令可测候选选择与请求解析；本轮没有运行这些业务基准：

```powershell
go test ./internal/service -run '^$' -bench 'Benchmark(OpenAIAccountSchedulerSelectTopK|ParseGatewayRequest_)' -benchmem -count=5
go test ./internal/repository -run '^$' -bench '^BenchmarkHTTPUpstreamProxyClient$' -benchmem -count=5
```

`BenchmarkAccountConcurrency` 会连接 Redis，应放到独立测试 Redis 执行。连接池微基准测对象构建/复用开销，不包含实际 TCP/TLS 往返。结果需记录 Go 版本、机器、数据规模和并发数，再做优化前后对照。

根据证据选择改动：数据库慢则查 `EXPLAIN (ANALYZE, BUFFERS)` 和索引；Redis 往返多则评估批量与缓存；分配高则用 pprof 定位解析/缓冲；并发槽等待高则检查容量和槽位生命周期。调整后同时观察 p99 和错误率，不能只看平均延迟。

## 中转分发：公平、故障恢复与长连接正确性

优先阅读 GPT Load 的 `internal/scheduler/`、`internal/state/`、`internal/gateway/` 与测试，Bifrost 的 `core/streamfallback_test.go`、`plugins/governance/loadbalance_test.go`，以及 New API 的 `service/channel_select.go` / `channel_affinity.go`。

1. **公平与容量：** 测试多用户、多账号、不同并发上限与不同请求长度。观察成功吞吐、排队时间、负载倾斜和饥饿，检查批量获取槽位的收益。
2. **健康与路由：** 区分账号健康、模型可用性、限流冷却、协议能力、额度和会话亲和性。先核实现有实现，再评估熔断和半开恢复是否能改善长尾。
3. **重试预算：** 区分 429、5xx、网络失败与不可重试的参数错误；结合 `Retry-After`、带抖动退避、总时间预算和排除已失败账号，避免多层重试叠加。
4. **流式边界：** 已向客户端发送部分结果后通常无法透明重放；Responses 的状态、`previous_response_id`、工具执行与 WS 会话归属会限制换号。回退必须按协议和提交状态判断。
5. **背压与退出：** 慢上游、慢客户端、客户端取消、Redis 故障、队列满和服务退出都要覆盖。不能通过丢失计费任务换取更高吞吐。
6. **缓存边界：** 配置、定价、模型目录等元数据适合评估缓存；模型响应缓存需要单独设计租户隔离和语义，不能直接沿用普通 API 缓存策略。

分发验收先使用可控 mock 上游，注入延迟、429、5xx、断流与取消，记录成功请求数、p95/p99、首 token、重试放大系数、资源回收和计费一致性。k6 普通 HTTP 请求耗时包含流式响应完成时间，默认并不等于首 token 时间；SSE/WS 的 TTFT 应专门测量或使用服务端指标。

## 建议实施顺序

| 顺序 | 具体交付 | 完成依据 |
| --- | --- | --- |
| 1 | 前端设计变量、公共组件规范；后端性能基线 | 三个代表页面的状态/截图清单；固定环境的延迟与资源报告 |
| 2 | 仪表盘、Key/模型页逐页升级；修复已测得的热点 | 页面行为测试通过；同等负载下 p95/p99 与错误率对照 |
| 3 | 账号调度、公平性、重试与故障恢复增强 | mock 故障矩阵、长连接会话与计费一致性检查通过 |
| 4 | 扩容验证与发布 | 多实例、容量极限与回滚记录；性能目标根据基线确定 |

参考项目的星数只作为维护与生态的筛选信号。Bifrost 等项目 README 中的性能宣传是其自身环境下的结果，不代表 Sub2API 当前或未来实测性能。

许可证与来源按项目保留。New API 使用 AGPL-3.0，且 `NOTICE` 含额外署名要求，直接复用代码时需一并处理；k6 在这里作为独立压测工具参考。MIT / Apache 项目也需保留适用的版权和 NOTICE。各项目的许可证不替代具体文件及其依赖的许可证。
