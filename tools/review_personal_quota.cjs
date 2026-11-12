async page => {
 const options=__MILE_REVIEW_OPTIONS__,base=options.base,fixtures=(__MILE_FIXTURES__)(base);
 const auth={...fixtures.auth,id:2,role:'admin'},user={...fixtures.auth,id:99,email:'quota-review@example.invalid',created_at:'2026-10-01T00:00:00Z',updated_at:'2026-10-01T00:00:00Z',subscriptions:[]};
 const end='2026-11-18T06:35:59Z',start='2026-11-11T06:35:59Z';
 const quotas=['openai','grok','experiment'].map(purpose=>({purpose,used:'30',pending:'0',period_start:start,period_end:end}));
 const errors=[],calls=[],writes=[],events=[],results=[];
 let timeoutOnce=false;
 page.setDefaultTimeout(15000);
 await page.context().addInitScript(auth=>{
  localStorage.setItem('auth_token','review-mock');localStorage.setItem('auth_user',JSON.stringify(auth));localStorage.setItem('sub2api_locale','zh');localStorage.setItem('theme','light');
  for(const key of ['onboarding_tour','admin_guide','user_guide'])for(const id of ['2_admin','guest_admin'])localStorage.setItem(key+'_'+id+'_v4_interactive','true');
 },auth);
 page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/*',async route=>{
  const req=route.request(),url=new URL(req.url()),method=req.method(),path=url.pathname;
  if(url.origin!==base)return route.abort();
  if(path==='/setup/status')return route.fulfill({json:{needs_setup:false}});
  if(path.startsWith('/api/v1/')){
   calls.push({path,method});let data={};
   if(path.endsWith('/auth/me')||path.endsWith('/user/profile'))data=auth;
   else if(path==='/api/v1/admin/users'&&method==='GET')data={items:[user],total:1,page:1,page_size:20,pages:1};
   else if(path.endsWith('/purpose-quota-resets')){
    if(method==='GET')data={quotas,events};
    else if(method==='POST'){
     const body=req.postDataJSON();writes.push(body);
     if(body.period_end!==end||!body.confirm||!body.note?.trim()||!body.id)throw Error('Invalid reset payload');
     let event=events.find(e=>e.id===body.id);
     if(!event){event={...body,target_user_id:99,actor_user_id:2,period_start:start,created_at:'2026-11-13T00:00:00Z'};events.unshift(event);quotas.find(q=>q.purpose===body.purpose).used='0';}
     if(timeoutOnce){timeoutOnce=false;return route.abort('failed');}
     data=event;
    }else throw Error('Unexpected reset method');
   }else if(path.endsWith('/platform-quotas'))data={platform_quotas:[]};
   else if(path.includes('/groups'))data=[];
   else if(path.includes('/user-attributes'))data=method==='GET'?[]:{};
   else if(path.endsWith('/dashboard/users-usage'))data={};
   else if(path.endsWith('/subscriptions'))data=[];
   else if(path.endsWith('/settings/public'))data=fixtures.resolve(path,method);
   else if(method!=='GET')throw Error('Blocked business request '+method+' '+path);
   return route.fulfill({json:{code:0,message:'ok',data}});
  }
  if(!['GET','HEAD'].includes(method))throw Error('Blocked external mutation');
  return route.continue();
 });
 await page.setViewportSize({width:1440,height:1000});
 await page.goto(base+'/admin/users',{waitUntil:'domcontentloaded'});
 await page.locator('.action-menu-trigger').first().click();
 await page.locator('.action-menu-content button').filter({hasText:'平台限额'}).click();
 await page.getByTestId('purpose-reset-openai').waitFor();
 for(const [index,purpose] of ['openai','grok','experiment'].entries()){
  await page.getByTestId('purpose-reset-'+purpose).click();
  if(!await page.getByTestId('purpose-reset-confirm').isDisabled())throw Error('Empty reason can submit');
  await page.getByTestId('purpose-reset-reason').fill('浏览器回归：'+purpose);
  if(index===0){
   await page.getByRole('button',{name:'取消',exact:true}).first().click();
   if(writes.length)throw Error('Cancel performed a reset');
   await page.getByTestId('purpose-reset-'+purpose).click();await page.getByTestId('purpose-reset-reason').fill('浏览器回归：'+purpose);
   timeoutOnce=true;
  }
  if(index===0)await page.screenshot({path:options.output+'/confirmation-desktop.png'});
  await page.getByTestId('purpose-reset-confirm').click();
  if(index===0){
   await page.getByRole('alert').filter({hasText:'未能确认重置结果'}).waitFor();
   await page.getByTestId('purpose-reset-confirm').click();
   if(writes.length!==2||writes[0].id!==writes[1].id)throw Error('Retry did not preserve operation ID');
  }
  await page.getByTestId('purpose-reset-confirm').waitFor({state:'detached'});
  results.push({purpose,unchangedEnd:quotas.find(q=>q.purpose===purpose).period_end===end});
 }
 await page.locator('summary').filter({hasText:'最近 20 条重置记录'}).click();
 await page.screenshot({path:options.output+'/history-desktop.png'});
 await page.setViewportSize({width:375,height:812});
 await page.getByTestId('purpose-reset-experiment').click();
 await page.getByTestId('purpose-reset-reason').fill('移动端核对');
 await page.getByTestId('purpose-reset-confirm').scrollIntoViewIfNeeded();
 await page.screenshot({path:options.output+'/confirmation-mobile.png'});
 const box=await page.getByTestId('purpose-reset-confirm').boundingBox();
 if(!box||box.x<0||box.x+box.width>375)throw Error('Mobile confirmation out of viewport');
 if(events.length!==3||writes.length!==4||errors.length)throw Error(JSON.stringify({events,writes,errors}));
 return {errors,apiMutations:[],mockedWrites:writes.length,auditEvents:events.length,results,calls};
}
