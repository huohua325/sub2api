# MILE Lab 线上同步记录 · 2026-10-27

当前线上版本为 `0.2.18-mile.20261027.grok1`，20:28 完成切换。下方保留历次发布记录，最新记录见文末。

## 部署结果

- 服务器：`172.104.117.114`，Linux x86_64。
- 版本：`0.2.18-mile.20261027`，2026-10-27 15:53（北京时间）完成切换。
- 源码标识：`7498ba6be-worktree`，包含本地尚未提交的额度账本改动；并非仅发布 HEAD。
- 入口：https://sub2api.172.104.117.114.sslip.io
- 备用入口配置：https://sub2api.172-104-117-114.sslip.io（本次未单独进行浏览器验证）。
- 部署方式：沿用 Docker Compose 和 `/opt/sub2api/bin/sub2api` 的只读文件挂载，仅重建 sub2api 容器。
- 发布文件：`/opt/sub2api/releases/mile-20261027/sub2api`，内嵌前端资源。
- SHA256：`ed82e04ca62c1439e4c7cab91a9172e12e2c7cb40da1843c611572759d82e945`。

## 同机服务

| 功能 | 运行方式 | 入口与用途 |
| --- | --- | --- |
| MILE Lab / sub2api | sub2api、PostgreSQL、Redis 三个容器；Caddy 转发至 127.0.0.1:8080 | 学员工作台、管理员后台与 AI API 中转 |
| MemSlides 中继 | `memslides-download-relay.service`；Python 服务监听 127.0.0.1:8790 | `fetch.memslides.com`；文档下载、抓取、Brave 搜索、摘要、图像与 MinerU 解析 |

本次未修改 MemSlides 服务或 Caddy 配置。

## 备份与数据库

备份目录：`/opt/sub2api/backups/before-mile-20261027T075307Z`，仅 root 可访问。

- `sub2api.previous`：切换前的程序。
- `database.dump`：停止旧应用后取得的 PostgreSQL 自定义格式备份，3,107,062 字节，已通过 `pg_restore --list` 读取检查；未进行完整恢复演练。
- `config-and-data.tar.gz`：Compose、环境配置和应用 data 目录。
- `container-inspect.json`、`new-startup.log`：原容器信息与新版启动日志。

预检确认历史迁移校验兼容，仅新增 `254_coding_quota_ledger.sql`。启动后已应用全部 307 个迁移文件，无待执行项或校验冲突。旧额度配置已备份至 `quota_ledger_previous_point_budget`；当前 Codex 50、Grok 30。6 名用户的 Codex/Grok 周周期和 API 实验月周期均保留。

已有估算消耗导入为明确标记的 opening entries；线上新请求已进入个人账本。未人为触发全体异常重置。

新账本产生流量后，不能只替换回旧程序而假定额度计费仍正确。若必须回退，应先停止流量并评估账本数据与数据库恢复方案，避免丢失上线后的使用记录。

## 验证范围

- sub2api 容器及 PostgreSQL、Redis 均为 healthy；Caddy 和 MemSlides 服务为 active。
- 公网 `/health`、首页、`/login`、公开配置接口均返回 200。
- HTML 引用的 4 个入口 JS 文件和 2 个 CSS 文件，内容校验与本地构建一致。
- Chromium 实际打开首页，点击登录并返回；首页和登录页均检查 1440px、375px 截图，MI 主视觉已显示，控制台无错误或警告。
- 新服务日志未发现 ERROR/FATAL、panic 或迁移校验错误。启动时仍有原配置相关提示（JWT 时长、URL allowlist、代理和 CORS 配置）及价格兜底提示，本次未调整这些配置。
- MemSlides `/healthz` 返回 200。
- 未使用真实账号登录，因此未在线复验受保护的仪表盘、管理员操作或真实上游调用；本次没有新增性能基准结论。

截图：

- `output/playwright/production-home-1440.png`
- `output/playwright/production-home-375.png`
- `output/playwright/production-login-1440.png`
- `output/playwright/production-login-375.png`

本地发布及检查记录位于 `output/releases/mile-20261027/`；服务器部署记录位于 `/opt/sub2api/releases/mile-20261027/deployment.json`。备份含私密配置，保留在服务器，未下载至仓库。

## 16:15 显示修复同步

已部署 `0.2.18-mile.20261027.ui1`：管理员侧栏版本号移至 Logo 下方，限制长版本号宽度并允许弹层完整换行；学员 Codex/Grok 卡片仅展示百分比、状态与重置时间，移除具体点数，API 实验保留金额。额度计算与数据库结构未修改。

- 新发布目录：`/opt/sub2api/releases/mile-20261027-ui1`。
- 切换前备份：`/opt/sub2api/backups/before-mile-20261027T081512Z`，数据库备份 3,132,554 字节，已读取目录检查。
- SHA256：`8c9b84247aef5dfbdfde49a54e98560cfb1175ef80c03b29c3c28d311d736b65`。
- 组件及仪表盘请求测试 24 项通过；国际化检查、Vue 类型检查、修改文件的只读 ESLint 和生产构建通过。
- Chromium 使用线上静态资源与固定模拟账号/额度响应验证：1440px 版本按钮位于 256px 侧栏内；学员 1440px、375px 均无稳定状态下的横向溢出；Codex/Grok 点数不出现在卡片文本中，百分比与倒计时保留，API 实验金额保留，桌面三栏进度条对齐。控制台无错误或警告。
- 公网健康检查正常，6 个入口 JS/CSS 文件与新构建校验一致；sub2api、PostgreSQL、Redis 健康，MemSlides 仍正常。
- 截图与检查记录：`output/playwright/display-fix-*`。此次线上受保护页面检查使用模拟认证与业务响应，未冒用现有账号或触发真实额度操作。

