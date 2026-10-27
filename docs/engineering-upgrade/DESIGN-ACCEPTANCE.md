# MILE Lab 浅色光谱设计系统：交付与验收

验收日期：2026-10-27。设计规范见 [DESIGN-SYSTEM.md](DESIGN-SYSTEM.md)。本轮交付包括主题变量、本地 Sora、公共控件与布局、光谱首页和仪表盘、模型/Key/接入指引/认证页面、其余页面的公共样式适配、管理员公共框架及开发组件展示页。

## MI 品牌主视觉更新

同日根据品牌反馈，将原曲线造型替换为 **MI 光学棱柱**。M 为连续折面，I 为独立棱柱，保留青紫光谱、透光高光、轻微悬浮与原动画暂停机制；调整工作台欢迎区的尺寸与位置，避免裁掉字母底部。实现入口仍为 `frontend/src/components/common/SpectrumArt.vue`。

本次局部更新通过 SpectrumArt 的 3 项生命周期测试、类型检查、组件只读 lint 和生产 Vite 构建。Chromium、Firefox、WebKit 分别检查桌面首页、移动首页、深色首页、工作台和登录页，共 15 组截图，无页面横向溢出、页面错误或 JS 异常。该轮使用开发服务器与固定 mock；下文完整系统回归、生产性能数值及旧截图保留为首次交付记录，没有将其冒充为更换图案后的重新测量。

更新后的素材：[主图细节](../../output/playwright/design-system/mi-prism-detail.png)、[透明 SVG](../../output/playwright/design-system/mi-prism.svg)、[桌面首页](../../output/playwright/design-system/chromium-mi-home-1440.png)、[移动首页](../../output/playwright/design-system/chromium-mi-home-375.png)、[工作台](../../output/playwright/design-system/chromium-mi-dashboard-1440.png)、[深色首页](../../output/playwright/design-system/chromium-mi-home-dark.png)。独立 SVG 约 4.1 kB，无字体或位图依赖。

## 实现范围

- 新访客默认浅色；沿用 `theme` 存储键、保留深色选择，首绘前恢复主题。统一首页、认证页和工作台的主题入口。
- CSS 语义变量映射 Tailwind 3；旧公共样式类继续兼容。内容、浮层、主视觉三种表面共用颜色、圆角、阴影和动效规范。
- 学员导航按工作台、使用情况、我的账户分组；保留原权限、功能开关与简单模式过滤，管理员个人区复用同一定义。
- 首页与仪表盘使用独立 SVG 装饰；三类额度沿用原计算逻辑、显示真实百分比和重置时间，未知读数无进度填充。
- 公共按钮、标题、表单、菜单、表格、分页、弹窗、状态、Toast、Skeleton 和空状态统一。其余学员与管理员页面通过公共布局和控件接入，复杂业务页保留原构图。
- 弹窗与移动导航支持键盘焦点约束、Esc 和关闭后返回；补齐 WebKit 点击按钮不自动聚焦时的兼容处理。
- 路由继续懒加载，模型和接入指引加入预加载；节省流量模式关闭推测下载。图表、支付 SDK、导出分别分包。
- 开发环境访问 `/__design-system`，查看控件、亮暗主题与窄屏状态；生产构建不包含该展示页。

后端接口、鉴权、权限、计费与额度规则未因本轮设计工作调整。工作区原有业务修改保留。

## 检查结果

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 相关组件、布局、组合函数、学员/认证与管理员回归 | 73 个测试文件、476 项测试通过 | `output/playwright/design-system/tests.log` |
| 国际化键完整性 | 3 项通过 | `after-build.log` 中 `check:i18n` |
| TypeScript | 通过 | `final-typecheck.log` |
| 只读 ESLint | 0 error，1 个既有测试未使用变量 warning | `lint.log` |
| 生产构建 | 通过；最终补丁另经类型检查与 Vite 构建 | `after-build.log`、`final-vite-build.log` |
| 差异空白检查 | `git diff --check` 通过 | 本地工作区检查 |
| 生产展示页隔离 | 无 `DesignSystemView` 产物 | `after-assets.json` |

既有 lint warning 位于 `components/admin/usage/__tests__/UsageMonitor.spec.ts` 的 `dialogMessage`；构建仍提示原有 AccountsView 分包超过 500 kB。

## 浏览器、交互与请求

