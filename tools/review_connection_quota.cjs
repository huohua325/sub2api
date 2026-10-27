// Run with playwright-cli run-code --filename=tools/review_connection_quota.cjs.
// All business APIs are mocked. Never sends a model request or uses a real Key.
async (page) => {
  const browser = page.context().browser()
  for (const other of browser.contexts()) if (other !== page.context()) await other.close()
  const engine = browser.browserType().name()
  const result = { engine, checks: [], errors: [], measurements: [] }
  // This Windows Firefox driver also hangs resizing about:blank. Keep its
  // initial viewport for functional review; responsive coverage uses the other engines.
  result.viewportMode = engine === 'firefox' ? 'fixed-driver-viewport' : 'responsive'
  const context = engine === 'firefox' ? page.context() : await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN' })
  const user = { id: 1, username: 'MILE Student', email: 'review@example.test', role: 'user', balance: 0, status: 'active', concurrency: 2, created_at: '2026-10-01T00:00:00Z' }
  const groups = [
    { id: 1, name: 'Codex / Research', platform: 'openai', usage_kind: 'coding', subscription_type: 'standard', rate_multiplier: 1 },
    { id: 2, name: 'Grok Build / Research', platform: 'grok', usage_kind: 'coding', subscription_type: 'standard', rate_multiplier: 1 },
    { id: 3, name: 'API / Messages', platform: 'anthropic', usage_kind: 'experiment', subscription_type: 'standard', rate_multiplier: 1 },
  ]
  const key = (id, group, name = `Research ${id}`) => ({ id, name, key: `sk-synthetic-review-${id}`, group_id: group.id, group, status: 'active', quota: 0, quota_used: 0, current_concurrency: 0, expires_at: null, last_used_at: null, created_at: '2026-10-01T00:00:00Z', ip_whitelist: [], ip_blacklist: [], rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0, usage_5h: 0, usage_1d: 0, usage_7d: 0 })
  const keys = Array.from({ length: 12 }, (_, i) => key(i + 1, groups[i % 3]))
  const requests = []
  let failList = false
  let quotaMode = 'normal'
  let creates = 0
  await context.addInitScript((user) => {
    localStorage.setItem('auth_token', 'synthetic-review-only')
    localStorage.setItem('auth_user', JSON.stringify(user))
    localStorage.setItem('locale', 'zh')
    localStorage.setItem('sub2api_locale', 'zh')
    localStorage.setItem('theme', 'light')
    localStorage.setItem('user_guide_1_user_v4_interactive', 'true')
    window.__review = { lcp: null, cls: 0 }
    try { new PerformanceObserver((list) => { window.__review.lcp = list.getEntries().at(-1).startTime }).observe({ type: 'largest-contentful-paint', buffered: true }) } catch {}
    try { new PerformanceObserver((list) => { for (const e of list.getEntries()) if (!e.hadRecentInput) window.__review.cls += e.value }).observe({ type: 'layout-shift', buffered: true }) } catch {}
  }, user)
  await context.route('**/setup/status', (route) => route.fulfill({ json: { needs_setup: false } }))
  await context.route('**/api/v1/**', async (route) => {
    const p = new URL(route.request().url()).pathname.replace('/api/v1', '')
    requests.push({ path: p, method: route.request().method() })
    let data = {}
    if (p === '/auth/me' || p === '/user/profile') data = user
    else if (p === '/settings/public') data = { site_name: 'MILE Lab', run_mode: 'standard', registration_enabled: true, api_base_url: 'https://gateway.example.test/proxy/v1', custom_menu_items: [], hide_ccs_import_button: true }
    else if (p === '/groups/available') data = groups
    else if (p === '/groups/rates') data = {}
    else if (p === '/keys' && route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      creates += 1
      // Exercise the response without a nested group; the submitted snapshot must survive.
      data = { ...key(100 + creates, groups.find((group) => group.id === body.group_id), body.name), group: undefined }
    } else if (p === '/keys') {
      if (failList) return route.fulfill({ status: 503, json: { code: 503, message: 'Synthetic list refresh failure' } })
      data = { items: keys, total: keys.length, page: 1, page_size: 20, pages: 1 }
    } else if (p === '/user/platform-quotas') {
      if (quotaMode === 'failed') return route.fulfill({ status: 503, json: { code: 503, message: 'Synthetic quota failure' } })
      data = { platform_quotas: [], server_now: new Date().toISOString(), purpose_allowances: [
        { purpose: 'openai', display_percent: quotaMode === 'unknown' ? null : 9, point_used: '4.4444', point_limit: '50', blocked: false, data_status: quotaMode === 'unknown' ? 'error' : 'pending', period_start: '2026-10-21T08:00:00Z', resets_at: '2026-10-28T08:00:00Z', reset_scope: 'personal', updated_at: '2026-10-27T08:00:00Z' },
        { purpose: 'grok', display_percent: 100, point_used: '30.1234', point_limit: '30', blocked: true, data_status: 'partial', period_start: '2026-10-22T08:00:00Z', resets_at: '2026-10-29T08:00:00Z', reset_scope: 'personal' },
        { purpose: 'experiment', display_percent: 1, used_amount: 2.94, limit_amount: 500, blocked: false, data_status: 'current', reset_scope: 'month', period_start: '2026-10-01T00:00:00+08:00', resets_at: '2026-11-01T00:00:00+08:00' },
      ] }
    } else if (p === '/announcements' || /subscriptions|batch-image/.test(p)) data = []
    else if (p === '/usage/dashboard/api-keys-usage') data = { stats: {} }
    else if (/logs|usage/.test(p)) data = { items: [], total: 0, page: 1, page_size: 20 }
    await route.fulfill({ json: { code: 0, data } })
  })
  // A model catalog is only requested when a user clicks its explicit button.
  await context.route('**/proxy/v1/models*', (route) => route.fulfill({ json: { models: [{ slug: 'gpt-5.5' }] } }))
  const p = engine === 'firefox' ? page : await context.newPage()
  async function resize(width) {
    if (engine !== 'firefox') await p.setViewportSize({ width, height: 900 })
  }
  await resize(1440)
  p.setDefaultTimeout(15000)
  p.setDefaultNavigationTimeout(15000)
  p.on('pageerror', (error) => result.errors.push(error.message))
  const base = new URL(page.url()).origin
  async function settle(path) {
    await p.goto(base + path)
    await p.locator('#app main').first().waitFor()
    await p.waitForTimeout(500)
  }
  function check(name, pass, detail) { result.checks.push({ name, pass, detail }) }
  const capturedShots = new Set()
  async function shot(name, width) {
    if (engine === 'firefox' && name.includes('equivalent-200pct')) return
    await resize(width)
    await p.waitForTimeout(300)
    const actualWidth = await p.evaluate(() => innerWidth)
    const shotName = `${name}-${actualWidth}`
    if (capturedShots.has(shotName)) return
    capturedShots.add(shotName)
    check(`${shotName}-overflow`, await p.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), { requestedWidth: width, actualWidth })
    await p.screenshot({ path: `output/playwright/connection-quota/${engine}-${name}-${actualWidth}.png`, fullPage: engine !== 'firefox', animations: 'disabled', timeout: 15000 })
  }

  await settle('/keys')
  check('panel-lazy', !(await p.evaluate(() => performance.getEntriesByType('resource').some((e) => e.name.includes('UseKeyModal')))))
  for (const width of [1440, 1024, 768, 375]) await shot('keys', width)
  await shot('keys-equivalent-200pct', 720)
  await resize(1440)
  await p.waitForTimeout(300)
  const trigger = p.getByRole('button', { name: '使用密钥', exact: true }).first()
  await trigger.click()
  const dialog = p.getByRole('dialog').filter({ has: p.getByTestId('connection-key') })
  await dialog.waitFor()
  check('masked-key', !(await dialog.innerText()).includes('sk-synthetic-review-1'))
  for (const width of [1440, 768, 375]) await shot('connection', width)
  await shot('connection-equivalent-200pct', 720)
  await resize(1440)
  await p.getByTestId('reveal-connection-key').click()
  check('explicit-reveal', (await dialog.innerText()).includes('sk-synthetic-review-1'))
  await p.getByTestId('reveal-connection-key').click()
  const downloadEvent = p.waitForEvent('download')
  await p.getByTestId('download-config-1').click()
  const download = await downloadEvent
  check('auth-download', download.suggestedFilename() === 'auth.json')
  await dialog.getByRole('button', { name: '关闭', exact: true }).focus()
  await p.keyboard.press('Tab')
  check('focus-trapped', await dialog.evaluate((el) => el.contains(document.activeElement)))
  await p.keyboard.press('Escape')
  await dialog.waitFor({ state: 'hidden' })
  check('focus-returned', await trigger.evaluate((el) => el === document.activeElement))
  // Warm interaction timing includes Playwright click/locator overhead, not field INP.
  const timings = []
  for (let i = 0; i < 5; i += 1) {
    const started = Date.now()
    await trigger.click()
    await dialog.waitFor()
    timings.push(Date.now() - started)
    await p.keyboard.press('Escape')
    await dialog.waitFor({ state: 'hidden' })
  }
  result.measurements.push({ name: 'warm-dialog-automation-ms', samples: timings })

  for (const [purpose, groupName, expected] of [['codex', 'Codex / Research', 'auth.json'], ['grok_build', 'Grok Build / Research', 'XAI_API_KEY'], ['experiment', 'API / Messages', '/messages']]) {
    failList = false
    await settle(`/keys?purpose=${purpose}&create=1`)
    await p.locator('[data-tour="key-form-name"]').fill(`New ${purpose}`)
    await p.locator('#key-form-group').click()
    await p.getByRole('option').filter({ hasText: groupName }).click()
    failList = true
    const before = creates
    await p.locator('[data-tour="key-form-submit"]').click()
    await dialog.waitFor()
    await p.getByTestId('connection-key').waitFor()
    check(`created-${purpose}`, creates === before + 1 && (await dialog.innerText()).includes(expected))
    check(`created-${purpose}-masked`, !(await dialog.innerText()).includes(`sk-synthetic-review-${100 + creates}`))
    await p.keyboard.press('Escape')
    await dialog.waitFor({ state: 'hidden' })
    check(`created-${purpose}-focus`, await p.locator('[data-tour="keys-create-btn"]').evaluate((el) => el === document.activeElement))
  }
  failList = false
  await settle('/guide#guide-grok')
  check('legacy-grok-anchor', await p.locator('#guide-grok').isVisible())
  await p.getByRole('button', { name: 'API 实验', exact: true }).click()
  check('purpose-guide', await p.locator('#guide-experiment').isVisible())
  await shot('guide', 375)

  const requestsBefore = requests.length
  await settle('/dashboard')
  await p.getByTestId('quota-ruler').getByText('已用', { exact: true }).first().waitFor()
  const quotaRequests = requests.slice(requestsBefore)
  check('one-quota-request', quotaRequests.filter((r) => r.path === '/user/platform-quotas').length === 1)
  check('history-lazy', !quotaRequests.some((r) => /trend|stats|models|logs/.test(r.path)))
  const quota = p.getByTestId('quota-ruler')
  check('no-coding-points', !/4\.4444|30\.1234|可以调用/.test(await quota.innerText()))
  check('yuan-kept', (await quota.innerText()).includes('2.94 / 500'))
  for (const width of [1440, 1024, 768, 375]) await shot('quota', width)
  await p.getByTestId('openai-purpose-card').locator('summary').click()
  check('exact-beijing-reset', (await p.getByTestId('openai-purpose-card').innerText()).includes('2026/10/28 16:00:00 UTC+8'))
  await shot('quota-details', 375)
  quotaMode = 'failed'
  await quota.getByRole('button', { name: '重新读取' }).click()
  await quota.locator('.quota-board-stale').waitFor()
  check('failed-refresh-keeps-reading', (await p.getByTestId('openai-purpose-card').innerText()).includes('9%'))
  quotaMode = 'unknown'
  await quota.getByRole('button', { name: '重新读取' }).click()
  await p.getByTestId('openai-purpose-card').getByText('额度暂不可确认，请稍后刷新。').first().waitFor()
  check('unknown-not-zero', await p.getByTestId('openai-purpose-card').locator('.quota-meter-percent').count() === 0)
  check('no-model-calls', !requests.some((r) => /responses|chat\/completions|messages|generateContent/.test(r.path)))
  result.measurements.push(await p.evaluate(() => window.__review))
  if (context !== page.context()) await context.close()
  return result
}
