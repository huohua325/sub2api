export default {
  guide: {
    title: '开始使用',
    lead: '用自己的密钥调用 Codex 和 Grok。地址是本站，模型名按这把密钥的分组填写。',
    tocLabel: '步骤',
    copy: '复制',
    copied: '已复制',
    keyToken: '你的密钥',
    grokModelToken: '分组里的模型名',
    dashboardHint: '还没有密钥？看开始使用',
    keysHint: '第一次调用看这里',
    steps: {
      login: {
        title: '登录',
        body: '账号由邀请或管理员开通。打开本站，用邮箱登录。'
      },
      quota: {
        title: '看周限额',
        body: '仪表盘上 Codex 和 Grok 各有一条已用百分比，分母是各自的周限额。用到 100% 就停，到页面上的刷新时间再打开。两条分开算，互不影响。'
      },
      key: {
        title: '创建密钥',
        body: '打开 API 密钥，新建一把，立刻复制。一把密钥只走一个分组。Codex 和 Grok 不在同一分组时，各建一把。',
        link: '打开 API 密钥'
      },
      call: {
        title: '发出第一次调用',
        body: '地址用本站的 /v1。下面两段命令可以直接复制。密钥换成刚复制的那一把。Grok 的模型名换成这把密钥所属分组允许的名字。',
        client: 'Codex 客户端和 Grok CLI 不要在这里手改地址。回到这把密钥，点「使用」，那里的配置已经按客户端生成。',
        codex: 'Codex',
        grok: 'Grok'
      },
      check: {
        title: '核对这次调用',
        body: '使用记录里能看到刚才的模型、token 和实扣。百分比以仪表盘上那两条周限额为准。',
        usage: '打开使用记录',
        dashboard: '回到仪表盘'
      }
    },
    troubleshoot: {
      title: '调用失败时先看这三件事',
      quota: '周限额是不是已经用完',
      group: '密钥是不是这个分组的',
      model: '模型名是不是该分组允许的'
    },
    manual: {
      lead: 'Codex 和 Grok Build 都可以用。先建密钥，再按下面的顺序安装、写配置、试一次。',
      windows: 'Windows',
      unix: 'macOS / Linux',
      openFolder: '打开配置目录',
      missingFile: '目录里没有这个文件就新建一个，再把内容整段贴进去。',
      keyHint: '示例里的 sk-你的密钥只是占位。换成你刚复制的整把密钥，不要发到聊天或截图里。',
      fieldsTitle: '这些配置是什么意思',
      prep: {
        title: '准备密钥',
        login: '用管理员开通的邮箱注册并设置密码，然后登录。',
        quota: '仪表盘上 Codex 和 Grok 各有一条周限额。用到 100% 就停，到页面上的刷新时间再打开。两条分开算。',
        key: 'Codex 和 Grok Build 各建一把密钥。创建时用途分别选 Codex、Grok Build。密钥只显示一次，马上复制。',
        link: '打开 API 密钥'
      },
      codex: {
        title: 'Codex',
        ready: '现在可以直接使用。',
        install: '需要本机已经装好 Node.js。Windows 和 macOS 都执行下面这一条，安装最新版 Codex。',
        firstRun: '安装完成后先运行一次 codex。出现登录方式选择时按 Ctrl+C 退出。这一步只为了生成配置目录，不要在这里登录 OpenAI。',
        version: '再执行 codex --version。能打出版本号，说明命令已经可用。',
        files: '目录里只改两个文件。config.toml 写网关和模型，auth.json 写密钥。没有就新建。AGENTS.md 是全局提示词，现在可以先不建。',
        authNote: 'requires_openai_auth = true 表示密钥从 auth.json 读取，不是去登录 OpenAI 官网。',
        sandbox: '[windows] sandbox = "elevated" 只在 Windows 生效，第一次会要一次管理员授权。授权失败时改成 unelevated，程序仍能运行，只是隔离弱一些。macOS 和 Linux 删掉整段 [windows] 即可。',
        test: '两个文件都保存后，新开一个终端再运行 codex。能收到回复，就说明密钥和网关都通了。',
        configFile: 'config.toml',
        authFile: 'auth.json',
        fields: {
          model: '发给网关的模型名。当前填写 gpt-6.1-sol。',
          model_provider: '本机给这个网关起的名字，必须和下方 [model_providers.mile] 的 mile 相同。',
          model_reasoning_effort: '默认思考等级。xhigh 比普通档更仔细，也更慢。',
          model_verbosity: '回答详细程度。high 会写得更完整。',
          disable_response_storage: '不把对话存到 OpenAI。经过本站时保持 true。',
          web_search_request: '允许 Codex 发起网页搜索。',
          base_url: '本站的接口地址，末尾是 /v1。',
          wire_api: '协议固定为 responses。',
          requires_openai_auth: 'true 时从 auth.json 读 OPENAI_API_KEY。密钥仍是本站创建的 sk- 密钥。',
          screen_reader_detection_done: '跳过第一次打开时的读屏检测，避免多停一步。'
        }
      },
      grok: {
        title: 'Grok Build',
        ready: '现在可以直接使用。安装脚本不需要 Node.js。',
        install: 'Windows 打开 PowerShell，执行左边的命令。macOS、Linux 和 WSL 打开终端，执行右边的命令。',
        version: '执行 grok --version。能打出 grok 和一串版本号就成功了。如果提示找不到命令，关掉终端再开一个。',
        files: '配置文件是 config.toml。安装脚本通常已经写了 [cli]、[marketplace] 和 [ui]。这些段保留，只把模型相关的段补进去，同一段不要写两遍。目录里还没有这个文件时，按下面整份新建。',
        envKey: '不想把密钥写进文件时，删掉 api_key 那一行，改成 env_key = "MILE_API_KEY"。然后在系统环境变量里设置 MILE_API_KEY。等号后面是变量名，不是密钥。',
        effort: 'supports_reasoning_effort = true 之后，会话里输入 /effort 调整思考等级，输入 /model grok-4.7 切换模型。无头模式用 grok -p "问题" -m grok-4.7 --effort 档位。档位以 /effort 菜单里列出的为准。改完文件要退出并重新打开 Grok Build。',
        search: 'supports_backend_search = false 是因为本站不提供 Grok 服务端搜索。客户端自己的搜索仍使用上面的 web_search = "grok-4.7"，请求还是发到本站。',
        inspect: '不确定客户端有没有读到文件时，先运行 grok inspect，看配置来源是不是这份 config.toml。',
        test: '然后运行 grok，随便发一句。也可以用下面这条，只要求回复 ok，用来确认网关和密钥。',
        configFile: 'config.toml',
        fields: {
          default: '新会话默认使用的模型。',
          web_search: '客户端搜索工具使用的模型，和 default 保持同一个。',
          models_base_url: '本站的接口地址，末尾是 /v1。',
          model: '真正发给接口的模型 ID，要和 [models] 里的名字一致。',
          name_description: '模型选择器里显示的名称和说明。',
          api_key: '这把 Grok Build 密钥。创建密钥时用途选 Grok Build。',
          api_backend: '必须写 responses。不写时，客户端会改走 /v1/chat/completions。',
          context_window: '上下文长度。grok-4.7 写 500000，客户端据此决定何时压缩对话。',
          supports_reasoning_effort: '声明这个模型可以调整思考等级。不写这行，/effort 对它不起作用。',
          supports_backend_search: '声明网关是否提供 Grok 服务端搜索。本站不提供，所以写 false。',
          installer: '官方脚本写成 internal，之后用 grok update 升级。文件里已经有这一行就不要再写一行。',
          auto_update: '允许客户端自己检查更新。',
          yolo: '减少执行命令前的确认。compact_mode = false 保持普通界面。',
          marketplace: '官方插件来源。安装脚本写过就保留，不要把同一段再贴一遍。'
        }
      },
      check: {
        title: '核对这次调用',
        body: '使用记录里能看到刚才的模型和实扣。百分比以仪表盘上的周限额为准。',
        usage: '打开使用记录',
        dashboard: '回到仪表盘'
      }
    }
  },
  models: {
    title: '模型',
    lead: '把名字填进 model。下面是元/百万 token。个人额度看仪表盘。',
    search: '搜索模型名',
    count: '{count} 个模型',
    empty: '没有这个模型名',
    copy: '复制',
    copied: '已复制',
    familyLabel: '家族',
    input: '输入',
    output: '输出',
    cache: '缓存',
    unit: '元/百万 token',
    footnote: '密钥要建在对应分组。标了高峰或超长的模型，按卡片上的说明加价。',
    families: {
      all: '全部',
      gpt: 'GPT',
      grok: 'Grok',
      claude: 'Claude',
      gemini: 'Gemini',
      qwen: 'Qwen',
      kimi: 'Kimi',
      deepseek: 'DeepSeek',
      glm: 'GLM'
    },
    notes: {
      peak: '工作日 9:00–12:00、14:00–18:00 按两倍',
      long2: '20 万 token 以上按两倍',
      long3: '超过 25.6 万 token 按三倍',
      long5: '超过 10 万 token 按五倍',
      long55: '超过 27.2 万 token：输入 6，输出 27，缓存 0.6'
    }
  }
}
