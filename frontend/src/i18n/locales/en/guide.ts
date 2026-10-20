export default {
  guide: {
    title: 'Get started',
    lead: 'Call Codex and Grok with your own key. Use this site as the address, and use the model name from that key’s group.',
    tocLabel: 'Steps',
    copy: 'Copy',
    copied: 'Copied',
    keyToken: 'YOUR_KEY',
    grokModelToken: 'model-from-your-group',
    dashboardHint: 'No key yet? Read Get started',
    keysHint: 'First call',
    steps: {
      login: {
        title: 'Sign in',
        body: 'An invitation or an administrator creates the account. Open this site and sign in with your email.'
      },
      quota: {
        title: 'Check the weekly limits',
        body: 'The dashboard shows a used percent for Codex and another for Grok. Each percent is measured against that platform’s weekly limit. At 100% calls stop until the refresh time shown on the page. The two limits are separate.'
      },
      key: {
        title: 'Create a key',
        body: 'Open API Keys, create one, and copy it immediately. One key belongs to one group. Create a separate key when Codex and Grok are in different groups.',
        link: 'Open API Keys'
      },
      call: {
        title: 'Make the first call',
        body: 'The address is this site’s /v1. Copy either command below. Replace the key with the one you just copied. For Grok, replace the model with a name allowed by that key’s group.',
        client: 'Do not hand-edit the address for the Codex app or the Grok CLI. Open the key and choose Use. That screen already builds the client configuration.',
        codex: 'Codex',
        grok: 'Grok'
      },
      check: {
        title: 'Confirm the call',
        body: 'Usage shows the model, tokens, and the amount charged for that call. The percents that matter are the two weekly limits on the dashboard.',
        usage: 'Open usage',
        dashboard: 'Back to the dashboard'
      }
    },
    troubleshoot: {
      title: 'If a call fails, check these three things',
      quota: 'Whether the weekly limit is already used up',
      group: 'Whether the key belongs to this group',
      model: 'Whether the group allows that model name'
    },
    manual: {
      lead: 'Codex and Grok Build are both ready. Create a key, then install, write the config, and send one test.',
      windows: 'Windows',
      unix: 'macOS / Linux',
      openFolder: 'Open the config folder',
      missingFile: 'If the file is not there, create it and paste the block in.',
      keyHint: 'Replace sk-YOUR_KEY with the key you just copied. Do not send a real key in chat or a screenshot.',
      prep: {
        title: 'Prepare a key',
        login: 'Register with an email an administrator approved, set a password, and sign in.',
        quota: 'The dashboard has a separate weekly limit for Codex and for Grok. At 100% calls stop until the refresh time on the page.',
        key: 'Create one key for Codex and another for Grok Build. Choose that use when you create the key. Copy the key immediately. It is shown once.',
        link: 'Open API Keys'
      },
      codex: {
        title: 'Codex',
        ready: 'The service is ready.',
        install: 'Install Node.js, then install Codex globally. Run it once. When the sign-in choices appear, press Ctrl+C. That run only creates the config folder.',
        version: 'A printed version means the install worked.',
        files: 'Two files matter. config.toml sets the gateway and model. auth.json holds the key.',
        sandbox: 'The Windows sandbox is elevated. If the computer blocks the administrator prompt, change that line to unelevated. macOS and Linux can omit the [windows] section.',
        test: 'Open a new terminal and run codex. A reply means it works.',
        configFile: 'config.toml',
        authFile: 'auth.json'
      },
      grok: {
        title: 'Grok Build',
        ready: 'The service is ready. The installer does not need Node.js.',
        install: 'Use the official install script. If the shell cannot find grok, close the terminal and open a new one.',
        version: 'A printed grok version means the install worked.',
        files: 'The config is config.toml. Create it if it is missing. If the installer already wrote [cli], add the model settings and do not duplicate installer.',
        effort: 'With supports_reasoning_effort set, use /effort in a session and /model grok-4.7 to switch models. Restart Grok Build after editing the file.',
        search: 'Leave supports_backend_search false. The gateway does not run server-side search. The client web_search tool still uses grok-4.7.',
        test: 'Run grok and send a sentence. Or use the command below to check the gateway and the key.',
        configFile: 'config.toml'
      },
      check: {
        title: 'Confirm the call',
        body: 'Usage shows the model and the charge. The percents that matter are the weekly limits on the dashboard.',
        usage: 'Open usage',
        dashboard: 'Back to the dashboard'
      }
    }
  },
  models: {
    title: 'Models',
    lead: 'Put the name in the model field. Prices are yuan per million tokens. Your own limit is on the dashboard.',
    search: 'Search model names',
    count: '{count} models',
    empty: 'No model with that name',
    copy: 'Copy',
    copied: 'Copied',
    familyLabel: 'Family',
    useLabel: 'Use',
    input: 'Input',
    output: 'Output',
    cache: 'Cache',
    unit: 'yuan / million tokens',
    footnote: 'Create the key in the matching group. Models marked peak or long context cost more, as the card says.',
    families: {
      all: 'All',
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
      all: 'All uses',
      chat: 'Chat',
      coding: 'Coding'
    },
    notes: {
      peak: 'Weekdays 9:00–12:00 and 14:00–18:00 cost double',
      long2: 'Above 200k tokens costs double',
      long3: 'Above 256k tokens costs triple',
      long5: 'Above 100k tokens costs five times',
      long55: 'Above 272k tokens: input 6, output 27, cache 0.6'
    }
  }
}
