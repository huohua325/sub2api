# 上游同步：2026-10-30

上次上游基线为 `a3298b46ebe239fb0720b068a1eb3383955bdac2`。本次固定纳入 `d851a38477597157e78450d30af2d8504764d085`，新增 10 个提交（含合并提交），85 个文件，1 个上游迁移。最新正式标签仍为 v0.2.19，目标包含其后的主分支更新。

额度修复先独立提交为 `c072ac5cf5161c8a6e5494d0355d72792a0e7de8`，再以它为 fork 基线形成双亲合并提交 `3f35985b16e17bcee14ac1048984124b00cd688d`；另一父提交就是本次固定上游。没有 squash，也没有向官方 origin 推送。

## 纳入与取舍

- vue-i18n / message-compiler 11.4.16、decode-uri-component 安全更新；CI Node 24。
- 管理 MCP 的 OAuth/PKCE、权限、授权撤销和审计，SDK 1.4.1。默认关闭，本轮不启用。
- 两处文本冲突：`auth_service.go` 同时保留本地 `purposeAllowances` 和上游 `mcpGrantRevoker`；`wire_gen.go` 同时保留学员钱包注入与 MCP handler 初始化。没有整文件覆盖，没有重新生成 Ent。
- 自动合并后的侧栏仍按本地学员分组和功能过滤；MCP 入口受开关控制。退出排空、预留、持久交接/恢复与 shadow 保留，Compose 的 120 秒外部退出窗口保留。
- 比较合并前后路径，MILE 样式、品牌、仪表盘、Key 页面、接入指引、额度展示、自有额度仓库与运行时、requestdrain 均未被上游修改。
- 上游 `249_mcp_oauth.sql` 与本地 `249_user_purpose_allowances.sql` 是不同完整文件名，迁移器以完整文件名为主键。保留旧文件与校验和，未重编号。

本地扩容修复另见 [容量记录](CAPACITY-AND-UPSTREAM-2026-10-30.md)。它不更改已结算扣点、个人周期、全体重置事件或 Grok/API 实验规则，不按合成复现自动退款。

## 验证

| 检查 | 实际结果 |
| --- | --- |
| 真实隔离 PostgreSQL 额度 `-race` | 合并后 61 个测试/子测试通过，0 失败、0 跳过；包括 CLI 选定测试 |
| MCP、额度、权限、排空专项 `-race` | 177 个通过，0 失败、0 跳过 |
| 前端全量 | 3168 项通过，0 失败、0 跳过；合并前 3126 项通过 |
| 类型、只读 lint、国际化、生产构建 | 通过；国际化 3 项通过 |
| Go vet / 全仓增量 lint | 通过 / 0 issues |
| 线上 schema 的只读副本升级 | 本地隔离数据库 312→314，重复启动迁移幂等；两个 249 和新 259 均有独立迁移记录 |
| Chromium / WebKit 生产构建 | mock 全部业务 API，375/1440px 仪表盘无横向溢出、无页面异常；68% 和中文正常，不展示点数，历史统计未首屏加载；接入指引正常 |
| 默认全量 Go | 14170 个通过事件、15 跳过、相同 9 项原有失败；没有新增失败 |

9 项失败逐项分类：`TestAuthIdentityFoundationSchemas` 本轮日志为 ent loader 找不到 go 的 PATH 问题；三个 `TestPgDumper*` 为 Windows 缺 sh 及对应锁 mock 的环境基线；平台枚举与调度桶两项为既有 qwen 覆盖问题；三个 UsageLog 写入测试为既有列数/参数及 native compaction 断言基线。没有用跳过代替通过。

补充边界：从完全空库运行全部旧迁移时，既有 `109_auth_identity_compat_backfill.sql` 触发 varchar(40) 长度错误；本轮未修改该旧迁移。实际发布路径是当前 312 项迁移的库增量升级，已用线上 schema-only 副本和迁移校验记录在隔离库验证。上游带 integration 标签的 Docker MCP 数据库套件未在本机执行；本轮真实数据库覆盖为额度和增量迁移，不把 MCP 单元测试称为其数据库集成测试。Firefox 本机虽成功启动，后续 smoke 未完成，不能计为通过。

本地证据：`output/quota-drop-20261030/`、`output/playwright/capacity-20261030/`；私有原始明细不提交。早期 `baseline-go.jsonl` 在扫描时纳入了新复现红测试，不能作为纯净 9 项失败的基线；原纯净基线见前次审计记录。

## 发布

- 2026-10-30 **22:37 北京时间**部署至 `172.104.117.114`；运行版本 `0.2.19-mile.20261030.capacity1`，源码为上述双亲合并提交。
- 程序 SHA256：`64ebc2b72acf4234d0e5882764a5f2d7898158325fce6ab891cf18705cfdc705`，构建记录 `vcs.modified=false`。本次重新构建前端，未复用旧版国际化产物。
- 备份：`/opt/sub2api/backups/before-capacity-20261030T143710Z`，数据库备份 7,017,397 字节，通过 `pg_restore --list`；原程序、配置及数据已备份。
- 同一参考时刻 `2026-10-30T14:37:12.764324Z` 核对：8,235 条原请求、6,479 条 settled/imported、1,328 条 unmetered 估算保留。原钱包起点、已确认及候选事件、纠正/对账、预留/事件/计费/异步任务均保留，个人周期不变。六名存量用户 Codex 截止仍为 11-04 14:35:59，北京时间。
- 只增加一个经平台所有者确认的容量事件，未创建或确认全体重置。详情见容量记录；CLI preview、固定 digest apply 及重复 apply 的结果一致。
- 迁移 312→314，Codex/Grok 仍为 shadow，原观察起点保留。独立核对时，发布后新产生的 30 条持久计费任务均已应用、没有重试错误；这不是宣称全部未知上游用量已经核实。
- 应用及 MemSlides 均返回 200；16 个公开资源哈希与发布产物一致。线上新版 Chromium mock 学员仪表盘与接入指引检查通过，不创建真实 Key、不调用模型、不确认重置。
- 部署控制台中 Docker 正常进度写入 stderr 被 PowerShell 包装成 `NativeCommandError`，该本地会话返回 1；远端已写完整 `deployment.json`，随后独立服务器验证退出 0，程序版本/哈希、314 项迁移、事件及健康均匹配。没有把该本地返回值伪称为通过。

证据在 `output/releases/mile-20261030-capacity1/`。回退只换程序，保留新增扩容事件及所有新旧账本、纠正、预留和持久任务，不用旧数据库覆盖新消耗。后续文档提交晚于运行程序源码 SHA。
