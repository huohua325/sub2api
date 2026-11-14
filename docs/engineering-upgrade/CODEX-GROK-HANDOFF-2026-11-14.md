# Codex → Grok 任务消息完整性调研

2026-11-14，北京时间。范围：核对 CCR 现有桥接，以及官方 Codex 的任务消息传递；不实施产品功能或部署。

## 结论

**任务消息可以完整交给 Grok，官方 Codex 0.169.0 的 CodeMode 委派路径已通过真实调用。** 本轮不需要任务文件、不修改服务端请求正文、不解密 OpenAI 密文。首条任务、后续追问及原任务在子会话中的保留均有客户端出站记录和实际 Grok 回答相互佐证。

**CCR 当前的 V1 协议桥本身不能解决已观察到的 V2 加密任务问题。** 它转换工具名称与 namespace，并不改变不透明任务载荷。不能把“存在多智能体桥接代码”当作 GPT→Grok 的 V2 任务已经兼容。

通过路径需要在隔离客户端启用原生 CodeMode 协作调用，并让主模型使用对应原生辅助函数；这不是仅更换 Base URL 或在网站开启路由即可实现的证据。VS Code、长会话、压缩、恢复及自动选模型未由本轮小任务证明。没有评估降本或提速收益。

## 固定版本与资料

