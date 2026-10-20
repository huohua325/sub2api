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
      keyHint: '把 sk-你的密钥换成刚复制的密钥。不要把真实密钥发到聊天或截图里。',
      prep: {
        title: '准备密钥',
        login: '用管理员开通的邮箱注册并设置密码，然后登录。',
        quota: '仪表盘上 Codex 和 Grok 各有一条周限额。用到 100% 就停，到页面上的刷新时间再打开。两条分开算。',
        key: 'Codex 和 Grok Build 各建一把密钥。创建时用途分别选 Codex、Grok Build。密钥只显示一次，马上复制。',
        link: '打开 API 密钥'
      },
      codex: {
        title: 'Codex',
        ready: '服务已经齐全，现在可以调用。',
        install: '先安装 Node.js，再全局安装 Codex。装好后运行一次，看到登录选择就按 Ctrl+C 退出。这一步只是为了生成配置目录。',
        version: '能打出版本号就说明安装成功。',
        files: '配置目录里用到两个文件。config.toml 指定网关和模型，auth.json 放密钥。',
        sandbox: 'Windows 的沙盒写成 elevated。如果电脑不允许管理员授权，把这一行改成 unelevated。macOS 和 Linux 可以不写 [windows] 这一段。',
        test: '保存后重新打开终端，运行 codex。能对话就通了。',
        configFile: 'config.toml',
        authFile: 'auth.json'
      },
      grok: {
        title: 'Grok Build',
        ready: '服务已经齐全，现在可以调用。安装脚本不需要 Node.js。',
        install: '用官方脚本安装。装完如果提示找不到命令，关掉终端再开一个。',
        version: '能打出 grok 和版本号就说明安装成功。',
        files: '配置在 config.toml。没有这个文件就新建。如果安装脚本已经写了 [cli]，模型配置补进去即可，installer 那行不用重复写。',
        effort: '写了 supports_reasoning_effort 之后，会话里用 /effort 调思考等级，用 /model grok-4.7 换模型。改完配置要重启 Grok Build。',
        search: 'supports_backend_search 保持 false。网关不做服务端搜索，客户端自己的 web_search 仍走 grok-4.7。',
        test: '运行 grok 随便说一句。也可以用下面这条确认网关和密钥都通。',
        configFile: 'config.toml'
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
    useLabel: '用途',
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
    uses: {
      all: '全部用途',
      chat: '对话',
      coding: '编程'
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
