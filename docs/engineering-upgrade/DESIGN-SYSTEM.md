# MILE Lab · 浅色光谱实验室

本轮实现以视觉品质、学员任务与响应速度为先。新访客默认浅色；已有 `theme=dark` 继续生效。首页保留自定义 HTML、iframe 和精简首页的优先级；导航、权限、计费、额度含义沿用已有实现。

## 主题与材质

唯一主题入口是 `frontend/src/styles/tokens.css`。Tailwind 3 的 primary、gray、dark 色阶映射到 CSS 变量；`design-system.css` 为旧的 `.btn`、`.input`、`.card`、`.table`、`.dropdown`、`.modal-*` 提供兼容样式。页面新增样式使用语义变量，避免再次定义一套页面主题。

| Token | 浅色 | 作用 |
| --- | --- | --- |
| `--color-page` | `#F5F7FC` | 珍珠底色 |
| `--color-surface` | `#FFFFFF` | 内容、输入和表格 |
| `--color-ink` | `#202941` | 标题与正文 |
| `--color-muted` | `#62708A` | 说明与时间 |
| `--color-brand` | `#6750D8` | 选中、链接、焦点 |
| `--color-cyan` | `#39C6D6` | 光谱装饰 |
| `--color-success/warning/danger` | 独立语义色 | 状态，配合文字说明 |

内容表面保持不透明；侧栏、顶栏与菜单使用局部透明和细描边；首页、欢迎区使用光谱曲面。厂商标志保留原色，装饰色不参与额度计算和账号健康判断。

品牌主视觉采用 **MI 光学棱柱**：M 为连续折面，I 为独立棱柱，使用蓝紫、冰青与珍珠高光形成厚度、倒角和透光层次。主图案由 `SpectrumArt.vue` 的 SVG 路径绘制，不依赖字体。首页完整展示字母，工作台欢迎区保留完整轮廓，移动工作台降低装饰透明度；MILE Lab 正式标志继续承担完整名称识别。动效只做轻微悬浮，避免改变字母可读性。

控件、面板、重点展示圆角分别为 10、16、24px。4px 为间距基准。正文 14–16px，页面标题 28–32px，首页标题最高 64px，额度 40–56px。移动端主操作保持 44px 触控区域，长代码在容器内横向滚动，阅读区允许长字段换行。

Sora 拉丁可变字体自托管于 `frontend/src/assets/fonts/`，25,284 bytes，保留 `Sora-OFL.txt`；中文使用系统字体，无外部字体服务请求。

字体来源：[Google Fonts Sora](https://github.com/google/fonts/tree/main/ofl/sora)，[WOFF2 子集](https://fonts.gstatic.com/s/sora/v17/xMQ9uFFYT72X5wkB_18qmnndmSdSnh2BAfO5mnuyOo1lfiQwV6-x.woff2)。SHA-256：`811e11966d29f3a01fcb19b087b61ac067d380665a55aebb7fcdf2cda95e4a93`。

## 可复用入口

| 入口 | 用法 |
| --- | --- |
| `BaseButton` | `variant="primary\|secondary\|ghost\|danger"`，可选 loading、disabled、size、原生 type/attrs；默认 type=button |
| `PageHeader` | title、可选 description 与 actions 插槽，提供页面 h1 |
| `SpectrumArt` | 纯装饰 SVG，不请求数据、不计算额度、不读取权限 |
| `useTheme` | 共享浅/深色状态，读取原有 theme 存储键，支持跨标签同步与存储不可用降级 |
| `usePageMetadata` | 顶栏与页面标题共用路由标题、计费模式和自定义页面名称 |
| `AppLayout` | 默认显示 PageHeader；已有专属标题的页面可传 `:page-header="false"` |
| `TablePageLayout` | 可选 header 插槽；按顶栏与实际页面标题高度保留表格空间 |
| `groupStudentNavigation` | 对已通过权限/功能过滤的项目分组；保留自定义项，隐藏空组 |

现有 Select、SearchInput、BaseDialog、DataTable、Pagination、StatusBadge、Toast、Skeleton、EmptyState 的调用契约保持兼容。BaseDialog 与移动导航约束键盘焦点、支持 Esc 关闭、返回触发位置。管理员使用同一主题与控件，表格采用更紧凑的行间距。

仅开发环境注册 `/__design-system`，集中展示排版、表面、按钮、表单、状态、表格、加载/空状态和弹窗。生产路由与构建不会包含该页面。

## 页面与导航

学员导航先经过原有权限、简单模式和功能开关过滤，再分为工作台、使用情况、我的账户。管理员个人区使用相同定义。桌面可折叠，移动端是同一棵导航树的抽屉。

首页以 SVG 折射曲面建立品牌视觉，提供工作台主入口和 Codex／Grok／API 实验三条路径，长配置按需展开、保留复制。仪表盘将欢迎区、三类额度、接入快捷操作、历史统计分层组织。未知额度没有百分比进度条；失败可重新读取；额度与装饰分别渲染。

模型页保留已有模型目录、价格、搜索与家族筛选；没有新增速度或推荐等级。Key 页围绕创建、复制与接入组织；危险操作与表单逻辑沿用原实现。认证页使用品牌叙事与表单双栏，移动端聚焦表单。其余学员页面和管理员页面通过公共布局、标题、控件及主题色统一；供应商托管界面和自定义 iframe 只处理外框。

## 动效与加载

控件反馈 160ms，浮层 220ms，欢迎区与首页入场 450–550ms。持续动画仅改变 transform；页面隐藏、装饰离开视口、移动端或 prefers-reduced-motion 时静止。交互不等待入场动画。

主题初始化脚本 `public/theme-init.js` 在样式首绘前执行。路由继续懒加载；模型和接入指引补入邻接预加载，saveData 下禁用推测下载。图表、xlsx 导出、Stripe 与 Airwallex SDK 分包。历史统计仍由展开动作触发，视觉层不新增额度接口或统计接口调用。

## 扩展规则

新增页面优先使用 PageHeader、lab-panel 与公共控件；新颜色进入 tokens，状态同时提供文字；新装饰不能拥有数据请求。新增动效要包含静态版本与卸载清理。新的功能导航必须先进入原权限过滤，再进入共享分组。

验收和复现方法见 [DESIGN-ACCEPTANCE.md](DESIGN-ACCEPTANCE.md)。
