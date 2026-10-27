# 参考项目与 Skills 索引

准备完成：2026-10-27。GitHub 元数据为研究时的快照；精确提交、归档 SHA-256、文件数与来源见 [references.lock.json](references.lock.json)。升级建议见 [README.md](README.md)。

## 已下载的 9 个项目

| 项目与上游 | 研究时 Stars | 许可证 | 本地用途 |
| --- | ---: | --- | --- |
| [unovue/shadcn-vue](https://github.com/unovue/shadcn-vue) | 10,736 | MIT | 前端控件与视觉规范 |
| [unovue/reka-ui](https://github.com/unovue/reka-ui) | 6,905 | MIT | 无样式交互、键盘与焦点管理 |
| [vbenjs/vue-vben-admin](https://github.com/vbenjs/vue-vben-admin) | 33,678 | MIT | 管理台布局、主题、表格与表单组织 |
| [maximhq/bifrost](https://github.com/maximhq/bifrost) | 9,118 | Apache-2.0 | Go AI 网关、流式回退与分发治理 |
| [QuantumNous/new-api](https://github.com/QuantumNous/new-api) | 50,730 | AGPL-3.0 | 多通道协议适配、通道选择与亲和性 |
| [tbphp/gpt-load](https://github.com/tbphp/gpt-load) | 7,171 | MIT | Go 多密钥分发、公平调度与重试预算 |
| [failsafe-go/failsafe-go](https://github.com/failsafe-go/failsafe-go) | 2,258 | MIT | Go 重试、隔舱、熔断与容量控制 |
| [prometheus/client_golang](https://github.com/prometheus/client_golang) | 6,042 | Apache-2.0 | Go 指标采集与直方图 |
| [grafana/k6](https://github.com/grafana/k6) | 32,054 | AGPL-3.0 | 独立运行的负载测试工具参考 |

ZIP 合计约 641.3 MiB，解压源码共 30,697 个文件。所有源码均已下载到本机，以下链接指向固定提交的本地副本。

### 前端设计与工程

- **shadcn-vue**：提交 `de1f76cbb4b3`；[本地说明](../../docs-local/upgrade-references/projects/unovue--shadcn-vue/README.md)、[Vue 控件示例](../../docs-local/upgrade-references/projects/unovue--shadcn-vue/apps/v4/registry/new-york-v4)、[样式](../../docs-local/upgrade-references/projects/unovue--shadcn-vue/apps/v4/styles)、[旧版参考](../../docs-local/upgrade-references/projects/unovue--shadcn-vue/deprecated)。新版示例需要按当前 Tailwind 3 适配。
- **Reka UI**：提交 `27e13d655e1e`；[本地说明](../../docs-local/upgrade-references/projects/unovue--reka-ui/README.md)、[交互组件源码](../../docs-local/upgrade-references/projects/unovue--reka-ui/packages/core/src)、[文档](../../docs-local/upgrade-references/projects/unovue--reka-ui/docs)。适合研究 Dialog、Popover、Select 等控件的键盘与焦点处理。
- **Vben Admin**：提交 `2392c1bb1b1c`；[中文说明](../../docs-local/upgrade-references/projects/vbenjs--vue-vben-admin/README.zh-CN.md)、[核心组件](../../docs-local/upgrade-references/projects/vbenjs--vue-vben-admin/packages/@core)、[样式](../../docs-local/upgrade-references/projects/vbenjs--vue-vben-admin/packages/styles)、[应用示例](../../docs-local/upgrade-references/projects/vbenjs--vue-vben-admin/apps)。参考布局和组织方式，工具链单独评估。

### 网关分发

- **Bifrost**：提交 `85d8a5225b20`；[本地说明](../../docs-local/upgrade-references/projects/maximhq--bifrost/README.md)、[核心](../../docs-local/upgrade-references/projects/maximhq--bifrost/core)、[治理模块](../../docs-local/upgrade-references/projects/maximhq--bifrost/plugins/governance)、[流式回退测试](../../docs-local/upgrade-references/projects/maximhq--bifrost/core/streamfallback_test.go)、[负载分发测试](../../docs-local/upgrade-references/projects/maximhq--bifrost/plugins/governance/loadbalance_test.go)。
- **New API**：提交 `34003e9c2eed`；[本地说明](../../docs-local/upgrade-references/projects/QuantumNous--new-api/README.md)、[协议适配](../../docs-local/upgrade-references/projects/QuantumNous--new-api/relay/channel)、[通道选择](../../docs-local/upgrade-references/projects/QuantumNous--new-api/service/channel_select.go)、[亲和性](../../docs-local/upgrade-references/projects/QuantumNous--new-api/service/channel_affinity.go)、[NOTICE](../../docs-local/upgrade-references/projects/QuantumNous--new-api/NOTICE)。
- **GPT Load**：提交 `6548ee0ed4cb`；[中文说明](../../docs-local/upgrade-references/projects/tbphp--gpt-load/README_CN.md)、[调度与公平性测试](../../docs-local/upgrade-references/projects/tbphp--gpt-load/internal/scheduler)、[状态快照](../../docs-local/upgrade-references/projects/tbphp--gpt-load/internal/state)、[网关执行与重试测试](../../docs-local/upgrade-references/projects/tbphp--gpt-load/internal/gateway)、[健康管理](../../docs-local/upgrade-references/projects/tbphp--gpt-load/internal/health)。

### 性能与可靠性

- **failsafe-go**：提交 `2942d1ad23ef`；[本地说明](../../docs-local/upgrade-references/projects/failsafe-go--failsafe-go/README.md)、[重试](../../docs-local/upgrade-references/projects/failsafe-go--failsafe-go/retrypolicy)、[预算](../../docs-local/upgrade-references/projects/failsafe-go--failsafe-go/budget)、[并发隔舱](../../docs-local/upgrade-references/projects/failsafe-go--failsafe-go/bulkhead)、[熔断](../../docs-local/upgrade-references/projects/failsafe-go--failsafe-go/circuitbreaker)、[自适应限流](../../docs-local/upgrade-references/projects/failsafe-go--failsafe-go/adaptivelimiter)。
- **Prometheus Go client**：提交 `595b65527a90`；[本地说明](../../docs-local/upgrade-references/projects/prometheus--client_golang/README.md)、[指标实现](../../docs-local/upgrade-references/projects/prometheus--client_golang/prometheus)、[示例](../../docs-local/upgrade-references/projects/prometheus--client_golang/examples)。
- **k6**：提交 `8ba085f9e15c`；[本地说明](../../docs-local/upgrade-references/projects/grafana--k6/README.md)、[压测示例](../../docs-local/upgrade-references/projects/grafana--k6/examples)、[WebSocket](../../docs-local/upgrade-references/projects/grafana--k6/examples/websockets)、[浏览器](../../docs-local/upgrade-references/projects/grafana--k6/examples/browser)。本次下载源码，没有安装 k6 可执行程序。

## 已安装的 8 个 Skills

从下一轮对话起可使用。安装位置是 `C:/Users/huohua/.codex/skills/`；表中的 skill 名称链接指向项目内的离线备份。精确来源提交与文件指纹均记录在锁文件中。

| Skill | 来源 | 用途 |
| --- | --- | --- |
| [frontend-design](../../docs-local/upgrade-references/skills/frontend-design/SKILL.md) | [anthropics/skills](https://github.com/anthropics/skills/tree/dbd4588f9e1033efb41dad4bef2f7947c8993d44/skills/frontend-design) | 页面视觉、内容层级、交互与响应式设计 |
| [vue-best-practices](../../docs-local/upgrade-references/skills/vue-best-practices/SKILL.md) | [vuejs-ai/skills](https://github.com/vuejs-ai/skills/tree/198f0cd8f7f9c0a3d9b566ccc16de6677519ee78/skills/vue-best-practices) | Composition API、状态归属、组件职责与性能 |
| [vue-testing-best-practices](../../docs-local/upgrade-references/skills/vue-testing-best-practices/SKILL.md) | [vuejs-ai/skills](https://github.com/vuejs-ai/skills/tree/198f0cd8f7f9c0a3d9b566ccc16de6677519ee78/skills/vue-testing-best-practices) | Vue Test Utils、Vitest 与异步交互测试 |
| [golang-pro](../../docs-local/upgrade-references/skills/golang-pro/SKILL.md) | [Jeffallan/claude-skills](https://github.com/Jeffallan/claude-skills/tree/b803b6ace895a7f8ea619414b59fbdf2aa548a34/skills/golang-pro) | Go 并发、context、pprof 与基准测试 |
| [supabase-postgres-best-practices](../../docs-local/upgrade-references/skills/supabase-postgres-best-practices/SKILL.md) | [supabase/agent-skills](https://github.com/supabase/agent-skills/tree/c9be0e931b7930f7d02126d04774d904c381e7d7/skills/supabase-postgres-best-practices) | PostgreSQL 查询、索引、连接与锁分析 |
| [api-design-principles](../../docs-local/upgrade-references/skills/api-design-principles/SKILL.md) | [wshobson/agents](https://github.com/wshobson/agents/tree/0874a27b17a22e2b7c2e2bcd725768e0803cad06/plugins/backend-development/skills/api-design-principles) | API 合同、分页与错误语义；实现遵循现有 Go/Gin |
| [prometheus-configuration](../../docs-local/upgrade-references/skills/prometheus-configuration/SKILL.md) | [wshobson/agents](https://github.com/wshobson/agents/tree/0874a27b17a22e2b7c2e2bcd725768e0803cad06/plugins/observability-monitoring/skills/prometheus-configuration) | 指标采集、记录规则与监控配置 |
| [slo-implementation](../../docs-local/upgrade-references/skills/slo-implementation/SKILL.md) | [wshobson/agents](https://github.com/wshobson/agents/tree/0874a27b17a22e2b7c2e2bcd725768e0803cad06/plugins/observability-monitoring/skills/slo-implementation) | 延迟、可用性与错误预算 |

安装使用了 [skill-installer](C:/Users/huohua/.codex/skills/.system/skill-installer/SKILL.md)。每个 skill 保留原始 `SKILL.md` 和参考资源；MIT 上游根许可证另存为 `UPSTREAM_LICENSE`，`frontend-design` 自带 Apache-2.0 的 `LICENSE.txt`。

本次只安装选定 skill 目录，没有安装来源仓库的插件、hooks、MCP 服务或其他 agents。项目原有 `playwright` skill 可配合前端浏览器验证。

## 筛选依据与边界

通过 GitHub 仓库搜索发现候选，再核对上游、近期更新、源码结构、许可证和本项目技术栈。搜索词包括 `ai gateway language:Go stars:>1000 archived:false` 与 `vue design system stars:>1000 archived:false`。原始结果保存在本地 `discovery.json`，搜索命中不代表被选用。

优先选择 Vue 3 生态、Go 网关和可独立阅读的容错、监控实现。最新 antfu Vite/Vitest skills 面向比本项目更新的工具链，本次暂不安装；SQL 优化选择 Supabase 维护的 PostgreSQL skill，以配合现有 PostgreSQL/Ent。

New API 的 `NOTICE` 有额外署名条款；Bifrost 的部分第三方内容、k6 的 vendored 依赖等应按各自文件许可证处理。这里只完成源码准备，后续复用具体代码时检查其许可证和依赖。

## 本轮验证

- 9 个源码归档已记录 SHA-256；全部解压文件与归档大小、CRC 相符。
- 8 个 skills 的 `SKILL.md` 名称及其中的 Markdown 本地参考链接已检查；安装副本和离线备份均有逐文件内容指纹。
- 未执行上游源码或 skill 内的示例脚本，没有将参考项目添加为业务依赖。
- 本轮未运行 Sub2API 的业务测试或真实上游压测，尚无性能提升实测结论。