使用 Playwright CLI 的隔离浏览器上下文，所有业务 API 均由固定 mock 返回；Key 创建只记录 mock POST。没有向真实账户或支付后台写入数据。

| 浏览器引擎 | 记录检查数 | 页面溢出 / JS 异常 / 页面错误横幅 | 弹窗焦点约束及返回 | 移动抽屉焦点约束及返回 |
| --- | ---: | --- | --- | --- |
| Chromium | 52 | 0 / 0 / 0 | 通过 | 通过 |
| Firefox | 50 | 0 / 0 / 0 | 通过 | 通过 |
| WebKit | 50 | 0 / 0 / 0 | 通过 | 通过 |

三引擎检查首页、仪表盘、模型、Key、指引、登录、注册和找回密码的 375、768、1440 CSS px 布局；另检查模型页 720 CSS px 的 200% 等效重排。该方式验证 1440px 显示区放大后的布局，不等同于操作系统或浏览器原生缩放的全部字体栅格行为。深色首页/仪表盘/模型、管理员用户页、Key 弹窗和移动导航有单独截图。

登录后跳转工作台、模型筛选、Key 创建、额度未知/耗尽/失败反馈均通过；Chromium 另外验证真实浏览器剪贴板中的模型 ID 和首页配置文本。三引擎的未知额度均无填充进度，失败状态存在 alert，减少动态效果时动画为 `none`，装饰离开视口后 `is-active=false`。页面隐藏和监听器清理另由组件测试覆盖。

仪表盘初始独立上下文请求与改造前保持一致：`/auth/me`、`/announcements`、两次不同参数的 `/keys` 和一次 `/user/platform-quotas`。三种宽度均没有提前请求历史 stats/trend/usage；历史展开及请求次数约束继续由 `DashboardView.request.spec.ts` 验证。视觉装饰不发请求。多页面导航的检查记录可能累计多次 auth 请求，不能用其累计列表代表单次首屏请求数。

功能开关、简单模式、原有首页覆盖逻辑、Key 表单与操作、支付结果和管理员监控依靠现有相关单测回归。真实支付供应商托管页面、真实上游额度和生产网络未在本次 mock 浏览器验收中实测；外部 iframe 与供应商页面仅统一外框。

## 性能对照

设备：Windows / Intel Core Ultra 7 255H，同机本地 HTTP，生产构建，固定 mock，无 CPU/网络节流。改造前构建保存在 `baseline-dist`；改造后使用 `backend/internal/web/dist`。Chromium 在其他引擎运行前顺序采样，每个页面、每个版本创建 3 个全新上下文；等待字体就绪并保留固定 800ms 观察窗。结果用于本地相对比较，不代表线上用户分位数或完整 Lighthouse 审计。

| 页面 | 改造前 LCP 中位数 | 改造后 LCP 中位数 | 改造前 CLS 最大值 | 改造后 CLS 最大值 |
| --- | ---: | ---: | ---: | ---: |
| 首页 | 172ms | 200ms | 0 | 0.0043 |
| 仪表盘 | 244ms | 236ms | 0.0098 | 0.0173 |

在本地条件下满足 LCP ≤2.5s、CLS ≤0.1。首页 LCP 增加 28ms，仪表盘减少 8ms；样本较少，不据此宣称全面提速。改造后普通学员首屏资源记录中没有 `vendor-chart`、`vendor-export`、`vendor-airwallex` 或 Stripe SDK。

| 引擎 | 模型筛选响应 p95 | 弹窗打开响应 p95 |
| --- | ---: | ---: |
| Chromium | 39.2ms | 15ms |
| Firefox | 33ms | 47ms |
| WebKit | 76ms | 120ms |

每项各 20 次，测量从派发操作到两次 `requestAnimationFrame` 的时间，均 ≤200ms；这是合成界面更新延迟，并非真实用户 INP。Firefox 与 WebKit 检查并行运行，因此表中数字不用于比较浏览器优劣。Chromium 动画 60 帧采样的 rAF 间隔 p95 为 8.7ms；此值不是 GPU/主线程实际渲染 CPU 耗时。

| gzip 资源指标 | 改造前 | 改造后 | 净变化 |
| --- | ---: | ---: | ---: |
| HTML 引用的入口 JS，含主题初始化 | 217,629 B | 215,088 B | −2,541 B |
| HTML 引用的入口 CSS | 37,048 B | 40,256 B | +3,208 B |
| 全构建 JS 分包合计 | — | — | +6,513 B |
| 全构建 CSS 分包合计 | — | — | +3,364 B |

