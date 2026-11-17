# MILE Lab 交付工具

## 发布准备与仅部署的边界

2026-11-17 的上游上线约耗时半小时，主要额外工作是补迁移发布契约和传输失败后改路。
完整前端没有重跑或重新构建；最终准备实际为 278.890 秒，服务器切换／持久数据核对
21.574 秒，成功的私有中转为 29.405 秒。后两个数字不能代表整体交付耗时。
原始事实与验证边界见 [发布记录](../docs/engineering-upgrade/UPSTREAM-DEPLOY-2026-11-17.md)。

以后按以下顺序执行：

1. 开发／合并开始就检查当前发布入口是否支持预期后端、迁移和回退语义。需要的新契约及工具
   先完成审查、相关测试与提交，之后再运行 `finalize`，避免被未提交输入守卫拦截。
2. 范围包含后续上线的工作，在开发交付时准备固定版本、源码 SHA、程序 SHA256、前端产物、
   不可变包和证据路径，并注明发布入口、线上基线、迁移及剩余检查。仅合并任务未打包时明确
   写“源码已验证，尚需准备发布包”，不要报告“可直接部署”。准备产物不代表获得上线授权。
3. 收到部署指令后先核对已有包和证据，按源码、依赖、配置、工具、环境、产物指纹及现有时效规则
   复用。只补失效／本轮必须实际执行的检查；额度竞态与浏览器实际执行、即时预检、备份校验、
   固定时刻账本／周期／权限核对及公网验收仍遵守原规则。证据准备后仅文档跟进不重复编译。
4. 先查传输历史。未知链路用发布暂存目录中的 256KiB–1MiB 非敏感样本做至多 30 秒的探测，
   记录字节数和耗时，用实测吞吐估算传输时间；不把 SSH 登录成功当成大文件传输正常。
   已知异常的本机到 `172.104.117.114:22` 直连，在测量确认恢复前优先复用私有中转。
5. 默认复用已验证差量缓存。比较“差量准备＋传输”与“完整压缩包＋传输”的实测成本后才显式
   选择 `--full-upload`，不因 Go 补丁版本变化就猜测完整包更快。包已选定则复用同一 payload；
   传输中断优先换通道，不重建程序、不反复生成另一种包。未有进展或明显丢包时尽早切换，
   不连续试 SFTP→旧 SCP→重算差量。切换前核对并结束本轮拥有的失败传输进程。
6. 使用下方现成的 `release_transfer.py` 时只中转 fork 的临时私有草稿资产；核对服务器 payload
   与程序哈希、草稿清理结果，再 stage、即时预检和 apply。凭证、签名地址和生产账本不能进入 Git。
7. 分段记录准备、验证／构建、传输、备份、切换／核对、公网验收、记录提交的起止时间，同时报告
   用户感知总耗时。并行阶段按实际墙钟时间计算，不能简单相加；缺少时间证据的区间标为估算。

以上是执行约定；现有工具提供构建／校验／传输能力，尚未自动完成吞吐探测和通道选择。
先复用现成命令，不把下一次发布变成新的流程开发任务。关键检查保留，优先消除重复准备和失败重试。

## v0.2.22 两迁移上游发布

`upstream_additive_upgrade.py` 单独固定已审查的双亲 merge、线上源基线及两条新增 SQL
的文件／数据库校验和。旧无迁移、前端和个人重置通道守卫保持原样。
先使用已有冻结前端证据执行 `delivery.py finalize --version <版本> --feature unified-coding
--feature personal-quota`，再用该入口 `package`，必须提供 `--admin-browser <分组产物回归/result.json>`。
`--full-upload` 仅在实测总成本更低时显式选择；Go 补丁版本变化本身不作为选择依据。
服务器用 `python3 upstream_additive_upgrade.py deploy --release <目录>` 依次 stage、预检、apply。
升级保留原迁移、额度账本／审计、持久任务、周期、分组、Key 绑定和测试授权，禁止自动启用
Ultra Fast。失败只回退程序；若已人工启用新档位则阻止自动回退并向前修复。
原程序支持新增列及兼容的汇总触发器，不执行数据库回退。

