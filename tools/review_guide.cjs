async page => {
 const options = __MILE_REVIEW_OPTIONS__;
 const {base} = options;
 const dir = options.output + '/';
 const fixtures = (__MILE_FIXTURES__)(base);
 const auth = fixtures.auth;
 await page.unrouteAll();
 page.setDefaultTimeout(10000);
 page.setDefaultNavigationTimeout(15000);
 if (options.clipboard) await page.context().grantPermissions(['clipboard-read','clipboard-write']);
 await page.context().addInitScript(auth => {
  localStorage.setItem('auth_token','review-mock'); localStorage.setItem('auth_user',JSON.stringify(auth));
  localStorage.setItem('sub2api_locale','zh'); localStorage.setItem('theme','light');
  for(const name of ['onboarding_tour','user_guide','admin_guide']) for(const u of ['3_user','guest_user']) localStorage.setItem(name+'_'+u+'_v4_interactive','true');
 }, auth);
 const errors=[],calls=[],measurements=[];
 page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/*', async route => {
  const request = route.request();
  const url = new URL(request.url());
  const method = request.method();
  if (url.origin !== base) return route.abort();
  if (url.pathname === '/setup/status' && method === 'GET') return route.fulfill({json:{needs_setup:false}});
  if (url.pathname.startsWith('/api/v1/')) {
   calls.push({path:url.pathname,method});
   try {
    const data = fixtures.resolve(url.pathname, method);
    return route.fulfill({json:{code:0,message:'ok',data}});
   } catch (error) { errors.push(error.message); return route.abort(); }
  }
  if (!['GET','HEAD'].includes(method) || /^\/(?:v1|backend-api)(?:\/|$)/.test(url.pathname)) {
   errors.push('Blocked request '+method+' '+url.pathname); return route.abort();
  }
  return route.continue();
 });

 await page.setViewportSize({width:1440,height:1000});
 await page.goto(base+'/guide',{waitUntil:'domcontentloaded'});
 await page.locator('a[data-guide-path="codex"]').waitFor();
 await page.evaluate(()=>Promise.race([document.fonts.ready,new Promise(resolve=>setTimeout(resolve,2000))]));
 const observations=[];
 async function aligned(id){
  try { await page.waitForFunction(id=>{
   const el=document.getElementById(id);if(!el||el.getClientRects().length===0)return false;
   const desired=Math.min(Math.max(el.getBoundingClientRect().top+scrollY-112,0),document.documentElement.scrollHeight-innerHeight);
   return location.hash==='#'+id && Math.abs(scrollY-desired)<4;
  },id,{timeout:10000}); } catch (error) {
   const state = await page.evaluate(id=>({id,hash:location.hash,scrollY,focus:document.activeElement.id,top:document.getElementById(id)?.getBoundingClientRect().top,height:document.documentElement.scrollHeight,viewport:innerHeight}),id);
   await page.screenshot({path:dir+'failure.png'});
   throw new Error(error.message+'; '+JSON.stringify(state));
  }
  const active=await page.locator('aside a[aria-current="location"]').getAttribute('href');
  if(!active.endsWith('#'+id))throw Error('Directory differs from hash '+id);
  if(await page.evaluate(()=>document.activeElement.id)!==id)throw Error('Focus did not enter '+id);
  observations.push(await page.evaluate(id=>({id,hash:location.hash,scrollY,top:document.getElementById(id).getBoundingClientRect().top}),id));
 }
 for(const [purpose,id] of [['codex','guide-codex'],['grok_build','guide-grok'],['experiment','guide-experiment']]){
  await page.locator('a[data-guide-path="'+purpose+'"]').click();await aligned(id);
  if(!await page.locator('#'+id).isVisible())throw Error('Hidden destination '+id);
 }
 // Re-selecting the current hash must still jump after manually returning up.
 await page.evaluate(()=>window.scrollTo({top:0,behavior:'instant'}));
 await page.locator('a[data-guide-path="experiment"]').click();await aligned('guide-experiment');
 // The top links require returning up; history must still resolve the chapter.
 await page.locator('a[data-guide-path="grok_build"]').click();await aligned('guide-grok');
 await page.goBack();await aligned('guide-experiment');
 await page.goForward();await aligned('guide-grok');
 const toc=await page.locator('aside nav[data-guide-toc] a').evaluateAll(links=>links.map(a=>new URL(a.href).hash));
 if(JSON.stringify(toc)!==JSON.stringify(['#guide-prep','#guide-codex','#guide-grok','#guide-experiment','#guide-check']))throw Error('Incomplete directory '+JSON.stringify(toc));
 for(const id of ['guide-prep','guide-codex','guide-grok','guide-experiment','guide-check']){
  await page.locator('aside a[href$="#'+id+'"]').click();await aligned(id);
 }
 await page.goto(base+'/guide?purpose=codex#guide-experiment',{waitUntil:'domcontentloaded'});await aligned('guide-experiment');
 await page.reload({waitUntil:'domcontentloaded'});await aligned('guide-experiment');
 await page.locator('#guide-experiment summary').click();
 const file=page.locator('#guide-experiment .lab-guide-file').first();
 await page.locator('#guide-experiment pre').first().waitFor();
 await page.screenshot({path:dir+'after-experiment-1440.png',fullPage:false});
 const layouts=[];
 for(const width of [375,768,1440]){
  await page.setViewportSize({width,height:1000});
  await page.evaluate(()=>window.scrollTo({top:0,behavior:'instant'}));
  await page.waitForTimeout(250);
  await page.screenshot({path:dir+'after-top-'+width+'.png',fullPage:false});
  if(width<1280){await page.locator('nav[data-guide-toc]').first().locator('a[href$="#guide-experiment"]').click();
   await page.waitForFunction(()=>location.hash==='#guide-experiment');
   await page.locator('a[data-guide-path="codex"]').click();
   await page.waitForFunction(()=>location.hash==='#guide-codex');
  }
  const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth);
  if(overflow)throw Error('Overflow '+width);layouts.push({width,overflow});
 }
 // Keyboard activation and copying are tested on the actual rendered controls.
 await page.evaluate(()=>window.scrollTo({top:0,behavior:'instant'}));
 const entry=page.locator('a[data-guide-path="experiment"]');await entry.focus();await page.keyboard.press('Enter');await aligned('guide-experiment');
 await page.locator('aside a[href$="#guide-prep"]').click();await aligned('guide-prep');
 if (options.clipboard) {
  await page.locator('.api-base-url__copy').click();
  const copied = await page.evaluate(()=>navigator.clipboard.readText());
  if(copied !== base+'/v1') throw Error('Address copy broken');
 }
 if(errors.length)throw Error('Page errors '+JSON.stringify(errors));
 return {observations,layouts,errors,apiMutations:calls.filter(c=>c.method!=='GET'&&!c.path.endsWith('/usage/dashboard/api-keys-usage')).length};
}
