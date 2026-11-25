async page => {
  const options = __MILE_REVIEW_OPTIONS__
  const base = options.base
  const fixtures = (__MILE_FIXTURES__)(base)
  const admin = { ...fixtures.auth, role: 'admin' }
  const workspace = '/admin/development-workspace'
  const ticketPath = '/api/v1' + workspace + '/native-ticket'
  const gateway = 'https://native-t3.example.invalid'
  const fixturePassword = 'synthetic-workspace-password'
  const sessionToken = 'synthetic-workspace-session'
  const errors = [], apiMutations = [], checks = []
  let enabled = true
  let issued = 0
  let submitted = 0
  let pendingTicket = ''
  let logins = 0

  const check = (name, pass) => {
    checks.push({ name, pass: !!pass })
    if (!pass) throw Error(name)
  }
  page.setDefaultTimeout(15000)
  page.on('pageerror', error => errors.push(error.message))
  await page.context().addInitScript(({ origin, id }) => {
    if (location.origin !== origin) return
    localStorage.setItem('sub2api_locale', 'en')
    localStorage.setItem('theme', 'light')
    for (const key of ['onboarding_tour', 'admin_guide', 'user_guide']) {
      localStorage.setItem(key + '_' + id + '_admin_v4_interactive', 'true')
      localStorage.setItem(key + '_guest_admin_v4_interactive', 'true')
    }
  }, { origin: base, id: admin.id })

  const json = (route, data) => route.fulfill({ json: { code: 0, data } })
  const rejectMutation = (route, url) => {
    apiMutations.push({ path: url.pathname, method: route.request().method() })
    return route.abort()
  }
  await page.route('**/*', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const method = request.method()
    const path = url.pathname
    if (url.origin === gateway) {
      if (path === '/_mile/enter' && method === 'POST') {
        const headers = await request.allHeaders()
        check('gateway-origin', headers.origin === base)
        check('gateway-form-content-type', headers['content-type']?.startsWith('application/x-www-form-urlencoded'))
        check('ticket-only-in-body', !!pendingTicket && request.postData() === 'ticket=' + pendingTicket)
        check('ticket-absent-from-url', !url.search && !url.hash)
        check('mile-authorization-not-forwarded', !headers.authorization && !headers.cookie)
        submitted++
        pendingTicket = ''
        return route.fulfill({ contentType: 'text/html', body: '<h1>Native T3 workspace fixture</h1>' })
      }
      return rejectMutation(route, url)
    }
    if (url.origin !== base) {
      return method === 'GET' ? route.abort() : rejectMutation(route, url)
    }
    if (path === '/setup/status') return route.fulfill({ json: { needs_setup: false } })
    if (path === '/api/v1/auth/login' && method === 'POST') {
      const body = request.postDataJSON()
      check('synthetic-login', body.email === admin.email && body.password === fixturePassword)
      logins++
      return json(route, { access_token: sessionToken, user: admin })
    }
    if (path === ticketPath && method === 'POST') {
      check('authenticated-admin-ticket-request', request.headers().authorization === 'Bearer ' + sessionToken)
      const bodyText = request.postData() || ''
      if (bodyText) {
        const parsed = JSON.parse(bodyText)
        check('valid-ticket-next-body', typeof parsed.next === 'string' && parsed.next.startsWith('/'))
      } else {
        check('empty-ticket-request-body', !bodyText)
      }
      if (!enabled) return route.fulfill({ status: 404, json: { code: 404, message: 'Native workspace disabled' } })
      await new Promise(resolve => setTimeout(resolve, 400))
      issued++
      pendingTicket = 'synthetic-ticket-' + issued
      return json(route, { url: gateway + '/_mile/enter', ticket: pendingTicket })
    }
    if (method !== 'GET' && method !== 'HEAD') return rejectMutation(route, url)
    if (!path.startsWith('/api/v1/')) return route.continue()
    if (path.endsWith('/auth/me') || path.endsWith('/user/profile')) return json(route, admin)
    if (path.endsWith(workspace + '/status')) return json(route, { enabled: false })
    return json(route, fixtures.resolve(path, method))
  })

  for (const [index, width] of [1440, 375].entries()) {
    await page.setViewportSize({ width, height: 900 })
    if (index) {
      await page.goto(base + '/admin/dashboard', { waitUntil: 'load' })
      await page.evaluate(() => {
        for (const key of ['auth_token', 'auth_user', 'refresh_token', 'token_expires_at']) localStorage.removeItem(key)
      })
    }
    await page.goto(base + workspace, { waitUntil: 'domcontentloaded' })
    await page.locator('#email').fill(admin.email)
    check('direct-workspace-link-login-first-' + width, new URL(page.url()).pathname === '/login' && issued === index)
    await page.locator('#password').fill(fixturePassword)
    await page.locator('form button[type="submit"]').click()
    await page.waitForURL(base + '/admin/dashboard')
    await page.waitForLoadState('networkidle')
    check('login-lands-on-admin-dashboard-' + width, logins === index + 1 && issued === index)
    if (width < 1024) await page.getByRole('button', { name: 'Toggle menu' }).click()
    const link = page.getByRole('link', { name: 'Development Workspace', exact: true })
    await link.waitFor({ state: 'visible' })
    await link.hover()
    await page.waitForLoadState('networkidle')
    check('navigation-hover-does-not-issue-ticket-' + width, issued === index)
    await link.click()
    await page.getByRole('heading', { name: 'Native T3 workspace fixture' }).waitFor({ state: 'visible' })
    check('native-gateway-landing-' + width,
      page.url() === gateway + '/_mile/enter' && submitted === index + 1 && issued === index + 1)
  }

  await page.goto(base + workspace + '?next=%2Fthreads%2Fdemo-1', { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: 'Native T3 workspace fixture' }).waitFor({ state: 'visible' })
  check('deep-link-next-ticket-handoff', page.url() === gateway + '/_mile/enter' && submitted === 3 && issued === 3)

  enabled = false
  await page.goto(base + workspace, { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: 'Development Workspace', exact: true }).waitFor({ state: 'visible' })
  check('disabled-native-gateway-preserves-existing-workspace', page.url() === base + workspace && submitted === 3 && issued === 3)
  check('no-real-business-writes', apiMutations.length === 0)
  return { errors, apiMutations, checks, issued, submitted, logins }
}
