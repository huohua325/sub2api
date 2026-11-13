async page => {
  const options = __MILE_REVIEW_OPTIONS__, base = options.base, fixtures = (__MILE_FIXTURES__)(base)
  const errors = [], apiMutations = [], checks = [], requests = []
  const user = { ...fixtures.auth, balance: 0 }
  const group = { id: 20, name: 'MILE Unified Coding', platform: 'composite', usage_kind: 'coding', subscription_type: 'standard', rate_multiplier: 1 }
  const key = { id: 90, name: 'Unified coding', key: 'sk-synthetic-unified-review', group_id: group.id, group, status: 'active', quota: 0, quota_used: 0, created_at: '2026-11-01T00:00:00Z', ip_whitelist: [], ip_blacklist: [], rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0 }
  let created = 0, catalogs = 0
  page.setDefaultTimeout(15000)
  page.on('pageerror', error => errors.push(error.message))
  await page.context().addInitScript(user => {
    localStorage.setItem('auth_token', 'synthetic-review-only')
    localStorage.setItem('auth_user', JSON.stringify(user))
    localStorage.setItem('sub2api_locale', 'zh')
    localStorage.setItem('locale', 'zh')
    localStorage.setItem('theme', 'light')
    localStorage.setItem(`user_guide_${user.id}_user_v4_interactive`, 'true')
  }, user)
  await page.route('**/*', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname, method = req.method()
    if (url.origin !== base) { apiMutations.push({ path, method }); return route.abort() }
    if (path === '/setup/status') return route.fulfill({ json: { needs_setup: false } })
    if (path === '/v1/models') {
      catalogs++
      if (req.headers().authorization !== `Bearer ${key.key}`) throw Error('Catalog did not use the unified Key')
      return route.fulfill({ json: { models: [{ slug: 'gpt-6.1-sol' }, { slug: 'grok-4.6' }] } })
    }
    if (!path.startsWith('/api/v1/')) {
      if (method !== 'GET') { apiMutations.push({ path, method }); return route.abort() }
      return route.continue()
    }
    requests.push({ path, method })
    let data = {}
    if (path.endsWith('/auth/me') || path.endsWith('/user/profile')) data = user
    else if (path.endsWith('/settings/public')) data = { site_name: 'MILE Lab', run_mode: 'standard', api_base_url: base + '/v1', hide_ccs_import_button: true, custom_menu_items: [] }
    else if (path.endsWith('/groups/available')) data = [group]
    else if (path.endsWith('/groups/rates')) data = {}
    else if (path.endsWith('/keys') && method === 'POST') {
      const body = req.postDataJSON()
      if (body.group_id !== group.id) throw Error('Wrong unified group')
      created++
      data = { ...key, name: body.name, group: undefined }
    } else if (path.endsWith('/keys')) data = { items: [key], total: 1, page: 1, page_size: 20, pages: 1 }
    else if (path.endsWith('/user/platform-quotas')) data = { platform_quotas: [], purpose_allowances: [], server_now: new Date().toISOString() }
    else if (/announcements|subscriptions/.test(path)) data = []
    else if (path.endsWith('/usage/dashboard/api-keys-usage')) data = { stats: {} }
    else if (method !== 'GET') { apiMutations.push({ path, method }); return route.abort() }
    await route.fulfill({ json: { code: 0, data } })
  })
  const check = (name, value) => { checks.push({ name, pass: !!value }); if (!value) throw Error(name) }
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(base + '/keys?purpose=coding&create=1')
  await page.locator('[data-tour="key-form-name"]').fill('Shared CLI and IDE')
  check('unified-purpose-selected', await page.locator('input[name="key-provider"][value="coding"]').isChecked())
  await page.locator('#key-form-group').click()
  await page.getByRole('option').filter({ hasText: group.name }).click()
  await page.locator('[data-tour="key-form-submit"]').click()
  const setup = page.getByTestId('unified-coding-setup')
  await setup.waitFor()
  const dialog = page.getByRole('dialog', { name: /使用 API 密钥/ })
  check('one-key-created', created === 1)
  check('shared-cli-ide', (await setup.innerText()).includes('VS Code'))
  check('one-config-file', await dialog.locator('pre code').count() === 1)
  check('secret-hidden', !(await dialog.innerText()).includes(key.key))
  check('no-env-step', !(await dialog.locator('pre code').innerText()).includes('env_key'))
  check('no-websocket', (await dialog.locator('pre code').innerText()).includes('supports_websockets = false'))
  check('no-auto-fetch-or-inference', catalogs === 0)
  await page.getByTestId('codex-model-catalog-fetch').click()
  await page.getByTestId('codex-model-catalog').getByText(/2/).first().waitFor()
  check('authorized-combined-catalog', catalogs === 1)
  const downloadEvent = page.waitForEvent('download')
  await page.getByTestId('download-config-0').click()
  const download = await downloadEvent
  check('config-download', download.suggestedFilename() === 'config.toml')
  await download.saveAs(options.output + '/config.synthetic.toml')
  await page.getByRole('button', { name: 'Close notification' }).click()
  await page.getByRole('button', { name: 'Close notification' }).waitFor({ state: 'hidden' })
  for (const width of [375, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 })
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    await dialog.locator('.modal-body').evaluate(el => { el.scrollTop = 0 })
    await dialog.screenshot({ path: options.output + `/unified-${width}.png` })
    const overflow = await page.evaluate(() => ({ width: innerWidth, scroll: document.documentElement.scrollWidth, elements: [...document.querySelectorAll('body *')].filter(el => { const rect = el.getBoundingClientRect(); return rect.width && rect.right > innerWidth + 1 && getComputedStyle(el).position !== 'fixed' }).slice(0, 8).map(el => ({ tag: el.tagName, cls: el.className })) }))
    check(`no-page-overflow-${width}: ${JSON.stringify(overflow)}`, overflow.scroll <= width + 1)
  }
  check('no-real-mutations', apiMutations.length === 0)
  return { errors, apiMutations, checks, requests }
}