SSH 大文件传输丢包时，可用 `release_transfer.py --package <已校验包> --host <SSH目标>
--remote-release /opt/sub2api/releases/<目录> --proxy http://127.0.0.1:7897 --output <传输证据.json>`
中转同一 payload。它只创建 fork 内的临时私有草稿资产，不公开发布；Git 凭证只在本机内存中
使用，短期下载地址通过 SSH stdin 传递，服务器核对大小／SHA256 后原子替换暂存 payload。
成功或失败均删除本次草稿，证据不包含凭证／签名 URL。发布元数据仍需上传；后续 stage／预检／apply
守卫不变。先查丢包及当前传输，不同时重建程序或改变线上防火墙。

## 无迁移上游合并

`upstream_upgrade.py` 是显式后端发布入口，当前固定合并前 SHA
`f3b217911923de35658969994b1d0f44ad070c38` 和上游 SHA
`530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484`。仅接受这两个父提交的真实、无冲突合并；
应用树须与 Git 自动合并结果一致，迁移目录不能增删或修改，后续工具／文档提交可以保留。
下一轮上游同步须重新审查并更新固定基线，不能直接复用旧契约。

仍先按上游 runbook 完成广覆盖 Go、vet、增量 lint、定制审查和失败基线比较，再完成
`validate` 及 `finalize --feature unified-coding --feature personal-quota`。
打包额外强制两种定制路径的真实产物浏览器证据；前端发布入口的后端拦截不变。

```powershell
& D:/Programs/anaconda3/python.exe tools/upstream_upgrade.py package `
  --output output/delivery/upstream-20261115 `
  --destination output/releases/mile-20261115-upstream1 `
  --version 0.2.21-mile.20261115.upstream1 `
  --source-commit <已审查的真实合并SHA> `
  --baseline-sha256 <线上程序SHA256> --baseline-binary <已校验的旧程序>
