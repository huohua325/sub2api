async page => {
  const options = __MILE_REVIEW_OPTIONS__, base = options.base, fixtures = (__MILE_FIXTURES__)(base)
  const user = { ...fixtures.auth, balance: 0 }, errors = [], apiMutations = [], requests = [], checks = []
  let state = 'accounting-pending', reads = 0
  const check = (name, value) => { checks.push({ name, pass: !!value }); if (!value) throw Error(name) }
  const allowances = () => {
    const now = Date.now(), time = new Date(now).toISOString(), end = new Date(now + 7 * 86400000).toISOString()
    const common = { server_now: time, period_start: new Date(now - 86400000).toISOString(), resets_at: end, updated_at: time }
    const experiment = { ...common, purpose: 'experiment', unit: 'yuan', limit_amount: 500,
      used_amount: state === 'settled' ? 224.00884 : state === 'exhausted' ? 500 : 150.45967,
      display_percent: state === 'settled' ? 45 : state === 'exhausted' ? 100 : 30,
      reset_scope: 'month', data_status: state.includes('pending') ? 'pending' : 'current',
      blocked: state === 'accounting-pending' || state === 'exhausted',
      ...(state === 'accounting-pending' ? { blocked_reason: 'EXPERIMENT_ACCOUNTING_PENDING', pending_requests: 12909 } : {}) }
    return { server_now: time, platform_quotas: [], purpose_allowances: [
      { ...common, purpose: 'openai', unit: 'points', point_used: '15', point_limit: '50', display_percent: 30,
        data_status: 'current', activity_status: 'ready', blocked: false, reset_scope: 'personal' },
      { ...common, purpose: 'grok', unit: 'points', point_used: '0.9', point_limit: '30', display_percent: 3,
        data_status: 'current', activity_status: 'ready', blocked: false, reset_scope: 'personal' }, experiment] }
  }
  await page.context().addInitScript(user => {
    localStorage.setItem('auth_token', 'synthetic-experiment-review-only')
    localStorage.setItem('auth_user', JSON.stringify(user))
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
    if (path.endsWith('/user/platform-quotas') && method === 'GET') { reads++; data = allowances() }
    else {
      try { data = fixtures.resolve(path, method) }
      catch { apiMutations.push({ path, method }); return route.abort() }
    }
    return route.fulfill({ json: { code: 0, message: 'ok', data } })
  })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(base + '/dashboard', { waitUntil: 'domcontentloaded', timeout: 45000 })
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
  check('only-mocked-quota-reads', reads >= 4)
  check('no-real-mutations-or-inference', apiMutations.length === 0)
  return { errors, apiMutations, checks, requests, quotaReads: reads }
}
