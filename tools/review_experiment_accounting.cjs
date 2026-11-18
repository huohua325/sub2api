async page => {
  const options = __MILE_REVIEW_OPTIONS__, base = options.base, fixtures = (__MILE_FIXTURES__)(base)
  const user = { ...fixtures.auth, balance: 0 }, errors = [], apiMutations = [], requests = [], checks = []
  let state = 'accounting-pending', reads = 0, adminMode = false
  let concurrency = { enabled: false, max_active: 1 }
  const concurrencyWrites = []
  const check = (name, value) => { checks.push({ name, pass: !!value }); if (!value) throw Error(name) }
  const allowances = () => {
    const now = Date.now(), time = new Date(now).toISOString(), end = new Date(now + 7 * 86400000).toISOString()
    const common = { server_now: time, period_start: new Date(now - 86400000).toISOString(), resets_at: end, updated_at: time }
    const experiment = { ...common, purpose: 'experiment', unit: 'yuan', limit_amount: 500,
      used_amount: state === 'settled' ? 224.00884 : state === 'exhausted' ? 500 : 150.45967,
      display_percent: state === 'settled' ? 45 : state === 'exhausted' ? 100 : 30,
      reset_scope: 'month', data_status: state.includes('pending') ? 'pending' : 'current',
      blocked: state === 'accounting-pending' || state === 'exhausted',
      ...(state === 'accounting-pending' ? { blocked_reason: 'EXPERIMENT_ACCOUNTING_PENDING', pending_requests: 12909 } : {}),
      ...(state === 'async-pending' ? { pending_requests: 12, in_flight_requests: 10, active_limit: 10 } : {}) }
    return { server_now: time, platform_quotas: [], purpose_allowances: [
      { ...common, purpose: 'openai', unit: 'points', point_used: '15', point_limit: '50', display_percent: 30,
        data_status: 'current', activity_status: 'ready', blocked: false, reset_scope: 'personal' },
      { ...common, purpose: 'grok', unit: 'points', point_used: '0.9', point_limit: '30', display_percent: 3,
        data_status: 'current', activity_status: 'ready', blocked: false, reset_scope: 'personal' }, experiment] }
  }
  await page.context().addInitScript(user => {
    const admin = sessionStorage.getItem('mile-experiment-review-admin') === 'true'
    localStorage.setItem('auth_token', 'synthetic-experiment-review-only')
    localStorage.setItem('auth_user', JSON.stringify(admin ? { ...user, id: 2, role: 'admin' } : user))
    localStorage.setItem('sub2api_locale', 'zh')
    localStorage.setItem('locale', 'zh')
    localStorage.setItem('theme', 'light')
    localStorage.setItem(`user_guide_${user.id}_user_v4_interactive`, 'true')
  }, user)
  page.setDefaultTimeout(15000)
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/*', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname, method = req.method()
    if (url.origin !== base || path.startsWith('/v1/')) { apiMutations.push({ path, method }); return route.abort() }
    if (path === '/setup/status') return route.fulfill({ json: { needs_setup: false } })
    if (!path.startsWith('/api/v1/')) {
      if (!['GET', 'HEAD'].includes(method)) { apiMutations.push({ path, method }); return route.abort() }
      return route.continue()
    }
    requests.push({ path, method })
    let data
    if (adminMode && (path.endsWith('/auth/me') || path.endsWith('/user/profile'))) data = { ...user, id: 2, role: 'admin' }
    else if (adminMode && path === '/api/v1/admin/users' && method === 'GET') data = { items: [{ ...user, id: 99, concurrency: 500, subscriptions: [] }], total: 1, page: 1, page_size: 20, pages: 1 }
    else if (adminMode && path === '/api/v1/admin/users/99/experiment-concurrency') {
      if (method === 'PUT') { concurrency = req.postDataJSON(); concurrencyWrites.push(concurrency) }
      else if (method !== 'GET') { apiMutations.push({ path, method }); return route.abort() }
      data = concurrency
    }
    else if (adminMode && path === '/api/v1/admin/dashboard/users-usage' && method === 'POST') data = {}
    else if (adminMode && method === 'GET' && (path.includes('/groups') || path.includes('/user-attributes') || path.endsWith('/subscriptions'))) data = []
    else if (adminMode && method === 'GET' && path.endsWith('/users/99/attributes')) data = {}
    else if (path.endsWith('/user/platform-quotas') && method === 'GET') { reads++; data = allowances() }
    else {
      try { data = fixtures.resolve(path, method) }
      catch { apiMutations.push({ path, method }); return route.abort() }
    }
    return route.fulfill({ json: { code: 0, message: 'ok', data } })
  })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(base + '/dashboard', { waitUntil: 'domcontentloaded', timeout: 90000 })
  const experiment = page.getByTestId('experiment-purpose-card'), codex = page.getByTestId('openai-purpose-card'), grok = page.getByTestId('grok-purpose-card')
  await experiment.getByText('对账中', { exact: true }).waitFor()
  let text = await experiment.innerText()
  check('settled-amount-retained', text.includes('150.46') && text.includes('500'))
  check('settled-amount-labelled', text.includes('已结算'))
  check('unknown-request-count-visible', /12[,\s]?909/.test(text))
  check('new-experiments-paused', text.includes('暂停') && text.includes('实验'))
  check('accounting-pause-is-not-quota-exhaustion', !text.includes('已到上限') && !text.includes('有剩余额度'))
  check('codex-still-ready', (await codex.locator('.quota-badge').innerText()).includes('有剩余额度'))
  check('grok-still-ready', (await grok.locator('.quota-badge').innerText()).includes('有剩余额度'))
  await page.screenshot({ path: options.output + '/accounting-pending-desktop.png', fullPage: true })
  for (const width of [375, 768]) {
    await page.setViewportSize({ width, height: 1000 })
    await experiment.scrollIntoViewIfNeeded()
    await experiment.screenshot({ path: options.output + `/accounting-pending-${width}.png` })
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)
    check(`accounting-card-no-overflow-${width}`, !overflow)
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
  const reload = page.getByRole('button', { name: '重新读取', exact: true })
  state = 'async-pending'
  await reload.click()
  await experiment.getByText('正在运行 10 / 10 条。', { exact: true }).waitFor()
  text = await experiment.innerText()
  check('execution-and-backlog-separated', text.includes('12 条请求的费用待核实') && text.includes('10 / 10'))
  check('background-billing-allows-more-requests', text.includes('可继续请求') && !text.includes('暂停') && !text.includes('已到上限'))
  check('pending-money-not-counted-as-free', text.includes('待结算费用尚未计入') && text.includes('150.46'))
  await experiment.screenshot({ path: options.output + '/async-billing.png' })
  state = 'settled'
  await reload.click()
  await experiment.getByText(/224\.01/).waitFor()
  text = await experiment.innerText()
  check('reconciled-amount-displayed', text.includes('224.01'))
  check('reconciled-requests-ready', (await experiment.locator('.quota-badge').innerText()).includes('有剩余额度'))
  check('old-pending-hint-clears', !/12[,\s]?909/.test(text) && !text.includes('暂停'))
  state = 'legacy-pending'
  await reload.click()
  await experiment.getByText('待确认', { exact: true }).waitFor()
  text = await experiment.innerText()
  check('legacy-pending-without-reason-keeps-semantics', !text.includes('对账中') && !text.includes('暂停') && !text.includes('已到上限'))
  state = 'exhausted'
  await reload.click()
  await experiment.getByText('已到上限', { exact: true }).waitFor()
  check('real-quota-exhaustion-still-distinct', (await experiment.innerText()).includes('100'))
  await experiment.screenshot({ path: options.output + '/real-quota-exhaustion.png' })
  adminMode = true
  await page.evaluate(user => {
    sessionStorage.setItem('mile-experiment-review-admin', 'true')
    localStorage.setItem('auth_user', JSON.stringify({ ...user, id: 2, role: 'admin' }))
    for (const key of ['onboarding_tour', 'admin_guide', 'user_guide']) {
      localStorage.setItem(key + '_2_admin_v4_interactive', 'true')
    }
  }, user)
  await page.goto(base + '/admin/users', { waitUntil: 'domcontentloaded' })
  const openEdit = async () => {
    await page.getByRole('button', { name: '编辑', exact: true }).first().click()
    await page.getByTestId('experiment-concurrency-control').waitFor()
    await page.getByTestId('experiment-async-enabled').waitFor({ state: 'visible' })
  }
  await openEdit()
  const control = page.getByTestId('experiment-concurrency-control')
  check('viewing-user-does-not-grant-access', concurrencyWrites.length === 0)
  await page.getByTestId('experiment-async-enabled').check()
  await page.getByTestId('experiment-concurrency-input').fill('37')
  await control.getByRole('button', { name: '保存', exact: true }).click()
  await page.getByText('实验并发已保存', { exact: true }).waitFor()
  check('administrator-selects-non-ten-limit', concurrency.enabled && concurrency.max_active === 37)
  await page.getByRole('button', { name: '取消', exact: true }).last().click()
  await openEdit()
  await page.waitForFunction(() => document.querySelector('[data-testid="experiment-concurrency-input"]')?.value === '37')
  check('saved-user-allocation-reloads', await page.getByTestId('experiment-async-enabled').isChecked())
  for (const width of [375, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 })
    await control.scrollIntoViewIfNeeded()
    await control.screenshot({ path: options.output + `/admin-concurrency-${width}.png` })
    check(`admin-concurrency-no-overflow-${width}`, !await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1))
  }
  await page.getByTestId('experiment-async-enabled').uncheck()
  await Promise.all([
    page.waitForResponse(response => response.url().endsWith('/users/99/experiment-concurrency') && response.request().method() === 'PUT'),
    control.getByRole('button', { name: '保存', exact: true }).click(),
  ])
  check('administrator-can-revoke-trial', concurrencyWrites.length === 2 && !concurrency.enabled)
  check('only-mocked-quota-reads', reads >= 4)
  check('no-real-mutations-or-inference', apiMutations.length === 0)
  return { errors, apiMutations, checks, requests, quotaReads: reads, mockedConcurrencyWrites: concurrencyWrites }
}
