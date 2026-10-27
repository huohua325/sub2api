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
      keyHint: 'sk-YOUR_KEY in the samples is only a placeholder. Replace it with the whole key you just copied. Do not send a real key in chat or a screenshot.',
      fieldsTitle: 'What these settings do',
      prep: {
        title: 'Prepare a key',
        login: 'Register with an email an administrator approved, set a password, and sign in.',
        quota: 'Codex and Grok have separate personal weekly quotas. All Keys for one purpose share quota. A rounded 100% is not necessarily exhausted; check the status.',
        key: 'Choose a purpose and group, then create a Key. Creation opens the connection panel. Existing Keys can reopen it using Use Key.',
        link: 'Open API Keys'
      },
      codex: {
        title: 'Codex',
        ready: 'Choose a Key, save the configuration, then start the client locally. The example below uses a placeholder.',
        install: 'Node.js must already be installed. Windows and macOS both use the command below to install the latest Codex.',
        firstRun: 'After installing, run codex once. When the sign-in choices appear, press Ctrl+C. That run only creates the config folder. Do not sign in to OpenAI there.',
        version: 'Then run codex --version. A printed version means the command is available.',
        files: 'Edit only two files in that folder. config.toml sets the gateway and model. auth.json holds the key. Create either file if it is missing. AGENTS.md is a global prompt and can wait.',
        authNote: 'requires_openai_auth = true means the key is read from auth.json. It does not mean you sign in on the OpenAI website.',
        sandbox: 'The connection panel offers system selection and both existing authentication modes. Preserve your local sandbox and tool settings when merging configuration.',
        test: 'After both files are saved, open a new terminal and run codex. A reply means the key and the gateway both work.',
        configFile: 'config.toml',
        authFile: 'auth.json',
        fields: {
          model: 'Model ID sent to the gateway. Template defaults are not a guarantee of permission. Fetch the model catalog explicitly in the connection panel.',
          model_provider: 'Local provider name. It must match the corresponding model_providers table.',
          model_reasoning_effort: 'Default reasoning level. xhigh is more careful and slower.',
          model_verbosity: 'How detailed the reply is. high writes a fuller answer.',
          disable_response_storage: 'Do not store the conversation at OpenAI. Keep this true when traffic goes through this site.',
          web_search_request: 'Allow Codex to request web search.',
          base_url: 'This site’s API address. It ends in /v1.',
          wire_api: 'Protocol. Keep responses.',
          requires_openai_auth: 'When true, Codex reads OPENAI_API_KEY from auth.json. The value is still a key created on this site.',
          screen_reader_detection_done: 'Skip the first-run screen-reader prompt.'
        }
      },
      grok: {
        title: 'Grok Build',
        ready: 'Install Grok Build, fill in your API Key and save config.toml. The example uses a placeholder.',
        install: 'On Windows, run the left command in PowerShell. On macOS, Linux, and WSL, run the right command in a terminal.',
        version: 'Run grok --version. A grok version string means it worked. If the shell cannot find grok, close the terminal and open a new one.',
        files: 'The file is config.toml. The installer usually already wrote [cli], [marketplace], and [ui]. Keep those sections and add the model sections. Do not write the same section twice. If config.toml does not exist yet, create it from the whole sample below.',
        effort: 'Models and reasoning levels depend on your client and group permissions. Restart Grok Build after changing configuration.',
        search: 'The template disables server search. Client search uses the web_search model; available capabilities depend on group permissions.',
        inspect: 'If you are not sure the client read the file, run grok inspect and check that the config source is this config.toml.',
        test: 'Then run grok and send a sentence. Or use the command below, which only asks for ok, to check the gateway and the key.',
        configFile: 'config.toml',
        fields: {
          default: 'Model used for a new session.',
          web_search: 'Model used by the client search tool. Keep it the same as default.',
          models_base_url: 'This site’s API address. It ends in /v1.',
          model: 'Model ID actually sent to the API. It must match the name under [models].',
          name_description: 'Name and description shown in the model picker.',
          api_key: 'Replace the example placeholder with the API Key created on this site.',
          api_backend: 'Must be responses. If omitted, the client calls /v1/chat/completions instead.',
          context_window: 'Client compaction threshold. Actual context capacity depends on the model.',
          supports_reasoning_effort: 'Says this model can change reasoning level. Without this line, /effort does nothing for it.',
          supports_backend_search: 'Optional capability. Enable only when supported by your group.',
          installer: 'The official script writes internal. Later updates use grok update. Do not add a second installer line if one is already there.',
          auto_update: 'Allow the client to check for its own updates.',
          yolo: 'Ask for fewer confirmations before running commands. compact_mode = false keeps the normal interface.',
          marketplace: 'Official plugin source. Keep the section the installer wrote. Do not paste a second copy.'
        }
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
    input: 'Input',
    output: 'Output',
    cache: 'Cache',
    unit: 'yuan / million tokens',
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
    notes: {
      peak: 'Weekdays 9:00–12:00 and 14:00–18:00 cost double',
      long2: 'Above 200k tokens costs double',
      long3: 'Above 256k tokens costs triple',
      long5: 'Above 100k tokens costs five times',
      long55: 'Above 272k tokens: input 6, output 27, cache 0.6',
      long272: 'Above 272k tokens costs double',
      tier: 'Longer context uses the next official tier'
    }
  }
}
