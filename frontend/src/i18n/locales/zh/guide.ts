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
    }
  },
  models: {
    title: '模型',
    lead: '把名字填进 model。额度看仪表盘。',
    search: '搜索模型名',
    count: '{count} 个模型',
    empty: '没有这个模型名',
    copy: '复制',
    copied: '已复制',
    familyLabel: '家族',
    useLabel: '用途',
    footnote: '密钥要建在对应分组，分组随后由管理员添加。GPT 与 Codex 共用仪表盘上的 OpenAI 周限额，Grok 与 Grok Build 共用 Grok 周限额。',
    families: {
      all: '全部',
      gpt: 'GPT',
      grok: 'Grok',
      qwen: 'Qwen',
      kimi: 'Kimi',
      deepseek: 'DeepSeek',
      glm: 'GLM'
    },
    uses: {
      all: '全部用途',
      chat: '对话',
      coding: '编程',
      image: '图像',
      video: '视频'
    },
    sources: {
      subscription: '订阅账号',
      experiment: '实验接口'
    }
  }
}
