// Run with playwright-cli run-code --filename ../tools/review_mile_frontend.js (from frontend/).
async (page) => {
  const browser = page.context().browser();
  const engine = browser.browserType().name();
  const output = '../output/playwright/design-system';
  const result = { engine, measurements: [], checks: [], errors: [] };
  const config = {
    site_name: 'MILE Lab', site_subtitle: 'Every Mile Matters.', site_logo: '',
    registration_enabled: true, email_verify_enabled: false, password_reset_enabled: true,
    subscription_enabled: false, payment_enabled: false, backend_mode_enabled: false,
    compact_home_enabled: false, home_content: '', model_plaza_enabled: false,
    channel_monitor_enabled: true, available_channels_enabled: false,
    custom_menu_items: [], custom_endpoints: [], api_base_url: 'https://api.mile.example',
    table_default_page_size: 20, table_page_size_options: [10,20,50,100],
    affiliate_enabled: false, version: '1.0.0',
  };
  async function create({ auth = true, theme = 'light', width = 1440, role = 'user', quota = 'normal', simple = false } = {}) {
    const context = await browser.newContext({ viewport: {width, height:1000}, locale:'zh-CN', colorScheme:'light' });
    context.setDefaultTimeout(15000);
    context.setDefaultNavigationTimeout(15000);
    const user = { id:1, email:'student@mile.example', username:'MILE Student', role, status:'active', balance:100, concurrency:5, created_at:'2026-10-01T00:00:00Z', run_mode:simple ? 'simple' : 'standard' };
    await context.addInitScript(({config,user,auth,theme}) => {
      window.__APP_CONFIG__ = config;
      localStorage.setItem('sub2api_locale','zh');
      if (theme) localStorage.setItem('theme',theme);
      if (auth) { localStorage.setItem('auth_token','visual-review-fixture'); localStorage.setItem('auth_user',JSON.stringify(user)); }
      localStorage.setItem('user_guide_1_user_v4_interactive','true');
      localStorage.setItem('admin_guide_1_admin_v4_interactive','true');
      window.__review = { lcp:0, cls:0 };
      try { new PerformanceObserver(list => { const entries=list.getEntries(); window.__review.lcp=entries[entries.length-1].startTime; }).observe({type:'largest-contentful-paint', buffered:true}); } catch {}
      try { new PerformanceObserver(list => { for (const e of list.getEntries()) if (!e.hadRecentInput) window.__review.cls+=e.value; }).observe({type:'layout-shift', buffered:true}); } catch {}
    }, {config,user,auth,theme});
    const p = await context.newPage();
    const requests = [];
    const writes = [];
    p.on('pageerror', error => result.errors.push(error.message));
    await p.route('**/api/v1/**', async route => {
      const path = new URL(route.request().url()).pathname.replace('/api/v1','');
      requests.push(path);
      if(route.request().method() === 'POST') writes.push(path);
      let data = {};
      if (path === '/auth/login') data={access_token:'visual-review-fixture',token_type:'Bearer',expires_in:3600,user};
      else if (path === '/auth/me' || path === '/user/profile') data=user;
      else if (path === '/settings/public') data=config;
      else if (path === '/announcements') data=[];
      else if (path.includes('user-attributes')) data=[];
      else if (path === '/user/platform-quotas') {
        if (quota === 'failed') return route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({code:503,message:'Review fixture: quota unavailable'})});
        data = {purpose_allowances:[
          {purpose:'openai',display_percent:quota === 'unknown' ? null : quota === 'exhausted' ? 100 : 24,point_used:'240',point_limit:'1000',reset_scope:'shared',resets_at:new Date(Date.now()+36000000).toISOString()},
          {purpose:'grok',display_percent:67,point_used:'670',point_limit:'1000',reset_scope:'personal',resets_at:new Date(Date.now()+18000000).toISOString()},
          {purpose:'experiment',display_percent:12,used_amount:6,limit_amount:50,reset_scope:'month',resets_at:'2026-11-01T00:00:00Z'},
        ]};
      }
      else if (path === '/keys' && route.request().method() === 'POST') data={id:7,name:'Review key',key:'sk-review-only-never-real',status:'active',group_id:1,created_at:'2026-10-01T00:00:00Z'};
      else if (path === '/keys' || path.includes('/users')) data={items:[],total:0,page:1,page_size:20,pages:0};
      else if (path.includes('groups') && !path.includes('rates')) data=[{id:1,name:'Codex Lab',platform:'openai',usage_kind:'coding',rate_multiplier:1,subscription_type:'standard'}];
      else if (path.includes('compliance')) data={required:false,acknowledged:true};
      else if (path === '/admin/settings') data={...config,ops_monitoring_enabled:false};
      else if (path.includes('trend')) data={trend:[]};
      else if (path.includes('models')) data={models:[]};
      else if (path.includes('usage')) data={items:[],total:0,page:1,page_size:20,pages:0};
      await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({code:0,data})});
    });
    await p.route('**/setup/status', route=>route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({code:0,data:{needs_setup:false}})}));
    return { context, p, requests, writes };
  }
  async function settle(p, path, port=4173) {
    await p.goto(`http://127.0.0.1:${port}${path}`);
    await p.locator('#app main').first().waitFor();
    await p.evaluate(()=>Promise.race([document.fonts.ready, new Promise(resolve=>setTimeout(resolve,3000))]));
    // A fixed observation window for local production comparisons, not a live API wait.
    await p.waitForTimeout(800);
  }
  async function shot(p, name) {
    const overflow=await p.evaluate(()=>document.documentElement.scrollWidth > innerWidth+1);
    const error=await p.locator('#main-content > p').allTextContents();
    result.checks.push({name,overflow,url:p.url(),headings:await p.locator('h1').allTextContents(),error});
    await p.screenshot({path:`${output}/${engine}-${name}.png`,fullPage:true,animations:'disabled'});
  }
  // Fresh contexts, same production builds and fixtures, 3 samples per entry point.
  if (engine === 'chromium') {
    for (const port of [4174,4173]) for (const path of ['/home','/dashboard']) for (let run=0;run<3;run++) {
      const {context,p,requests}=await create({auth:path==='/dashboard',theme:null});
      await settle(p,path,port);
      result.measurements.push({build:port===4174?'before':'after',path,run,...await p.evaluate(()=>({...window.__review,resources:performance.getEntriesByType('resource').map(e=>e.name.split('/').pop())})),requests});
      await context.close();
    }
  }
  for (const width of [1440,768,375]) {
    const {context,p,requests}=await create({width});
    for (const path of ['/home','/dashboard','/models','/keys','/guide']) {
      await settle(p,path);
      await shot(p,`${path.slice(1)}-${width}`);
      if(path==='/dashboard') result.checks.push({name:`dashboard-requests-${width}`,requests:[...requests],noPrematureHistory:!requests.some(path=>/dashboard\/(stats|trend|models)|^\/usage/.test(path))});
    }
    if(width===375) {
      await settle(p,'/dashboard');
      const trigger=p.getByRole('button',{name:'切换菜单'});
      await trigger.click();
      const drawer=p.locator('aside[role="dialog"]');
      await drawer.waitFor();
      await shot(p,'navigation-375');
      await p.keyboard.press('Shift+Tab');
      const trapped=await drawer.evaluate(el=>el.contains(document.activeElement));
      await p.keyboard.press('Escape');
      await drawer.waitFor({state:'hidden'});
      result.checks.push({name:'drawer-keyboard',trapped,restored:await trigger.evaluate(el=>el===document.activeElement)});
    }
    if(width===1440) {
      // 200% layout reflow: halve the CSS viewport, preserving the same physical display area.
      const reflow=await create({width:720});
      await settle(reflow.p,'/models');
      await shot(reflow.p,'models-200-percent-reflow');
      await reflow.context.close();
    }
    await context.close();
    const guest=await create({auth:false,width});
    for(const path of ['/login','/register','/forgot-password']) { await settle(guest.p,path); await shot(guest.p,`${path.slice(1)}-${width}`); }
    if(width===1440) {
      await settle(guest.p,'/home'); await shot(guest.p,'home-guest');
      await settle(guest.p,'/login');
      await guest.p.locator('input[type="email"]').fill('student@mile.example');
      await guest.p.locator('input[type="password"]').fill('visual-review-password');
      await guest.p.locator('button[type="submit"]').click();
      await guest.p.waitForURL('**/dashboard');
      result.checks.push({name:'login-redirect',passed:true});
    }
    await guest.context.close();
  }
  for (const quota of ['unknown','exhausted','failed']) {
    const {context,p}=await create({quota});
    await settle(p,'/dashboard');
    const card=p.locator('[data-testid="openai-purpose-card"]');
    result.checks.push({name:`quota-${quota}`,unknownNoFill:quota==='unknown'?await card.locator('.quota-meter-fill').count()===0:undefined,alert:quota==='failed'?await p.getByRole('alert').count():undefined});
    await shot(p,`quota-${quota}`);
    await context.close();
  }
  const dark=await create({theme:'dark'});
  for(const path of ['/home','/dashboard','/models']) {await settle(dark.p,path);await shot(dark.p,`${path.slice(1)}-dark`);}
  await dark.context.close();
  const admin=await create({role:'admin'});
  await settle(admin.p,'/admin/users');await shot(admin.p,'admin-users');await admin.context.close();
  const core=await create();
  await settle(core.p,'/models');
  const response=[];
  for(let i=0;i<20;i++) {
    response.push(await core.p.evaluate(async i=>{
      const input=document.querySelector('#model-search');const start=performance.now();
      input.value=i%2?'grok':'';input.dispatchEvent(new Event('input',{bubbles:true}));
      await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
      return performance.now()-start;
    },i));
  }
  response.sort((a,b)=>a-b);
  result.checks.push({name:'filter-response',samples:response,p95:response[Math.ceil(response.length*.95)-1]});
  await settle(core.p,'/keys');
  const createTrigger=core.p.locator('[data-tour="keys-create-btn"]');
  await createTrigger.click();
  const dialog=core.p.getByRole('dialog');await dialog.waitFor();await shot(core.p,'key-dialog');
  await core.p.keyboard.press('Shift+Tab');
  const trapped=await dialog.evaluate(el=>el.contains(document.activeElement));
  await core.p.keyboard.press('Escape');
  await dialog.waitFor({state:'hidden'});
  result.checks.push({name:'dialog-keyboard',trapped,restored:await createTrigger.evaluate(el=>el===document.activeElement)});
  const dialogResponse=[];
  for(let i=0;i<20;i++) {
    dialogResponse.push(await core.p.evaluate(async()=>{
      const start=performance.now();document.querySelector('[data-tour="keys-create-btn"]').click();
      await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
      return performance.now()-start;
    }));
    await core.p.keyboard.press('Escape');
    await dialog.waitFor({state:'hidden'});
  }
  dialogResponse.sort((a,b)=>a-b);
  result.checks.push({name:'dialog-response',samples:dialogResponse,p95:dialogResponse[Math.ceil(dialogResponse.length*.95)-1]});
  await createTrigger.click();await dialog.waitFor();
  await core.p.locator('[data-tour="key-form-name"]').fill('Review key');
  await core.p.locator('#key-form-group').click();
  await core.p.getByRole('option').filter({hasText:'Codex Lab'}).click();
  await core.p.locator('[data-tour="key-form-submit"]').click();
  await dialog.waitFor({state:'hidden'});
  result.checks.push({name:'key-create',posted:core.writes.includes('/keys')});
  if(engine==='chromium') {
    await core.context.grantPermissions(['clipboard-read','clipboard-write']);
    await settle(core.p,'/models');
    const copy=core.p.locator('button[data-testid^="copy-"]').first();
    const id=(await copy.getAttribute('data-testid')).slice(5);
    await copy.click();
    result.checks.push({name:'model-copy',matches:await core.p.evaluate(()=>navigator.clipboard.readText())===id});
    await settle(core.p,'/home');
    await core.p.locator('.lab-home-path details summary').first().click();
    await core.p.locator('.lab-home-config button').first().click();
    const config=await core.p.evaluate(()=>navigator.clipboard.readText());
    result.checks.push({name:'config-copy',matches:config.includes('wire_api = "responses"')});
  }
  await settle(core.p,'/dashboard');
  await core.p.emulateMedia({reducedMotion:'reduce'});
  result.checks.push({name:'reduced-motion',animation:await core.p.locator('.spectrum-fold').evaluate(el=>getComputedStyle(el).animationName)});
  await core.p.emulateMedia({reducedMotion:'no-preference'});
  const frames=await core.p.evaluate(async()=>{
    const deltas=[];let last=performance.now();for(let i=0;i<60;i++){await new Promise(resolve=>requestAnimationFrame(resolve));const now=performance.now();deltas.push(now-last);last=now;}return deltas;
  });
  result.checks.push({name:'animation-frames',frames});
  await core.p.locator('.lab-welcome').evaluate(el=>el.style.marginBottom='1200px');
  await core.p.locator('.lab-history').scrollIntoViewIfNeeded();
  await core.p.waitForTimeout(250);
  result.checks.push({name:'offscreen-animation',active:await core.p.locator('.spectrum-art').evaluate(el=>el.classList.contains('is-active'))});
  await core.context.close();
  return result;
}
