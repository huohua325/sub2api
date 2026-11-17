async page => {
  const options = __MILE_REVIEW_OPTIONS__, base = options.base, fixtures = (__MILE_FIXTURES__)(base)
  const auth = { ...fixtures.auth, id: 2, role: 'admin' }
  let group = { id: 7, name: 'Upstream group review', platform: 'openai', usage_kind: 'coding', status: 'active', subscription_type: 'standard', is_exclusive: true, rate_multiplier: 1, sort_order: 0, force_openai_fast: false, force_openai_ultrafast: false, free_openai_fast: false, model_pricing: [], created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z' }
  const errors = [], apiMutations = [], checks = [], saved = []
  const check = (name, passed) => { checks.push({ name, pass: !!passed }); if (!passed) throw Error(name) }
  page.setDefaultTimeout(15000)
  page.on('pageerror', e => errors.push(e.message))
  await page.context().addInitScript(auth => {
    localStorage.setItem('auth_token', 'synthetic-admin-review')
    localStorage.setItem('auth_user', JSON.stringify(auth))
    localStorage.setItem('sub2api_locale', 'zh')
    localStorage.setItem('theme', 'light')
    for (const key of ['onboarding_tour', 'admin_guide', 'user_guide']) for (const id of ['2_admin', 'guest_admin']) localStorage.setItem(key + '_' + id + '_v4_interactive', 'true')
  }, auth)
  await page.route('**/*', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname, method = req.method()
    if (url.origin !== base) { apiMutations.push({ path, method }); return route.abort() }
    if (path === '/setup/status') return route.fulfill({ json: { needs_setup: false } })
    if (!path.startsWith('/api/v1/')) {
      if (!['GET', 'HEAD'].includes(method)) { apiMutations.push({ path, method }); return route.abort() }
      return route.continue()
    }
    let data = {}
    if (path.endsWith('/auth/me') || path.endsWith('/user/profile')) data = auth
    else if (path === '/api/v1/admin/groups' && method === 'GET') data = { items: [group], total: 1, page: 1, page_size: 20, pages: 1 }
    else if (path === '/api/v1/admin/groups/7' && method === 'PUT') {
      const body = req.postDataJSON()
      saved.push(body)
      group = { ...group, ...body }
      data = group
    } else if (path.endsWith('/groups/all')) data = [group]
    else if (path.includes('usage-summary') || path.includes('capacity-summary')) data = []
    else if (path.includes('accounts') || path.includes('model-allowlist-candidates')) data = { items: [], total: 0, pages: 0 }
    else if (method !== 'GET') { apiMutations.push({ path, method }); return route.abort() }
    else data = fixtures.resolve(path, method)
    await route.fulfill({ json: { code: 0, data } })
  })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(base + '/admin/groups')
  await page.getByRole('button', { name: '编辑', exact: true }).first().click()
  const fast = page.getByTestId('edit-force-openai-fast'), ultra = page.getByTestId('edit-force-openai-ultrafast')
  await ultra.waitFor()
  check('existing-group-fast-default-off', await fast.getAttribute('aria-checked') === 'false')
  check('existing-group-ultrafast-default-off', await ultra.getAttribute('aria-checked') === 'false')
  await fast.click()
  await ultra.click()
  check('independent-fast-and-ultrafast', await fast.getAttribute('aria-checked') === 'true' && await ultra.getAttribute('aria-checked') === 'true')
  await page.getByRole('dialog').getByRole('button', { name: '更新', exact: true }).click()
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  check('explicit-save-both-tiers', saved.length === 1 && saved[0].force_openai_fast && saved[0].force_openai_ultrafast)
  check('exclusive-and-purpose-preserved', group.is_exclusive === true && group.usage_kind === 'coding')
  await page.getByRole('button', { name: '编辑', exact: true }).first().click()
  check('reopen-preserves-explicit-tier', await ultra.getAttribute('aria-checked') === 'true')
  for (const width of [375, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 })
    await ultra.scrollIntoViewIfNeeded()
    check('no-page-overflow-' + width, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    await page.getByRole('dialog').screenshot({ path: options.output + '/group-tier-' + width + '.png' })
  }
  await page.keyboard.press('Escape')
  check('cancel-does-not-save', saved.length === 1)
  return { checks, errors, apiMutations, mockedSaves: saved.length }
}