- 实际执行的官方 CLI：`codex-cli 0.169.0`，沿用已安装的独立客户端，未更新全局 CLI。
- 对应官方 tag：`rust-v0.169.0`，提交 `d32063f96c2dff1dc96af839b3952abb6acfeda4`。核心消息分支已对照该版本源码，避免仅用最新 main 推断旧二进制。
- 调研时 Codex main：`048431dcbecc510f1879c9421a97220016ecac44`。
- CCR 固定源码：`057abaa1f95519ab8faf999e1ccef2b80ccdeb9e`。
- [OpenAI 官方多智能体说明](https://developers.openai.com/codex/multi-agent)及[配置 schema](https://developers.openai.com/codex/config-schema.json)。

## 消息在哪里变为不透明内容

Codex V2 的 `spawn_agent.message`、`send_message.message` 和 `followup_task.message` 在工具 schema 中显式调用 `.with_encrypted()`。父模型返回加密参数后，客户端将其作为 `AgentMessage::Encrypted` 保存和传递，并不会先解密成普通文本供其他厂商模型使用。

客户端区分以下来源：

| 来源 | 原生处理 |
| --- | --- |
| V2 直接工具调用，返回 `encrypted_function_args=["message"]` | 按加密消息传递 |
| V2 直接工具调用，返回 `encrypted_function_args=[]` | 按明文消息传递 |
| V2 直接工具调用，未返回该字段 | 默认按加密消息传递，不能擅自视为明文 |
| CodeMode 原生工具调用 | 按明文消息传递 |

显式空数组与字段缺失不是同一个含义。即使某次参数字符串是普通文本，缺少元数据也可能被原生客户端装进 `encrypted_content`。本轮仅用模拟端点验证显式空数组分支，没有改写真实响应或伪装密文为明文。

源码：

- [V2 加密字段定义](https://github.com/openai/codex/blob/d32063f96c2dff1dc96af839b3952abb6acfeda4/codex-rs/core/src/tools/handlers/multi_agents_spec.rs#L649)
- [直接调用的明文识别条件](https://github.com/openai/codex/blob/d32063f96c2dff1dc96af839b3952abb6acfeda4/codex-rs/core/src/tools/router.rs#L69)
- [CodeMode 与直接调用的消息分支](https://github.com/openai/codex/blob/d32063f96c2dff1dc96af839b3952abb6acfeda4/codex-rs/core/src/tools/handlers/multi_agents_v2.rs#L54)
- [官方 CodeMode 明文委派测试](https://github.com/openai/codex/blob/d32063f96c2dff1dc96af839b3952abb6acfeda4/codex-rs/core/tests/suite/code_mode_agent_message_tests.rs#L24)

## CCR 做了什么、没有做什么

读取并执行了固定版本 `codex-multi-agent-bridge.ts` 中的原始转换函数。隔离运行省略未使用的路由依赖，值读取函数取自 CCR 原文件；没有另写一套消息转换来代替它。6 项断言均通过：

1. V1 namespace 工具转为普通函数名时，明文任务参数保持原样。
2. 即使 V1 工具发生转换，`agent_message.encrypted_content` 仍保持原样。
3. V2 `collaboration` namespace 不触发 V1 桥。
4. 无 namespace 的 V2 工具及其 `encrypted: true` 注解保持原样。
5. 返回工具名恢复时，加密参数和 `encrypted_function_args` 保持原样。
6. SSE 中的同类转换也保持这些内容原样。

这些断言验证转换函数，不等于运行了完整 CCR 服务或验证其全部路由。CCR 的 Claude Code 模型描述/任务标签路由是另一条实现，不能直接用作 Codex V2 消息兼容的证明。

[CCR 桥接源码](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/gateway/features/codex-multi-agent-bridge.ts)。

## 官方 CLI 本地协议验证

使用真实官方 CLI、独立 `CODEX_HOME` 和 loopback 脚本端点。端点按预定步骤返回工具调用，捕获客户端发给 Grok 模型路径的请求。这里没有模型推理、真实 Key 或线上调用；验证的是客户端传输行为。

最终 7 组对照均符合预期，每组都观察到首条和追问两次子模型请求：

| 场景 | 首条任务、追问的原生明文状态 |
| --- | --- |
| V2 加密参数，`fork_turns=none` | 密文保持原样，无法取得任务明文 |
| V2 加密参数，`fork_turns=all` | 同上；继承父历史不会解密当前任务 |
| V2 显式明文标记，`none` | 首条与追问逐字保留 |
| V2 文本参数但标记缺失，`none` | 被归为加密内容，不能作为规范明文路径 |
| V2 显式明文标记，`all` | 首条与追问逐字保留 |
| CodeMode 原生委派，`none` | 首条与追问逐字保留 |
| CodeMode 原生委派，`all` | 首条与追问逐字保留 |

前期有工具发现探针未找到直接 `spawn_agent`：现代请求将工具放在 `input.additional_tools`；修正探针后才进行上述对照。另两次 V1 候选配置仍未发现可直接调用的工具，保留失败记录，不计为 V1 兼容通过。不将自定义模型目录或 `multi_agent_v2=false` 当作已验证的 V1 回退方式；模型元数据仍可选择 V2。

## 真实 GPT → Grok 验证

执行目录：`D:/codex_user/test/.mile-test/20261114-handoff-live-code-mode/`。使用之前已授权的管理员统一 Key；真实 Key 仅在本地转发进程内存中读取，客户端配置用无效的 loopback 占位值，不复制全局 auth.json。

隔离配置在已有统一入口上增加：

```toml
[features.multi_agent_v2]
enabled = true
non_code_mode_only = false
```

该设置允许通过 CodeMode 使用原生协作工具。默认 `non_code_mode_only` 为 true，因此不能假定现有客户端已经开放这条辅助函数路径。

本轮使用独立 `probe` 角色选择 `grok-4.7`；主模型为 `gpt-6.1-sol`。两者推理强度均为 low。主模型被明确要求通过原生 `tools.collaboration__spawn_agent` 发起一次 `fork_turns="none"` 委派，通过 `tools.collaboration__followup_task` 追问同一子会话；没有任务文件，也没有服务端工具注入。

任务包括随机 nonce、中文原文、`/v1` 约束、数值 73129、包含空格的路径与逐字段 JSON 输出要求。追问再提供一个随机 nonce，要求保留全部初始事实。

本地中继仅将请求转发到现有 MILE Lab 入口，并记录正文和选定诊断信息；没有修改工具 schema、工具调用、加密元数据或响应事件。它不是新部署的网关实现。

结果：

- CLI 正常退出，耗时 **29.55 秒**，7 次真实请求全部 HTTP 200：GPT 5 次、Grok 2 次。
- Grok 首次出站请求包含完整初始任务；追问请求同时包含完整初始任务和完整追问，逐字比对通过。
- 两次真实 Grok 回答的全部要求字段均核对正确，主模型最后收到了并展示了两次 JSON 结果。
- 工作区没有生成任务文件或代码文件。
- 全局 config.toml / auth.json 哈希保持一致，测试目录没有真实 MILE Key 副本。

## 服务端只读核对

管理员测试 Key 仍为 ID 38、用户 2、统一编程分组 17。账本查询只读，未修改额度、周期或账本。

本轮的 7 条 usage 为 ID 66194–66200，对应 7 条额度账本 ID 39034–39040。请求模型、上游模型和返回模型一致：GPT 使用 OpenAI 账号 5、账本用途 `openai`；Grok 使用账号 14、用途 `grok`。

关联依据是测试 Key、时间窗口及唯一的模型/输入/输出/缓存 Token 组合。HTTP 诊断 request ID 与账本 `client:` request ID 不同，首次按 HTTP ID 匹配得到 0 条，已按上述证据修正核对，未伪造两类 ID 相等。

核对快照中这 7 条额度账本均为 pending，按既有流程等待结算；不能把模型路由和用途正确写成最终点数已经结算完成。

## 适用边界与后续判断

这次证据证明：在指定官方 CLI 版本和隔离设置下，使用原生 CodeMode 委派，可以通过现有 MILE Lab 入口完成 GPT→Grok 的明文任务与追问闭环。此前“只能通过任务文件交接”的判断应收窄为当时的默认直接加密路径。

这不是 CCR 已经完整支持当前 Codex 的证明，也未证明零新增客户端配置、自动选择经济模型、所有父历史可跨厂商无损复用、VS Code 等价、长期恢复可靠或整体更省钱。`fork_turns=all` 复制的其他加密历史仍可能不可读；选择 none 则必须显式提供必要材料。

优先复用官方已经存在的 CodeMode 明文机制，再评估客户端接入和网站策略如何衔接。是否实施该适配属于下一步产品决定，本轮没有更新线上程序、开放名单或用户全局配置。

## 证据位置

- 原始源码、官方资料、CCR 原函数验证与协议探针脚本：`output/research/codex-grok-handoff-20261114/`。
- CCR 固定源码：`output/research/ccr-codex-grok-20261114/`。
- loopback 请求、会话及每组摘要：`D:/codex_user/test/.mile-test/20261114-handoff-probe/`。
- 真实调用证据：`D:/codex_user/test/.mile-test/20261114-handoff-live-code-mode/evidence/`，包括 `summary.json`、`wire.json`、`answer-verification.json` 和 `billing-verification.json`。
- 原失败记录与任务文件试验：[先前混合模型试验](MIXED-AGENT-2026-11-14.md)。