以上是实际产物净差，含分包调整和页面重构，不是逐模块归因；入口 JS 未增加，CSS 新增约 3.13 KiB，均在新增全局 JS 35 KiB / CSS 25 KiB 预算内。Sora WOFF2 为 25,284 B；装饰为内联 SVG，没有新增位图、视频或 WebGL 资源。字体与装饰规模低于 200 KiB 预算。

## 截图和原始记录

所有截图、基线和测量 JSON 在本地被忽略的 `output/playwright/design-system/`，保留供本机复核；规范与复现工具可随源码提交。

| 页面 | 桌面 | 移动 / 其他 |
| --- | --- | --- |
| 光谱首页 | [1440px](../../output/playwright/design-system/chromium-home-1440.png) | [375px](../../output/playwright/design-system/chromium-home-375.png) |
| 学员仪表盘 | [1440px](../../output/playwright/design-system/chromium-dashboard-1440.png) | [375px](../../output/playwright/design-system/chromium-dashboard-375.png) |
| 模型目录 | [1440px](../../output/playwright/design-system/chromium-models-1440.png) | [200% 等效重排](../../output/playwright/design-system/chromium-models-200-percent-reflow.png) |
| 认证 | [登录](../../output/playwright/design-system/chromium-login-1440.png) | [移动登录](../../output/playwright/design-system/chromium-login-375.png) |
| 接入与操作 | [Key 弹窗](../../output/playwright/design-system/chromium-key-dialog.png) | [移动导航](../../output/playwright/design-system/chromium-navigation-375.png) |
| 管理员与深色 | [用户管理 / WebKit](../../output/playwright/design-system/webkit-admin-users.png) | [深色仪表盘](../../output/playwright/design-system/chromium-dashboard-dark.png) |
| 组件展示 | [1440px](../../output/playwright/design-system/chromium-showcase-1440.png) | [375px](../../output/playwright/design-system/chromium-showcase-375.png) |

原始结果：[汇总](../../output/playwright/design-system/summary.json)、[Chromium](../../output/playwright/design-system/chromium-review.json)、[Firefox](../../output/playwright/design-system/firefox-review.json)、[WebKit](../../output/playwright/design-system/webkit-review.json)。

## 复现

前置条件：已安装项目依赖和对应 Playwright 浏览器。保留已有 `baseline-dist`，不要用改造后的构建覆盖基线。

```powershell
# frontend/：只读检查与生产构建
pnpm run typecheck
pnpm run lint:check
pnpm run check:i18n
pnpm run build

# 仓库根目录：分别在终端启动两个仅本机监听的 SPA 服务
python tools/serve_frontend_review.py output/playwright/design-system/baseline-dist 4174
python tools/serve_frontend_review.py backend/internal/web/dist 4173

# frontend/：先打开命名会话，再运行 CLI harness
npx --yes --package @playwright/cli playwright-cli -s=mile open --browser=chrome
npx --yes --package @playwright/cli playwright-cli -s=mile run-code --filename ../tools/review_mile_frontend.js *> ../output/playwright/design-system/chromium-review.log

npx --yes --package @playwright/cli playwright-cli -s=firefox open --browser=firefox
npx --yes --package @playwright/cli playwright-cli -s=firefox run-code --filename ../tools/review_mile_frontend.js *> ../output/playwright/design-system/firefox-review.log

npx --yes --package @playwright/cli playwright-cli -s=webkit open --browser=webkit
npx --yes --package @playwright/cli playwright-cli -s=webkit run-code --filename ../tools/review_mile_frontend.js *> ../output/playwright/design-system/webkit-review.log

# 全部运行结束后，在仓库根目录生成 JSON 与 gzip 汇总
python tools/summarize_frontend_review.py

# frontend/：组件展示页仅在开发环境提供
pnpm dev --host 127.0.0.1 --port 5173
# 浏览 http://127.0.0.1:5173/__design-system
```

测试采用 Vitest 2 和 Vue Test Utils；新增重点为主题存储、预加载节省流量模式、导航分组、按钮行为、光谱动画生命周期及弹窗焦点循环，保留既有业务请求测试。
