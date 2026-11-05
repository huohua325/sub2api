# 2026-11-05 上游主分支检查

北京时间 13:29:58 检查，已执行 `git fetch origin --tags`，并用 `git ls-remote origin refs/heads/main` 核对远端。

- 当前 fork：`10331d5730d5a6fabe2fe216aa5b3dc1ad766470`。
- 已合并上游基线仍为 `4a4a83ee5ec86b19d3a2dd9b6471de5128963bec`。
- 官方 main 最新：`8e92c892b47f3f934f18ce63da4c4dcb75f0b6bd`；最近可达正式标签仍为 `v0.2.19`。
- 缺少 23 条提交（包括合并提交），其中 14 条非合并提交；共涉及 187 个文件，新增 10,318 行、删除 490 行。本批未增加或修改 `backend/migrations` 文件。

值得同步的内容包括：网关请求体内存预算、流式读写及取消后的有限收尾、后台任务和缓存资源边界；Grok Responses 消息输入兼容；OpenAI instructions 透传与回显；用户 CSV 导出限流重试；生图尺寸选号与粘性；注册邮箱黑名单；Kimi 并发限制冷却与 Antigravity 兼容。另有默认导出模型调整及赞助素材更新。

建议优先评估资源保护提交 `457602c8f`。它涉及 `gateway_usage_billing.go`、`openai_gateway_handler.go` 等本项目定制入口，后续不能仅凭文本自动合并就发布；需回归预留上下文、持久交接、强制计费任务、客户端取消和晚到用量。新增邮箱批量禁用功能需要验证管理员权限及确认流程。

本轮用户要求检查是否需要合并，故仅记录更新范围，没有执行 merge、源码修改或线上部署。`upstream-sync.json` 顶层已合并 SHA 与部署信息保持原值，只有最近检查字段更新。后续合并按[合并流程](UPSTREAM-MERGE-RUNBOOK.md)执行。
