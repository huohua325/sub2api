async page => {
  const options = __MILE_REVIEW_OPTIONS__, base = options.base, fixtures = (__MILE_FIXTURES__)(base)
  let auth = { ...fixtures.auth, id: 2, role: 'admin' }, primaryUnavailable = false, failSave = true
  const errors = [], apiMutations = [], requests = [], mockedWrites = [], checks = []
  const group = { id: 17, name: 'Unified admin review', platform: 'composite', usage_kind: 'coding', status: 'active', subscription_type: 'standard', rate_multiplier: 1 }
  const groups = [group,
    { ...group, id: 18, name: 'Codex admin review', platform: 'openai' },
    { ...group, id: 19, name: 'Grok Build admin review', platform: 'grok' },
    { ...group, id: 21, name: 'Experiment must be excluded', platform: 'openai', usage_kind: 'experiment' },
    { ...group, id: 22, name: 'Inactive must be excluded', platform: 'grok', status: 'inactive' },
    { ...group, id: 23, name: 'Other platform must be excluded', platform: 'anthropic' }
  ]
  const accountsByGroup = {
    17: [{ id: 10, name: 'Review subscription', type: 'oauth', platform: 'openai' }, { id: 20, name: 'Review API', type: 'apikey', platform: 'openai' }],
    18: [{ id: 11, name: 'Codex subscription', type: 'oauth', platform: 'openai' }, { id: 21, name: 'Codex API', type: 'apikey', platform: 'openai' }],
    19: [{ id: 30, name: 'Grok Build subscription', type: 'oauth', platform: 'grok' }, { id: 40, name: 'Grok Build API', type: 'apikey', platform: 'grok' }]
  }
  let experimentAccount = {
    id: 51, name: 'GPT experiment supplier', type: 'apikey', platform: 'openai', status: 'active',
    schedulable: true, group_ids: [21], groups: [groups[3]], priority: 1, concurrency: 2, rate_multiplier: 1,
    credentials_status: { has_api_key: true },
    credentials: { base_url: 'https://example.invalid', model_mapping: { 'gpt-6.1-sol': 'gpt-6.1-sol' } },
    extra: { openai_responses_mode: 'force_responses' }
  }
  const policies = [{
    id: 'review-policy', version: 3, group_id: 17, platform: 'openai', model: 'gpt-6.1-sol',
    primary_account_ids: [10], backup_account_ids: [20], primary_evidence: [], enabled: true,
    capabilities_verified: true, expires_at: '2099-01-01T00:00:00Z', points_per_usd: '0.13', supplier_multiplier: '0.3',
    input_price_per_million: '2', cached_input_price_per_million: '0.2', output_price_per_million: '12',
    per_request_budget_usd: '0.1', daily_budget_usd: '1', total_budget_usd: '5', max_input_tokens: 500,
    max_output_tokens: 100, note: 'Synthetic price and capability verification'
  }]
  const attempts = [{
    id: 'review-unresolved', request_id: 'synthetic-upstream-request', policy_id: 'review-policy', policy_version: 3,
    user_id: 2, account_id: 20, model: 'gpt-6.1-sol', state: 'unresolved', reason: 'usage_missing',
    official_usd: '0', supplier_usd: '0', points: '0', reserved_usd: '0.1', reserved_points: '0.04333333',
    started_at: '2026-11-17T00:00:00Z', receipt: { response_id: 'synthetic-receipt-id', usage: { input_tokens: 100, output_tokens: 20 } },
    events: [{ type: 'unknown', actor_user_id: null, evidence: 'Synthetic missing final usage evidence', created_at: '2026-11-17T00:00:01Z' }]
  }]
  page.setDefaultTimeout(15000)
  page.on('pageerror', error => errors.push(error.message))
  const check = (name, value) => { checks.push({ name, pass: !!value }); if (!value) throw Error(name) }
  await page.context().addInitScript(admin => {
    const user = sessionStorage.getItem('mile-fallback-review-role') === 'user' ? { ...admin, id: 3, role: 'user' } : admin
    localStorage.setItem('auth_token', 'synthetic-review-only')
    localStorage.setItem('auth_user', JSON.stringify(user))
    localStorage.setItem('sub2api_locale', 'zh')
    localStorage.setItem('locale', 'zh')
    localStorage.setItem('theme', 'light')
    for (const key of ['onboarding_tour', 'admin_guide', 'user_guide']) {
      for (const identity of ['2_admin', '3_user', 'guest_admin']) localStorage.setItem(key + '_' + identity + '_v4_interactive', 'true')
    }
  }, auth)
  await page.route('**/*', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname, method = req.method()
    if (url.origin !== base) return route.abort()
    if (path === '/setup/status') return route.fulfill({ json: { needs_setup: false } })
    if (path.startsWith('/v1/')) { apiMutations.push({ path, method }); return route.abort() }
    if (!path.startsWith('/api/v1/')) {
      if (!['GET', 'HEAD'].includes(method)) { apiMutations.push({ path, method }); return route.abort() }
      return route.continue()
    }
    requests.push({ path, method, role: auth.role })
    let data = {}
    if (path.endsWith('/auth/me') || path.endsWith('/user/profile')) data = auth
    else if (path.endsWith('/settings/public')) data = fixtures.resolve(path, method)
    else if (path.endsWith('/admin/compliance/status')) data = { required: false }
    else if (path === '/api/v1/admin/api-fallback' && method === 'GET') data = {
      policies, attempts, experiment_keys: [
        { id: 71, name: 'GPT lab Key', group_name: 'GPT', platform: 'openai' },
        { id: 72, name: 'Grok lab Key', group_name: 'Grok', platform: 'grok' }
      ],
      summary: { settled_usd: '0', reserved_usd: '0', unresolved_usd: attempts[0].state === 'unresolved' ? '0.1' : '0', today_usd: '0.1', settled_count: 0, unresolved_count: attempts[0].state === 'unresolved' ? 1 : 0 }
    }
    else if (path === '/api/v1/admin/api-fallback/policies' && method === 'POST') {
      const body = req.postDataJSON(); mockedWrites.push({ path, body })
      if (!body.confirm) throw Error('Missing explicit policy confirmation')
      if (body.id === 'review-policy' && failSave) {
        failSave = false
        return route.fulfill({ status: 409, json: { code: 409, message: 'Synthetic version conflict' } })
      }
      const index = policies.findIndex(policy => policy.id === body.id)
      const selectedGroup = groups.find(group => group.id === body.group_id)
      if (!selectedGroup || selectedGroup.usage_kind !== 'coding' || selectedGroup.status !== 'active' || (selectedGroup.platform !== 'composite' && selectedGroup.platform !== body.platform)) throw Error('Invalid policy entrance')
      data = { ...body, id: body.id || (body.group_id === 17 ? 'review-draft' : 'review-draft-' + body.group_id), version: body.expected_version + 1 }
      if (index >= 0) policies[index] = data; else policies.push(data)
    }
    else if (path === '/api/v1/admin/api-fallback/policies/review-policy/disable' && method === 'POST') {
      const body = req.postDataJSON(); mockedWrites.push({ path, body })
      if (!body.confirm || body.expected_version !== policies[0].version) throw Error('Invalid disable version')
      policies[0] = { ...policies[0], enabled: false, version: policies[0].version + 1 }; data = policies[0]
    }
    else if (/\/api-fallback\/policies\/[^/]+\/preview$/.test(path) && method === 'POST') {
      const policy = policies.find(policy => policy.id === path.split('/').at(-2)), eligible = policy?.enabled && primaryUnavailable
      data = {
        eligible, reason: !policy?.enabled ? 'policy_disabled' : primaryUnavailable ? '主订阅额度已明确耗尽' : '订阅已恢复，继续使用订阅',
        checks: [{ name: '主池状态', passed: !!eligible, detail: !policy?.enabled ? '策略已关闭' : primaryUnavailable ? '所有主身份有有效失效证据' : '仍有健康订阅' }]
      }
    }
    else if (path === '/api/v1/admin/api-fallback/attempts/review-unresolved/resolve' && method === 'POST') {
      const body = req.postDataJSON(); mockedWrites.push({ path, body })
      if (!body.confirm || !body.evidence?.trim()) throw Error('Unverified unknown spending release')
      attempts[0] = { ...attempts[0], state: body.outcome === 'not_billed' ? 'cancelled' : 'settled' }; data = attempts[0]
    }
    else if (path === '/api/v1/admin/groups/all') data = groups
    else if (path === '/api/v1/admin/accounts' && method === 'GET') {
      const filtered = accountsByGroup[url.searchParams.get('group')]
      const items = filtered
        ? filtered.filter(account => account.platform === url.searchParams.get('platform'))
        : [experimentAccount]
      data = { items, total: items.length, page: 1, pages: 1, page_size: 100 }
    }
    else if (path === '/api/v1/admin/accounts/51' && method === 'GET') data = experimentAccount
    else if (path === '/api/v1/admin/accounts/check-mixed-channel' && method === 'POST') data = { has_risk: false }
    else if (path === '/api/v1/admin/accounts/today-stats/batch' && method === 'POST') data = { stats: {} }
    else if (path === '/api/v1/admin/accounts/51' && method === 'PUT') {
      const body = req.postDataJSON()
      mockedWrites.push({ path, body })
      data = experimentAccount = { ...experimentAccount, ...body, id: 51 }
    }
    else if (method === 'GET') data = fixtures.resolve(path, method)
    else if (path === '/api/v1/usage/dashboard/api-keys-usage' && method === 'POST') data = { stats: {} }
    else { apiMutations.push({ path, method }); return route.abort() }
    return route.fulfill({ json: { code: 0, message: 'ok', data } })
  })

  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(base + '/admin/api-fallback', { waitUntil: 'domcontentloaded', timeout: 45000 })
  await page.getByTestId('fallback-policy-review-policy').waitFor()
  check('admin-navigation-entry', await page.locator('nav a[href="/admin/api-fallback"]').count() > 0)
  const attemptDetails = page.getByTestId('fallback-attempt-review-unresolved').locator('details')
  await attemptDetails.locator('summary').click()
  check('upstream-receipt-and-evidence-visible', (await attemptDetails.innerText()).includes('synthetic-receipt-id') && (await attemptDetails.innerText()).includes('Synthetic missing final usage evidence'))
  await page.screenshot({ path: options.output + '/overview-desktop.png' })
  await page.getByTestId('fallback-preview-review-policy').click()
  await page.getByTestId('fallback-preview-result').waitFor()
  check('recovered-subscription-prevents-takeover', (await page.getByTestId('fallback-preview-result').innerText()).includes('订阅已恢复'))
  await page.getByRole('button', { name: 'Close modal', exact: true }).click()
  primaryUnavailable = true
  await page.getByTestId('fallback-preview-review-policy').click()
  await page.getByTestId('fallback-preview-result').waitFor()
  check('unavailable-subscriptions-preview', (await page.getByTestId('fallback-preview-result').innerText()).includes('满足接管条件'))
  await page.getByRole('button', { name: 'Close modal', exact: true }).click()

  await page.getByTestId('fallback-edit-review-policy').click()
  await page.getByTestId('fallback-primary-10').waitFor()
  await page.getByTestId('fallback-input_price_per_million').fill('-1')
  await page.getByTestId('fallback-approved').check()
  await page.getByTestId('fallback-save').click()
  await page.getByTestId('fallback-form-error').waitFor()
  check('invalid-price-does-not-write', mockedWrites.length === 0)
  await page.getByTestId('fallback-input_price_per_million').fill('2')
  await page.getByTestId('fallback-note').fill('Synthetic revised approval evidence')
  const evidencePanel = page.getByTestId('fallback-policy-form').locator('details').first()
  await evidencePanel.locator('summary').click()
  await evidencePanel.locator('select').selectOption('quota_exhausted')
  const times = await page.evaluate(() => {
    const local = value => new Date(value - new Date(value).getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
    return [local(Date.now() - 60_000), local(Date.now() + 30 * 60_000)]
  })
  await evidencePanel.locator('input[type="datetime-local"]').nth(0).fill(times[0])
  await evidencePanel.locator('input[type="datetime-local"]').nth(1).fill(times[1])
  await evidencePanel.locator('input:not([type="datetime-local"])').fill('Synthetic official exhaustion evidence')
  await page.getByTestId('fallback-save').click()
  await page.getByTestId('fallback-form-error').waitFor()
  check('failed-save-preserves-draft', await page.getByTestId('fallback-note').inputValue() === 'Synthetic revised approval evidence')
  await page.getByTestId('fallback-save').click()
  await page.getByTestId('fallback-policy-form').waitFor({ state: 'detached' })
  check('evidence-saved-with-policy', policies[0].primary_evidence.length === 1)
  await page.getByTestId('fallback-disable-review-policy').click()
  await page.getByTestId('fallback-disable-review-policy').waitFor({ state: 'detached' })
  check('one-click-disable', policies[0].enabled === false)

  check('no-new-per-model-policy', await page.getByTestId('fallback-new').count() === 0)
  check('configuration-in-accounts', await page.getByTestId('fallback-account-settings').getAttribute('href') === '/admin/accounts')

  await page.getByTestId('fallback-attempt-state').selectOption('unresolved')
  await page.getByTestId('fallback-resolve-review-unresolved').click()
  await page.getByTestId('fallback-resolution-outcome').selectOption('not_billed')
  const resolutionsBefore = mockedWrites.length
  await page.getByRole('dialog').getByRole('button', { name: '确认', exact: true }).click()
  await page.getByTestId('fallback-resolution-error').waitFor()
  check('unknown-cost-not-released-without-evidence', mockedWrites.length === resolutionsBefore)
  await page.getByTestId('fallback-resolution-evidence').fill('Synthetic supplier ledger: no charge for this request')
  await page.getByTestId('fallback-resolution-confirm').check()
  await page.getByRole('dialog').getByRole('button', { name: '确认', exact: true }).click()
  await page.getByTestId('fallback-resolution-form').waitFor({ state: 'detached' })
  check('unknown-cost-explicitly-resolved', attempts[0].state === 'cancelled')

  await page.getByTestId('fallback-account-settings').click()
  await page.waitForURL(base + '/admin/accounts')
  await page.getByText(experimentAccount.name, { exact: true }).waitFor()
  await page.getByTestId('account-edit-51').click()
  const standby = page.getByTestId('account-coding-standby')
  await standby.waitFor()
  check('account-standby-default-off', !await page.getByTestId('account-coding-standby-17').isChecked() && !await page.getByTestId('account-coding-standby-18').isChecked())
  check('no-grok-or-experiment-entry', await standby.locator('input').count() === 2)
  await page.getByTestId('account-coding-standby-17').check()
  await page.getByTestId('account-coding-standby-18').check()
  await standby.scrollIntoViewIfNeeded()
  await page.screenshot({ path: options.output + '/account-standby-desktop.png' })
  await page.locator('button[form="edit-account-form"]').click()
  try {
    await standby.waitFor({ state: 'detached' })
  } catch (error) {
    const formState = await page.locator('#edit-account-form').evaluate(form => [...form.querySelectorAll(':invalid')].map(field => [field.name, field.id, field.validationMessage]))
    throw Error(JSON.stringify({ formState, requests: requests.slice(-15), apiMutations, errors, message: String(error) }))
  }
  check('standby-settings-without-normal-binding', JSON.stringify(experimentAccount.group_ids) === '[21]' &&
    JSON.stringify(experimentAccount.extra.coding_standby_group_ids) === '[17,18]')
  await page.setViewportSize({ width: 375, height: 812 })
  await page.getByTestId('account-edit-51').click()
  await standby.waitFor()
  check('saved-standby-selection-visible', await page.getByTestId('account-coding-standby-17').isChecked() && await page.getByTestId('account-coding-standby-18').isChecked())
  await standby.scrollIntoViewIfNeeded()
  const box = await standby.boundingBox()
  check('mobile-standby-fits', box && box.x >= 0 && box.x + box.width <= 375)
  await page.screenshot({ path: options.output + '/account-standby-mobile.png' })

  auth = { ...fixtures.auth, id: 3, role: 'user' }
  await page.evaluate(() => { sessionStorage.setItem('mile-fallback-review-role', 'user'); localStorage.setItem('sidebar_collapsed', 'false') })
  const ordinaryStart = requests.length
  await page.goto(base + '/admin/api-fallback', { waitUntil: 'domcontentloaded', timeout: 45000 })
  await page.waitForURL(base + '/dashboard')
  check('ordinary-user-redirected', new URL(page.url()).pathname === '/dashboard')
  check('ordinary-user-no-backup-ui', await page.getByTestId('fallback-admin-page').count() === 0)
  check('ordinary-user-no-backup-nav', await page.locator('a[href="/admin/api-fallback"]').count() === 0)
  check('ordinary-user-no-admin-requests', !requests.slice(ordinaryStart).some(request => request.path.includes('/api-fallback')))
  await page.screenshot({ path: options.output + '/ordinary-user-mobile.png' })
  check('no-page-errors', errors.length === 0)
  return { errors, apiMutations, checks, mockedWrites: mockedWrites.length, requests }
}
