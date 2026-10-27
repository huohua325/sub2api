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
        quota: 'Codex 和 Grok 分别使用个人周额度。同用途的 Key 共用额度；是否耗尽以状态为准，显示四舍五入后的 100% 不一定代表已耗尽。',
        key: '按用途选择分组并创建密钥。创建后会打开接入面板；已有密钥也能通过“使用密钥”重新配置。',
        link: '打开 API 密钥'
      },
      codex: {
        title: 'Codex',
        ready: '选择对应 Key 并保存配置后，在本机启动客户端。下方仅为占位示例。',
        install: '需要本机已经装好 Node.js。Windows 和 macOS 都执行下面这一条，安装最新版 Codex。',
        firstRun: '安装完成后先运行一次 codex。出现登录方式选择时按 Ctrl+C 退出。这一步只为了生成配置目录，不要在这里登录 OpenAI。',
        version: '再执行 codex --version。能打出版本号，说明命令已经可用。',
        files: '目录里只改两个文件。config.toml 写网关和模型，auth.json 写密钥。没有就新建。AGENTS.md 是全局提示词，现在可以先不建。',
        authNote: 'requires_openai_auth = true 表示密钥从 auth.json 读取，不是去登录 OpenAI 官网。',
        sandbox: '接入面板提供系统选择和两种已有认证方式。合并配置时，保留本机已有的沙箱和工具设置。',
        test: '两个文件都保存后，新开一个终端再运行 codex。能收到回复，就说明密钥和网关都通了。',
        configFile: 'config.toml',
        authFile: 'auth.json',
        fields: {
          model: '发给网关的模型 ID。模板默认值不保证已分配给你的分组，可在接入面板主动读取模型目录。',
          model_provider: '本机的提供商名称，必须和下方 model_providers 表中的名称一致。',
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
        ready: '先安装 Grok Build，再保存配置并设置密钥环境变量。下方仅为占位示例。',
        install: 'Windows 打开 PowerShell，执行左边的命令。macOS、Linux 和 WSL 打开终端，执行右边的命令。',
        version: '执行 grok --version。能打出 grok 和一串版本号就成功了。如果提示找不到命令，关掉终端再开一个。',
        files: '配置文件是 config.toml。安装脚本通常已经写了 [cli]、[marketplace] 和 [ui]。这些段保留，只把模型相关的段补进去，同一段不要写两遍。目录里还没有这个文件时，按下面整份新建。',
        envKey: '模板从 XAI_API_KEY 环境变量读取密钥。在启动 Grok 的同一终端中执行下面的环境设置，然后运行客户端。',
        effort: '模型和思考等级以当前客户端及分组权限为准。修改配置或环境变量后，退出并重新启动 Grok Build。',
        search: '搜索与生图等可选能力取决于分组。若不支持服务端搜索，请将 supports_backend_search 设为 false。',
        inspect: '不确定客户端有没有读到文件时，先运行 grok inspect，看配置来源是不是这份 config.toml。',
        test: '然后运行 grok，随便发一句。也可以用下面这条，只要求回复 ok，用来确认网关和密钥。',
        configFile: 'config.toml',
        fields: {
          default: '新会话默认使用的模型。',
          web_search: '客户端搜索工具使用的模型，和 default 保持同一个。',
          models_base_url: '本站的接口地址，末尾是 /v1。',
          model: '真正发给接口的模型 ID，要和 [models] 里的名字一致。',
          name_description: '模型选择器里显示的名称和说明。',
          api_key: 'env_key 指定环境变量名 XAI_API_KEY；在终端中设置它的实际值。',
          api_backend: '必须写 responses。不写时，客户端会改走 /v1/chat/completions。',
          context_window: '客户端据此决定何时压缩对话，实际上下文能力以模型为准。',
          supports_reasoning_effort: '声明这个模型可以调整思考等级。不写这行，/effort 对它不起作用。',
          supports_backend_search: '可选能力声明。仅在分组支持服务端搜索时启用。',
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
      long55: '超过 27.2 万 token：输入 6，输出 27，缓存 0.6',
      long272: '超过 27.2 万 token 按两倍',
      tier: '更长上下文按官网更高一档'
    }
  }
}
