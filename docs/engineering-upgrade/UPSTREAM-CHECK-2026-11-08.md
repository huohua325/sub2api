# 2026-11-08 官方 main 检查

北京时间 2026-11-08 19:40:34 获取官方 main 与标签，并通过 `git ls-remote` 核对远端。用户本轮要求检查是否有更新，未执行合并、运行代码测试或线上部署。

## 固定范围

- 当前本地与 fork 的 `lab/0.2.17`：`68b4bf6ac444daaadf4322cabb808257645b0904`。
- 上次已纳入的官方基线：`25608d216bb7be04e4a646830cb065cd09b7d7e9`。
- 最新官方 main：`6b4b37db43443b5e88e424fe3c69602cc3810dfb`，提交时间为北京时间 11 月 7 日 14:31:41。
- 新正式标签：`v0.2.20`，解引用提交 `dab829a4e9b7d1cc307189aea8b0319eb29e29d0`。main 比该标签多一条 VERSION 同步提交，没有额外业务代码。
- 当前 fork 缺少 69 条上游提交（含合并提交），其中 42 条非合并提交；193 个文件变化，新增 8,160 行、删除 830 行。
- 上次官方基线是最新 main 的祖先；没有新增或修改 `backend/migrations`、`backend/ent/schema` 路径。

## 与 MILE 相关的更新

| 范围 | 内容及意义 |
| --- | --- |
| GPT-6.1 Terra | 改用官方 Codex descriptor，校验推理档位并修复兼容缓存键；OAuth 类订阅请求免除 Terra 长上下文附加费，API Key 仍按 API 价卡。 |
| Codex 导出 | 默认模型更新为 `gpt-6.1-sol`；导出保留原生生图鉴权标记并启用 live web search，CC Switch 附带 provider 配置。 |
| 网关性能 | 减少 JSON 重复解析／复制、Gemini 流式前缀复制，以及每请求的 RPM override／隐私配置数据库查询；引入 `MoeForge/moego v0.1.0-alpha.1`。 |
| 稳定性 | 请求体读取超时与错误分类、客户端断开及错误 Key 不再误报 500、长对话 cyber transcript 误拦修复、WebSocket HTTP bridge 指纹及每轮首 token 耗时修复。 |
| 安全与内存 | Brotli 升至 1.2.7、klauspost/compress 升至 1.20.2；上游提交注明修复 govulncheck 告警。另补充 Bedrock frame 长度、异步图片内存预留、SSE／非流式读取限制。此处是差异检查，不是本轮自行执行的漏洞扫描结果。 |
| 模型／媒体兼容 | GPT Image 2.6 原生链路、Responses 消息与 reasoning 内容、Gemini 工具媒体、Cloudflare Anthropic 请求、Antigravity 响应／额度窗口等修复。 |

## 合并建议与必须保留的定制

建议安排一次完整安全合并。这批有正式版、直接相关模型兼容和请求性能／稳定性修复，值得升级；不能只更新版本号或替换官方镜像。

476 个定制路径中有 31 个与这批上游变化交叉。主要涉及网关、Key 鉴权与缓存、计费、UseKeyModal、部署配置；不能以无文本冲突代替业务回归。

- Terra 长上下文计费修复会影响未来请求成本及点数分摊权重，应检查原始成本与上游订阅语义；不重定价历史请求或持久计费命令，不改个人上限、容量事件或周期。
- 减少 RPM／隐私数据库查询依赖鉴权缓存投影，需核对管理员修改后的缓存失效、用户并发／额度限制及权限语义。
- JSON 热路径、请求读取超时、异步图片内存所有权与 WebSocket 处理均与持久预留／计费交接相邻；须回归客户端断开、部分输出、重试、晚到用量及恢复。
- Codex 配置导出应保留 MILE 品牌、接入地址、Key 遮罩／复制和现有 Grok 单文件配置。
- 上游 compose 模板与 fork 有交集；发布须保留线上实际目录、私密配置、Caddy 和 MemSlides，继续 shadow。
- 合并前后继续核对账本、重置／扩容／纠正／预留／计费任务；实际运行隔离 PostgreSQL 的额度竞态测试及相关前后端验证。

本轮只更新检查记录。`upstream-sync.json` 顶层已合并 SHA 和实际部署信息保持原值，仅更新 `last_upstream_check`，避免把待合并 main 误记为已上线。
