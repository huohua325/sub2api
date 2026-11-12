# MILE Lab 交付工具

第一阶段保持现有嵌入式前端架构、额度保护和 shadow 模式。工具使用 Python 3.11+ 标准库；生成差量包时额外安装 `requirements-delivery.txt`。以下命令在仓库根目录执行，PowerShell 普通工具调用应禁用 profile 加载。Python 使用 `D:/Programs/anaconda3/python.exe`，pnpm 使用 `pnpm.cmd`。

## 日常修改

1. 先写清本次用户操作和预期结果，修改后先测相关用例及真实页面交互。
2. 启动开发服务器：`pnpm.cmd --dir frontend dev --host 127.0.0.1`。检查输出中的实际端口。
3. 执行完整前端验证及一次最终构建：

   ```powershell
   & D:/Programs/anaconda3/python.exe tools/delivery.py validate --review-url http://127.0.0.1:3000 --output output/delivery/my-change
   ```

   顺序为 frozen install → 指南核心路径 Chromium 快验 → lint/full Vitest/`vue-tsc -b`（最多两项并行）→ Vite 构建。全量 Vitest 已含 i18n，故不再额外调用一次；日常 `pnpm build` 完整入口保持原样。默认指南场景是公共导航基线，**不替代本次变更特有的用户路径**；例如密钥权限、支付和管理操作必须补充相应已有回归。

4. 对最终构建执行真实产物回归：

   ```powershell
   & D:/Programs/anaconda3/python.exe tools/delivery.py review-artifact --output output/delivery/my-change
   ```

   临时本机 SPA server 自动启动和关闭，Chromium/WebKit 验证 16 次导航、历史/深链接/刷新/焦点、375/768/1440px、复制。接口共用 mock fixtures，真实业务写入和推理请求被拦截。页面等待 DOM 及业务元素就绪，不等完整 `load`。首次需准备 Node、npx、`@playwright/cli` 和浏览器；工具使用 `npx --no-install`，缺失会失败，不会暗中安装新依赖。

## 结果复用与计时

同一个 `--output` 下保留历史 `manifest.json` 和每次原始日志。只有输入文件内容（含锁文件/配置）、完整参数、运行目录、环境摘要、工具版本、执行文件，以及产物/日志哈希都匹配，lint/tests/types/build 才会复用。新增/删除文件、产物损坏、执行失败、运行中编辑输入都会导致失效或失败。环境只存摘要，不把 DSN 和密钥写入 manifest；原始命令输出仍需按敏感证据管理，留在忽略的 output 内。

这是本地可信工作区的效率工具，不是防篡改签名系统。不可把其他人的手写 manifest 当作验证证据。依赖目录在扫描前剪枝，仅锁文件和依赖元数据参与指纹；编译生成的 vite.config.js/.d.ts 和 tsbuildinfo 不当作源文件，构建明确使用 vite.config.ts。依赖安装每轮执行 frozen 检查；浏览器和额度竞态每次实际执行。`--fresh` 强制重跑前端检查。只读预检可并行准备，不能并行修改同一输出目录；锁存在时先确认持有进程状态，工具不会自动删锁。

```powershell
& D:/Programs/anaconda3/python.exe tools/delivery.py summary --output output/delivery/my-change
```

摘要直接使用证据生成，不会重新构建。源码提交和后续发布记录提交各自保留，文档提交本身不触发程序构建。

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

默认生成差量包并在本地重建验证；需要 `bsdiff4==1.4.1`。只有显式 `--full-upload` 才选择完整 gzip，不会因缺依赖自动扩大上传量。包内包括程序/payload 哈希、前端文件清单、验证证据摘要及服务器工具，目录不可覆盖。打包不重跑编译。**后端或迁移相对基线有变化时，这条发布通道拒绝执行，改走完整升级 runbook。** 不自动从旧的 exit.txt、固定日期或固定测试数量生成通过结论。

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

## 公开资源与记录

```powershell
& D:/Programs/anaconda3/python.exe tools/check_public_release.py --base-url https://<站点> --dist backend/internal/web/dist --output output/delivery/my-change/public.json --timeout 60
```

核对整个构建静态资源及公开 HTML 引用的入口哈希，同时检查 health/login/public settings。四个并行请求共享总时间预算；仅瞬时网络/过载错误最多三次尝试，哈希不匹配及 404 直接失败。保存每次尝试；浏览器断言失败不自动重试。客户端失败时用服务器同样核对，分别标明客户端网络和服务端结果，不把旧失败当作当前故障，也不以服务器单点通过替代客户端验收。

## 工具自身验证

```powershell
$env:RUN_DELIVERY_POSTGRES='1' # Docker Desktop 已运行；只创建自己的临时 PostgreSQL，无生产 DSN
& D:/Programs/anaconda3/python.exe -m unittest discover -s tools/tests -v
node tools/tests/test_browser_fixtures.cjs
```

临时 PostgreSQL 验证的是发布只读快照 SQL，**不替代** `TestCodingQuotaDB -race`。不设置 RUN_DELIVERY_POSTGRES 时该项明确 skip，不能宣称数据库检查通过。CI 单独运行完整工具测试，包括真实临时 PostgreSQL。