```

服务器依次执行 `python3 upstream_upgrade.py deploy --release <发布目录> --stage`、
不带 `--apply` 的只读预检，再带 `--apply` 正式发布。复用冻结 Compose、备份验证、
固定时刻持久数据核对和同 schema 程序回退，绝不恢复旧数据库。
线上 `docker-compose.override.yml` 中的 Go 内存回收参数也随有效 Compose 保留。

第一阶段保持现有嵌入式前端架构、额度保护和 shadow 模式。工具使用 Python 3.11+ 标准库；生成差量包时额外安装 `requirements-delivery.txt`。以下命令在仓库根目录执行，PowerShell 普通工具调用应禁用 profile 加载。Python 使用 `D:/Programs/anaconda3/python.exe`，pnpm 使用 `pnpm.cmd`。

## 日常修改

先生成范围清单，并在编码期间并行运行只读预检：

```powershell
& D:/Programs/anaconda3/python.exe tools/delivery.py plan --base <开始修改前的固定SHA> --output output/delivery/my-change
& D:/Programs/anaconda3/python.exe tools/delivery.py preflight --host root@172.104.117.114 --expected-sha256 <线上程序SHA256> --output output/delivery/my-change/preflight
```

`plan` 按路径提供建议范围，不是自动免检凭据；依赖影响仍需审查。仅工具／文档改动不发布应用；涉及前端或后端的实际应用升级仍要求完整前端证据、真实额度竞态及持久数据核对。普通额度改动运行相关包／用例，公共计费、鉴权或上游升级扩大 Go 覆盖；CI 保持全量。新增迁移单独要求隔离迁移验证，不因单独 SQL 文件就运行无关模型定价全量。

`preflight` 把当前工具通过 SSH 送入服务器私有临时目录，仅执行只读命令、临时生成并核对冻结配置；不要求已有发布包、不停止或重建服务、不备份或清理业务目录。日志及报告写入本地 output，不保存环境明文或密钥。核对程序基线、备份空间、应用和 MemSlides、shadow、Compose 实际计划及冻结后的 canonical hash；临时目录自动回收。部署仍执行自己的即时预检，早期报告不能替代部署检查。

1. 先写清本次用户操作和预期结果，修改后先测相关用例及真实页面交互。
2. 启动开发服务器：`pnpm.cmd --dir frontend dev --host 127.0.0.1`。检查输出中的实际端口。
3. 先完成本次路径和相关用例及审查，冻结应用源码，再执行完整前端验证及一次最终构建：

   ```powershell
   & D:/Programs/anaconda3/python.exe tools/delivery.py freeze --reviewed --output output/delivery/my-change
   & D:/Programs/anaconda3/python.exe tools/delivery.py validate --review-url http://127.0.0.1:3000 --output output/delivery/my-change
   ```

   顺序为 frozen install → 指南核心路径 Chromium 快验 → lint/full Vitest/`vue-tsc -b`（最多两项并行）→ Vite 构建。全量 Vitest 已含 i18n，故不再额外调用一次；日常 `pnpm build` 完整入口保持原样。默认指南场景是公共导航基线，**不替代本次变更特有的用户路径**；例如密钥权限、支付和管理操作必须补充相应已有回归。

   缺少冻结记录或应用源码在冻结后变化，验证在安装、测试和构建开始前拒绝；生成的 dist、vite.config.js/.d.ts、tsbuildinfo 不使冻结失效。冻结记录只证明声明审查后的源码快照，不能代替用例、审查或检查证据。实际改源码后完成相关测试／审查并重新 freeze，按内容指纹重跑受影响的完整检查。

4. 对最终构建执行真实产物回归：

   ```powershell
   & D:/Programs/anaconda3/python.exe tools/delivery.py review-artifact --output output/delivery/my-change
   ```

   临时本机 SPA server 自动启动和关闭，Chromium/WebKit 验证 16 次导航、历史/深链接/刷新/焦点、375/768/1440px、复制。接口共用 mock fixtures，真实业务写入和推理请求被拦截。普通导航等待 DOM、字体和业务元素，并确认滚动稳定；尾部短章节检查标题实际可见。WebKit 新文档深链接及刷新会在 load 阶段完成原生 hash 恢复，仅这两处使用最多 15 秒的 load 等待，避免 DOM 阶段探测干扰原生滚动。首次需准备 Node、npx、`@playwright/cli` 和浏览器；工具使用 `npx --no-install`，缺失会失败，不会暗中安装新依赖。

## 结果复用与计时

同一个 `--output` 下保留历史 `manifest.json` 和每次原始日志。只有输入文件内容（含锁文件/配置）、完整参数、运行目录、环境摘要、工具版本、执行文件，以及产物/日志哈希都匹配，lint/tests/types/build 才会复用。新增/删除文件、产物损坏、执行失败、运行中编辑输入都会导致失效或失败。环境只存摘要，不把 DSN 和密钥写入 manifest；原始命令输出仍需按敏感证据管理，留在忽略的 output 内。

这是本地可信工作区的效率工具，不是防篡改签名系统。不可把其他人的手写 manifest 当作验证证据。依赖目录在扫描前剪枝，仅锁文件和依赖元数据参与指纹；编译生成的 vite.config.js/.d.ts 和 tsbuildinfo 不当作源文件，构建明确使用 vite.config.ts。依赖安装每轮执行 frozen 检查；浏览器和额度竞态每次实际执行。`--fresh` 强制重跑前端检查。只读预检可并行准备，不能并行修改同一输出目录；锁存在时先确认持有进程状态，工具不会自动删锁。

```powershell
& D:/Programs/anaconda3/python.exe tools/delivery.py summary --output output/delivery/my-change
```

摘要直接使用证据生成，不会重新构建。源码提交和后续发布记录提交各自保留，文档提交本身不触发程序构建。

Go 检查统一保留 UTF-8 JSON 原始日志及 go-summary.json；命令非零、JSON 失败、编译失败或没有实际通过的测试均不能标为通过。已知基线失败只是解释证据，不提供忽略失败或跳过检查的参数。

```powershell
& D:/Programs/anaconda3/python.exe tools/delivery.py go-check --go-executable D:/Project/sub2api/output/quota-review/go.exe --packages ./internal/repository ./internal/handler/admin --run 'TestPersonalQuota|TestCodingQuota' --output output/delivery/my-change/go-targeted
```

`--race` 和 `--tags` 按改动选择；`go-check` 不替代固定的 `quota` 零跳过竞态检查。`quota`、`go-check`、`build-backend` 都支持 `--go-executable` 单一路径，避免 PowerShell JSON 引号问题；多参数包装器仍可用原 `--go-command`。

## 第二阶段：一次准备发布产物

完成真实功能路径、审查、冻结、`validate` 并提交应用源码后，优先用一条命令完成最终检查和编译：

```powershell
# DSN 和 Go 工具链仍从本机安全配置进入环境，不能写入命令参数或 Git。
& D:/Programs/anaconda3/python.exe tools/delivery.py finalize --version <版本> --feature unified-coding --go-executable D:/Project/sub2api/output/quota-review/go.exe --output output/delivery/my-change
```

`--feature unified-coding` 核对统一编程；`--feature personal-quota` 核对管理员重置。多条真实路径可重复传入 `--feature`，新功能应先增加对应浏览器场景。仅指南导航变化可明确选择 `--feature guide-only`；它不能替代其它功能的验收。

工具先检查已冻结、已提交的源码及全部前端证据，再调度真实额度竞态、双浏览器基线／所选功能路径和嵌入式程序编译。默认最多两项并行；资源充足时可明确用 `--jobs 3`，资源不足用 `--jobs 1`。前端 dist 此时已经稳定，不能在运行中继续编辑或重建。此命令不重复前端构建、不上传、不发布、不自动提交源码。

每次并行任务使用独立证据目录，汇总时核对同一工作区、本轮开始时间、当前输入、产物和原日志哈希。额度结果再次解析 Go JSON，拒绝零执行、skip 和失败。原文件不改写，汇总记录保存来源清单及哈希；不再手工修改 manifest。任意任务失败都会等待其余任务完成并保留日志，`finalize.json` 标为失败；打包拒绝沿用这次失败前的旧成功。修改相应用例／审查后重跑同一命令即可，浏览器和竞态每次实际执行，已验证且输入未变的程序可复用。

`finalize` 完成后继续使用原 `package` 和已授权发布流程。普通前端、个人重置及固定后端预览通道的原有范围守卫保留，新增工具不会把任意后端变化放入前端发布通道。

`validate` 的只读 lint 额外使用 ESLint 原生 `--cache-strategy content`，缓存放在忽略的 `output/delivery-cache/eslint/`，按依赖及工具版本隔离。每轮仍扫描全部文件；变化文件重新检查，ESLint 配置变化由原生缓存失效机制处理，依赖变化切换缓存空间。`--fresh` 同时禁用这层 lint 缓存和命令结果复用。完整 Vitest、类型、国际化、产物浏览器和额度保护要求保持不变。

## 准备一次纯前端程序发布

需要 Go 项目要求的工具链、真实隔离 PostgreSQL、前述前端验证及产物回归。**先提交应用源码，再运行 Go 编译和打包**；不能先编译脏源码、再提交并沿用旧 HEAD 标识。构建/打包也会拒绝未跟踪的应用输入。若根目录有用户原有未跟踪文件，应在对应提交的干净 worktree 中构建，保留原目录和文件，不能删除或擅自提交它们。环境变量从本地安全配置取得，不放到 Git、命令参数或文档。

```powershell
# 先在当前进程配置 SUB2API_QUOTA_TEST_DSN，且必须是隔离测试库。
& D:/Programs/anaconda3/python.exe tools/delivery.py quota --output output/delivery/my-change
& D:/Programs/anaconda3/python.exe tools/delivery.py build-backend --version <新版本> --output output/delivery/my-change
```

`quota` 固定执行 `go test -json -race ./internal/repository -run TestCodingQuotaDB -count=1`，解析实际测试事件，至少一项通过、零跳过、零失败才会生成可放行的证据；从不复用旧额度竞态结果。`--go-command` 可传 JSON 参数数组，指定实际 Go 可执行文件或受控包装器；不会把参数拼为 shell。包装器必须原样传递 JSON 事件和退出码，并正确继承隔离 DSN。缺少本机工具链不是通过。

Linux/amd64 程序使用 embed、固定版本/源码/提交时间，产物哈希归入证据。源码已提交且构建通过后打包；后续只有发布记录等应用源码未变的提交可以沿用原运行时 SHA：

```powershell
& D:/Programs/anaconda3/python.exe tools/delivery.py package --output output/delivery/my-change --destination output/releases/<发布名> --version <新版本> --baseline-commit <当前线上源码SHA> --baseline-sha256 <当前线上程序SHA256> --baseline-binary <本地已验证的当前线上程序>
```

默认生成差量包并在本地重建验证；需要 `bsdiff4==1.4.1`。只有显式 `--full-upload` 才选择完整 gzip，不会因缺依赖自动扩大上传量。包内包括程序/payload 哈希、前端文件清单、验证证据摘要及服务器工具，目录不可覆盖。打包不重跑编译。**后端生产文件或迁移相对基线有变化时，这条发布通道拒绝执行，改走完整升级 runbook；已提交且不进入当前生产构建的 `_test.go` 变更保留真实测试要求，不据此判为生产后端变化。** 未跟踪测试同样拒绝。不自动从旧的 exit.txt、固定日期或固定测试数量生成通过结论。

已验证差量缓存保存在 `output/delivery-cache/deltas/<旧SHA>-<新SHA>/`。发布工具或记录提交后的重新打包会复用差量；每次仍核对缓存哈希并用旧程序重建到目标程序，损坏缓存重算并验证，绝不直接相信缓存 metadata。应用和版本未变时，build-backend 保留已有成功构建的源码 SHA，让文档提交不再仅因 HEAD 改变而触发重编；源码、构建输入、参数、工具、环境或产物改变仍会使对应缓存失效。

## 只调整应用运行资源

服务器扩容后，可使用 `runtime_resources.py` 调整现有程序的内存配置。此入口不替换程序、
不执行迁移，也不提高数据库连接池或上游账号并发。必须固定当前程序 SHA256，默认只预演：

```bash
python3 runtime_resources.py --expected-sha256 <当前程序SHA256> --body-mib 192 --output /opt/sub2api/releases/<工具目录>/resources-plan.json
```

核对物理内存及精确配置差异后，使用新的报告路径并加 `--apply`。默认应用硬上限
768MiB、内存与 swap 合计 1024MiB、`GOMEMLIMIT=512MiB`、`GOGC=50`；这些默认值针对
已核对的 2GB 主机，不能据此声称支持固定并发数。`--body-mib` 是所有在途请求体缓冲的
共享预算，不是单请求大小上限，必须按真实请求及处理中的额外副本评估，不能任意照抄。

工具只允许上述应用资源字段变化；保留原 override 的其他设置和变量引用，另存原始内容、
权限和所有者。验证 PostgreSQL 备份后才平滑停止应用，以固定参考时刻核对个人周期、
原始扣点、审计及持久任务。失败恢复原配置和冻结 Compose，**从不恢复数据库**。
如发现外部修改，保留外部文件并报告。独立 MemSlides、数据库、Redis 和 Caddy 不重启。
证据写入忽略的 `output/`，源码只需对应工具测试及真实隔离 PostgreSQL 工具核对；
原程序、应用源码和版本未变时不重跑前端构建。

## 上传、预检和发布

只向 `fork` 推送。上传前审阅包内 manifest，目标仍为已确认的服务器和部署根目录。将包内文件上传到 `/opt/sub2api/releases/<发布名>/`；工具本身不自动选择 SSH 目标，也不自动触发线上发布。上传失败可以重传同一不可变包，远端 staging 会重新核对 payload 和当前程序基线。

在服务器用系统 `python3`：

```sh
python3 /opt/sub2api/releases/<发布名>/release_server.py --root /opt/sub2api --release /opt/sub2api/releases/<发布名> --stage
python3 /opt/sub2api/releases/<发布名>/release_server.py --root /opt/sub2api --release /opt/sub2api/releases/<发布名>
# 预检结果及业务验证满足发布要求后，执行已获授权的程序切换：
python3 /opt/sub2api/releases/<发布名>/release_server.py --root /opt/sub2api --release /opt/sub2api/releases/<发布名> --apply
```

默认预检不停止服务、不创建备份；`--stage` 仅重建发布目录中的候选程序。预检将有效 Compose 配置哈希与运行容器的 Compose 标签对照，已有配置漂移直接拒绝；经核实的完整解析配置保存在权限受限的备份目录。正常重建和失败回退都使用这份冻结配置，不重新读取可能已变化的 .env/override，也不构建或拉取新镜像。`--apply` 验证当前基线与候选哈希，先备份并通过 `pg_restore --list` 检查 dump，再以 120 秒 grace 停应用、取固定参考时刻快照、硬链接原子切换、只重建 sub2api，检查应用和 MemSlides 及持久数据。

守卫保留原始请求和已结算扣点、确认事件、原钱包起点、纠正/对账/扩容审计、持久预留与计费/异步任务、迁移集合及运行环境。允许后台结算推进已有任务；不允许删除原任务或改写旧已结算请求。程序切换后任何校验失败都会尝试只回退程序并保留当前数据库；即使健康检查已成功也不会跳过回退。不会恢复旧库、清空数据、删历史备份或自动改 enforce。原程序有硬链接时只能新建文件并替换路径，不能原地覆盖。

这里只接受当前部署布局（sub2api、sub2api-postgres，应用 8080、独立 MemSlides 8790）。布局或业务规则变化时先调整和验证工具；不能为绕过预检修改线上配置。数据库备份和原始快照只在服务器受限备份目录保存，不下载到 Git。

## 个人额度完整升级通道

`full_upgrade.py` 是独立的、限定范围的完整升级入口：只接受本次审查的 `260_personal_quota_resets.sql` 及固定校验和，原前端通道仍拒绝后端／迁移变更。完整升级同样需要已提交源码、全部前端证据、实际零跳过额度竞态、最终程序，以及两引擎的实际重置操作回归。

```powershell
& D:/Programs/anaconda3/python.exe tools/review_personal_quota.py --artifact --output output/<本次证据>/feature-browser
& D:/Programs/anaconda3/python.exe tools/full_upgrade.py package --output output/<本次证据> --destination output/releases/<发布名> --version <版本> --baseline-commit <线上源码SHA> --baseline-sha256 <线上程序SHA256> --baseline-binary <已验证旧程序>
```

服务器使用包内入口 `python3 full_upgrade.py deploy --root /opt/sub2api --release <发布目录>`；先 `--stage`，再只读预检，最后按已获授权的发布执行 `--apply`。完整保留既有迁移，只允许新增该迁移；两张个人重置表加入持久数据核对。若新个人重置已实际使用，失败时禁止自动回退到不认识这些记录的旧程序，必须保留数据库并向前修复。

完整升级打包可显式指定 `--full-upload` 复用同一程序生成 gzip，适合差量计算成本大于传输成本时。Compose 2.40 的 `config --hash` 与容器标签可能不一致；此时必须由 `compose --dry-run up --no-deps --no-build --pull never` 的真实变更检测确认唯一结果为当前应用 `Running`，任何创建／重建／其他服务变更都拒绝，冻结配置仍须再次核对。

备份清理仅在用户明确授权后运行 `backup_retention.py --root /opt/sub2api/backups --keep 2` 审阅，再加 `--apply`；先验证保留备份的归档、配置与程序。该清理不属于发布自动步骤。

## 公开资源与记录

统一编程管理员预览使用 `coding_upgrade.py`。该入口固定已审查的应用提交和线上额度重置基线，只接受列出的后端文件变化，禁止新增或改写迁移；保留前端与个人重置通道原有守卫。打包前需要 `artifact-feature-browser` 双浏览器证据及根证据目录内最新实际额度竞态结果。服务切换仍通过冻结 Compose、完整备份、固定时刻持久数据核对和优雅停止。出现统一编程分组后禁止自动回退到不理解该用途的旧程序；撤回入口应停用新分组并保留数据。

```powershell
& D:/Programs/anaconda3/python.exe tools/coding_upgrade.py package --output output/delivery/codex-grok-preview --destination output/releases/mile-20261113-coding-preview1 --version 0.2.20-mile.20261113.coding-preview1 --baseline-sha256 <核实的线上程序SHA256> --baseline-binary <核实的旧程序>
```

服务器使用 `python3 coding_upgrade.py deploy --root /opt/sub2api --release <目录>`，依次 stage、只读预检、已获授权后 apply。此入口不自动创建分组或授权测试用户。

```powershell
& D:/Programs/anaconda3/python.exe tools/check_public_release.py --base-url https://<站点> --dist backend/internal/web/dist --output output/delivery/my-change/public.json --timeout 60
```

核对整个构建静态资源及公开 HTML 引用的入口哈希，同时检查 health/login/public settings。四个并行请求共享总时间预算；仅瞬时网络/过载错误最多三次尝试，哈希不匹配及 404 直接失败。保存每次尝试；浏览器断言失败不自动重试。客户端失败时用服务器同样核对，分别标明客户端网络和服务端结果，不把旧失败当作当前故障，也不以服务器单点通过替代客户端验收。

网络中断后可补验失败资源，保留原报告并写入新文件：

```powershell
& D:/Programs/anaconda3/python.exe tools/check_public_release.py --base-url https://<站点> --dist backend/internal/web/dist --resume output/delivery/my-change/public.json --output output/delivery/my-change/public-resumed.json --timeout 180
```

只接受同一站点、同一构建且 30 分钟内的报告；成功资源须带原始匹配哈希及校验时间。过期资源重新下载，重复补验不能延长原资源的有效期。首页、健康、登录和公共配置每轮重新访问，首页中的资源引用也重新核对。站点／产物不符、原成功缺少匹配哈希时拒绝复用；旧版不含构建指纹的报告需完整核对一次。原失败记录不覆盖，不把服务端单点检查替代客户端核对。

## 工具自身验证

管理员分组的 Fast／Ultra Fast 修改可复用双浏览器场景：

```powershell
& D:/Programs/anaconda3/python.exe tools/review_admin_groups.py --artifact --output output/delivery/my-change/admin-groups
```

该场景只使用 mock API，核对默认关闭、独立开关、显式保存／重开、专属用途保持、取消与
375/768/1440px 布局；开发时以 `--base-url <实际开发地址>` 替代 `--artifact`。

```powershell
$env:PYTHONDONTWRITEBYTECODE='1' # 避免工作树出现未跟踪的工具字节码
& D:/Programs/anaconda3/python.exe -m pip install -r tools/requirements-delivery.txt
$env:RUN_DELIVERY_POSTGRES='1' # Docker Desktop 已运行；只创建自己的临时 PostgreSQL，无生产 DSN
& D:/Programs/anaconda3/python.exe -m unittest discover -s tools/tests -v
node tools/tests/test_browser_fixtures.cjs
```

临时 PostgreSQL 验证的是发布只读快照 SQL，**不替代** `TestCodingQuotaDB -race`。不设置 RUN_DELIVERY_POSTGRES 时该项明确 skip，不能宣称数据库检查通过。CI 单独运行完整工具测试，包括真实临时 PostgreSQL。