## 16:29 API 密钥页空间调整

已部署 `0.2.18-mile.20261027.ui2`：标题与创建/列设置/刷新操作合并一行，接入提示与筛选同行，区域间距从 24px 减至 12px，表格单元格上下留白从 16px 减至 10px。改动仅作用于 API 密钥页，移动端自动换行并使用页面自然滚动；创建、复制、分组、筛选、批量选择和危险操作逻辑沿用原实现。

- 1440×800、相同 12 条固定数据：表格从距顶部约 379px 上移至 238px，高度从约 296px 增至 449px；按完整行且预留滚动条的保守口径，可见行数从 2 增至 5。
- 1280×680 和 1024×768 可完整显示 4 行；768px、375px 无页面横向溢出。线上资源配合模拟认证与业务响应重验上述布局，浏览器控制台无错误或警告。
- Key 页现有 26 项测试通过，修改文件只读 ESLint、国际化检查、Vue 类型检查和生产构建通过。
- 服务健康，公网 6 个入口资源与本地构建一致；MemSlides 中继继续正常运行。
- 发布目录：`/opt/sub2api/releases/mile-20261027-ui2`；备份目录：`/opt/sub2api/backups/before-mile-20261027T082854Z`。数据库备份 3,139,149 字节，已读取目录检查；本轮没有新迁移。
- SHA256：`dae16f0f85de979095e4f8ca68810d228018771550e5611d736a752356eccf28`。
- 截图：`output/playwright/keys-space-before.png`、`output/playwright/keys-space-after-{1440,1280,1024,768,375}.png`；尺寸对照：`output/releases/mile-20261027-ui2/layout-comparison.json`。

## 18:09 学员接入与额度状态统一

已部署 `0.2.18-mile.20261027.connect1`，业务源码提交 `56e9e1cd84e073e590d922c9460c45fe43c84272`。创建 Key 后直接进入对应接入面板，统一首页、指引与实际配置；支持遮罩、显隐、复制和单文件下载。额度继续隐藏 Coding 点数，补齐已用比例、周期与数据可信度，修正空账本的更新时间，合并重复刷新。计费规则和数据库结构未改变。

- 发布目录：`/opt/sub2api/releases/mile-20261027-connect1`。
- SHA256：`ec9e50bf02dcf316db5bf5233b1a2a5f1aeef56f07710ddf7081b2d7ad3f4798`。
- 备份目录：`/opt/sub2api/backups/before-mile-20261027T100903Z`；数据库备份 3,184,720 字节，已读取目录检查。
- 运行程序版本与提交、18 个公网资源校验一致；三个容器健康，Caddy 与 MemSlides 正常；仍为 307 个迁移，无待执行或校验冲突。
- 公网静态资源配合模拟业务响应，Chromium 40 项检查通过，无页面异常；没有生产账号写入或收费调用。部署后个人周期与计量只读核对正常。
- 自动化、浏览器结果、Firefox 驱动限制及计量边界详见 [接入与额度验收](CONNECTION-AND-QUOTA-ACCEPTANCE.md)。

## 20:28 恢复 Grok 简版配置

按用户要求恢复原首页与指引的 32 行 `config.toml`：单个 `grok-4.7`、直接填写 `api_key`、一个 `models_base_url`，恢复原有 `supports_reasoning_effort = true`、`supports_backend_search = false`、插件市场、自动更新、`yolo = true` 等设置。配置模板与原提交逐字核对一致，仅将地址和密钥插值保留为正确的 TOML 转义。首页、开始使用和真实接入面板统一使用此模板，不再要求额外环境变量。默认密钥遮罩、显隐、复制与下载保留。

- 版本：`0.2.18-mile.20261027.grok1`；源码：`40e80eb16cd4de0bcdf01c64d385a3a2e284321b`。
- 发布目录：`/opt/sub2api/releases/mile-20261027-grok1`。
- SHA256：`07b6d75f2d7f7964dcc00b7fdee02b02fae786086a9a6beba105cdafe3472d0b`。
- 切换前备份：`/opt/sub2api/backups/before-mile-20261027T122815Z`；数据库备份 3,209,558 字节，`pg_restore --list` 检查通过，未进行完整恢复演练。
- 77 项相关前端测试、国际化检查、类型检查、全量只读 lint 与生产构建通过。Chromium、WebKit 各 45 项本地模拟业务检查通过；使用公网资源的 Chromium 45 项复验通过，无页面异常。
- 公网 18 个静态资源与本地构建校验一致；健康、登录、公开配置接口均 200。三个容器 healthy，Caddy 和 MemSlides active，中继健康检查正常。
- 本轮没有修改后端逻辑或迁移，也没有实际调用模型、探测模型权限或写入生产 Key。已下载的客户端配置不会自动替换，需要用户重新复制或下载并重启客户端。
- 复核文件：`output/releases/mile-20261027-grok1/`；截图：`output/playwright/connection-quota/*grok-config*`。含凭证的私密备份仅保留在服务器。
