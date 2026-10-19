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
    }
  }
}
