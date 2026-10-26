// Real Chromium / production templates. All API fixtures are explicitly synthetic.
const {test,before,after}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {chromium}=require('playwright');
const root=path.resolve(__dirname,'../src/openvpn-web/templates');
const shots=process.env.UI_SCREENSHOT_DIR || '/root/.hermes/cache/scratch/admin-console-enterprise-screenshots';
let browser;
before(async()=>{fs.mkdirSync(shots,{recursive:true});browser=await chromium.launch({headless:true,args:['--no-sandbox']});});
after(async()=>{await browser?.close();});
async function openPage({width=1440,tab='client',populated=false,delay=0,fail=false}={}) {
  const page=await browser.newPage({viewport:{width,height:960}});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  const state={fail,delay,mutations:[]};
  await page.route('http://vpn.test/**',async route=>{
    const req=route.request(),url=new URL(req.url());
    const local=path.join(root,url.pathname);
    if((url.pathname.startsWith('/static/')||url.pathname.endsWith('.html'))&&fs.existsSync(local)) {
      let body=fs.readFileSync(local);
      if(local.endsWith('.html')) body=body.toString().replace(/{{[^}]+}}/g,'测试环境');
      return route.fulfill({body,contentType:{'.js':'application/javascript','.html':'text/html','.css':'text/css','.json':'application/json'}[path.extname(local)]||'application/octet-stream'});
    }
    const user={id:1,gid:1,username:'fixture.engineering',ipAddr:'10.8.0.20',ovpnConfig:'engineering.ovpn',mfaEnabled:true,isEnable:true,name:'合成测试账号',email:'fixture@example.test',expireDate:'2030-01-01',createdAt:'2026-01-01',updatedAt:'2026-01-01'};
    const clients=populated?[{name:'engineering-fixture',fullName:'engineering-fixture.ovpn',file:'/download/engineering-fixture.ovpn',date:'2026-10-01 10:30:00'}]:[];
    const history=populated?[{username:'fixture.engineering',common_name:'engineering-fixture',vip:'10.8.0.20',vip6:'',rip:'192.0.2.20',bytes_received:'128 KB',bytes_sent:'256 KB',time_unix:'2026-10-01 10:30:00',time_duration:'00:12:30'}]:[];
    const api={
      '/csrf':{csrfToken:'synthetic-presentation-token'},
      '/ovpn/client':clients,
      '/ovpn/group':populated?[{id:1,parent_id:null,name:'合成测试组织'}]:[],
      '/ovpn/group/null/users':{users:populated?[user]:[],authUser:true},
      '/ovpn/online-client':{server:{Address:'vpn.example.test:1194',Status:'运行中',BytesIn:'128 KB',BytesOut:'256 KB',RunDate:'2026-10-01 10:00:00'},clients:populated?[{id:1,username:user.username,commonName:'engineering-fixture',vip:'10.8.0.20',vip6:'',rip:'192.0.2.20',sendBytes:0,recvBytes:0,connDate:'2026-10-01 10:30:00',onlineTime:'00:12:30',isNftBlacklist:false}]:[]},
      '/ovpn/firewall':populated?[{id:1,sip:'10.8.0.0/24',dip:'192.0.2.0/24',sg:[],dg:[],policy:'accept',status:true,comment:'合成测试规则',createdAt:'2026-10-01'}]:[],
      '/ovpn/certs':populated?[{name:'server-fixture',type:'服务器',status:'正常',notBefore:'2026-01-01',notAfter:'2030-01-01',expiresIn:'1162'}]:[],
      '/ovpn/history':{draw:Number(url.searchParams.get('draw')),recordsTotal:history.length,recordsFiltered:history.length,data:history},
      '/settings':{system:{base:{},ldap:{},email:{}},client:{client_url:{},mfa_app_url:{}},openvpn:{}},
      '/ovpn/server':{content:'# Synthetic browser fixture only\nport 1194\nproto udp\ndev tun',message:'合成测试响应'},
    };
    if(req.method()!=='GET'&&url.pathname!=='/csrf') state.mutations.push({path:url.pathname,body:req.postData()});
    if(url.pathname.startsWith('/ovpn/')&&req.method()==='GET') {
      if(state.delay) await new Promise(r=>setTimeout(r,state.delay));
      if(state.fail && url.pathname!=='/ovpn/group') return route.fulfill({status:503,json:{message:'合成测试：服务暂不可用'}});
    }
    return route.fulfill({json:api[url.pathname]||{}});
  });
  await page.goto('http://vpn.test/index.html?tab='+tab);
  await page.waitForFunction(()=>window.vtable);
  return {page,errors,state};
}
async function settled(page){await page.waitForFunction(()=>window.vtable && !document.querySelector('#vtableContainer').matches('[data-state="loading"]') && !!document.querySelector('#vtable tbody'));}
async function shot(page,name){await page.screenshot({path:path.join(shots,name+'.png'),fullPage:true,animations:'disabled'});}
async function noOverflow(page){assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'no document overflow');}
test('phone summaries and action labels stay readable and footer controls do not compete',async()=>{
  const {page}=await openPage({width:320,tab:'status',populated:true});await settled(page);
  assert.equal(await page.locator('#serverTable td').nth(1).evaluate(e=>getComputedStyle(e).whiteSpace),'nowrap');
  await page.close();
  const client=await openPage({width:390,populated:true});await settled(client.page);
  const length=await client.page.locator('.dt-length').boundingBox();const info=await client.page.locator('.dt-info').boundingBox();
  assert.ok(info.y>=length.y+length.height,JSON.stringify({length,info,footer:await client.page.locator('#vtable_wrapper').evaluate(e=>e.lastElementChild.outerHTML)}));
  assert.equal(await client.page.locator('#vtable a[download]').evaluate(e=>getComputedStyle(e).whiteSpace),'nowrap');
  assert.equal(await client.page.locator('#vtable a[download]').evaluate(e=>getComputedStyle(e).color),'rgb(15, 118, 110)');
  await noOverflow(client.page);await client.page.close();
});
test('rapid navigation while table language loads never initializes destroyed tables',async()=>{
  const {page,errors}=await openPage();
  await page.evaluate(()=>{for(const id of ['showUser','showClient','showFirewall','showHistory','manageCert','showStatus'])document.getElementById(id).click();});
  await page.waitForFunction(()=>document.querySelector('#vtableContainer').dataset.state==='ready');
  await page.waitForTimeout(1200);
  assert.equal(await page.locator('#consoleTitle').textContent(),'在线状态');
  assert.deepEqual(errors,[]);assert.equal(await page.locator('#vtable_wrapper').count(),1);await page.close();
});
test('client forms expose labels, required validation and pending/failure feedback',async()=>{
  const {page,errors,state}=await openPage();await settled(page);
  await page.getByRole('button',{name:'添加',exact:true}).click();await page.waitForSelector('#addClientModal.show');
  const name=page.locator('#addClientModal').getByLabel('名称',{exact:false});
  assert.equal(await name.count(),1);
  await page.locator('#addClientModal button[type=submit]').click();
  assert.equal(await name.getAttribute('aria-invalid'),'true');
  assert.equal(state.mutations.length,0);
  await name.fill('browser-fixture');
  await page.route('http://vpn.test/ovpn/client',async route=>{
    if(route.request().method()==='POST') {await new Promise(r=>setTimeout(r,800));return route.fulfill({status:503,json:{message:'Synthetic failure'}});}
    return route.fallback();
  });
  await page.locator('#addClientModal button[type=submit]').click();
  assert.equal(await page.locator('#addClientModal').getAttribute('aria-busy'),'true');
  assert.equal(await page.locator('#addClientModal button[type=submit]').isDisabled(),true);
  await page.waitForFunction(()=>document.querySelector('#addClientModal .operation-feedback')?.textContent.includes('操作失败'));
  assert.equal(await page.locator('#addClientModal button[type=submit]').isDisabled(),false);
  assert.equal(await name.inputValue(),'browser-fixture');
  await shot(page,'client-form-error');
  // Existing rejected mutation promises are asserted by security tests; presentation consumes UI errors.
  assert.deepEqual(errors,[]);await page.close();
});
test('filtered clients explain no results and offer a working clear-search action',async()=>{
  const {page,errors}=await openPage({populated:true});await settled(page);
  await page.locator('.dt-search input').fill('does-not-exist');
  await page.waitForSelector('.dt-empty');
  assert.match(await page.locator('.dt-empty').textContent(),/没有匹配结果/);
  await shot(page,'client-no-results');
  await page.getByRole('button',{name:'清除搜索',exact:true}).click();
  await page.waitForSelector('#vtable a[download]');
  assert.equal(await page.locator('.dt-search input').inputValue(),'');
  assert.equal(await page.locator('#vtable tbody tr').count(),1);
  assert.deepEqual(errors,[]);await page.close();
});
test('client loading and failure are distinct, accessible and retryable without mutations',async()=>{
  const {page,errors,state}=await openPage({delay:1200,fail:true});
  await page.waitForSelector('#tableFeedback');
  assert.equal(await page.locator('#vtableContainer').getAttribute('aria-busy'),'true');
  assert.match(await page.locator('#tableFeedback').textContent(),/正在加载/);
  await shot(page,'client-loading');
  await page.waitForFunction(()=>document.querySelector('#vtableContainer').dataset.state==='error');
  assert.match(await page.locator('#tableFeedback').textContent(),/加载失败/);
  assert.equal(await page.locator('#vtableContainer').getAttribute('aria-busy'),'false');
  await shot(page,'client-error');
  state.fail=false;state.delay=0;
  await page.getByRole('button',{name:'重新加载',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#vtableContainer').dataset.state==='ready');
  assert.match(await page.locator('.dt-empty').textContent(),/暂无客户端/);
  assert.deepEqual(state.mutations,[]);assert.deepEqual(errors,[]);await page.close();
});
test('account and history toolbars align search filters and actions on one desktop row',async()=>{
  for(const tab of ['user','history']) {
    const {page}=await openPage({tab,populated:true});await settled(page);
    const search=await page.locator('.dt-search input').boundingBox();
    const action=await page.locator('.dt-buttons button').last().boundingBox();
    assert.ok(Math.abs(search.y-action.y)<=2,tab+' toolbar is aligned');
    await page.close();
  }
});
test('wide client lists stay bounded rather than stretching sparse content',async()=>{
  const {page}=await openPage({width:1920,populated:true});await settled(page);
  assert.ok((await page.locator('.console-table-column').boundingBox()).width<=1200);
  const row=await page.locator('#vtable tbody tr').boundingBox();assert.ok(row.height>=44&&row.height<=48);
  await page.close();
});
test('firewall save shares pending feedback without changing its payload',async()=>{
  const {page}=await openPage({tab:'firewall'});await settled(page);
  await page.getByRole('button',{name:'添加',exact:true}).click();await page.waitForSelector('#addFirewallModal.show');
  await page.locator('#addFirewallModal input[name=sip]').fill('10.8.0.0/24');
  await page.locator('#addFirewallModal button[name=saddBtn]').click();
  await page.locator('#addFirewallModal input[name=dip]').fill('192.0.2.0/24');
  await page.locator('#addFirewallModal button[name=daddBtn]').click();
  let payload;
  await page.route('http://vpn.test/ovpn/firewall',async route=>{
    if(route.request().method()!=='POST') return route.fallback();
    payload=new URLSearchParams(route.request().postData());
    await new Promise(r=>setTimeout(r,700));await route.fulfill({json:{message:'合成测试成功'}});
  });
  await page.locator('#addFirewallModal button[type=submit]').click();
  assert.equal(await page.locator('#addFirewallModal').getAttribute('aria-busy'),'true');
  await page.waitForFunction(()=>!document.querySelector('#addFirewallModal').classList.contains('show'));
  assert.equal(payload.get('sip'),'10.8.0.0/24');assert.equal(payload.get('dip'),'192.0.2.0/24');assert.equal(payload.get('policy'),'drop');
  await page.close();
});
test('forms and settings tabs share control sizing without clipping',async()=>{
  for(const width of [320,1440]) {
    for(const [tab,modal] of [['user','addUserModal'],['client','addClientModal'],['firewall','addFirewallModal']]) {
      const {page,errors}=await openPage({width,tab});await settled(page);
      await page.getByRole('button',{name:'添加',exact:true}).click();await page.waitForSelector('#'+modal+'.show');
      await noOverflow(page);
      assert.ok(await page.locator('#'+modal+' .modal-content').evaluate(e=>e.scrollWidth<=e.clientWidth));
      const field=page.locator('#'+modal+' input:not([hidden]):not([type=checkbox])').first();
      assert.ok(Math.abs((await field.boundingBox()).height-(width<992?44:36))<0.1);
      if(tab==='firewall'&&width<992) assert.ok((await page.locator('#'+modal+' button[name=saddBtn]').evaluate(e=>e.parentElement.getBoundingClientRect().height))<=60,'mobile transfer actions must not reserve a 232px empty column');
      await shot(page,`${tab}-form-${width}`);assert.deepEqual(errors,[]);await page.close();
    }
    const {page}=await openPage({width});await settled(page);
    if(width<992){await page.getByRole('button',{name:'打开导航',exact:true}).click();await page.waitForSelector('#offcanvasNavbar.show');}
    await page.locator('#settings').click();await page.waitForSelector('#settingsModal.show');
    for(const section of ['client','openvpn']) {
      await page.locator('#pills-'+section+'-tab').click();await page.waitForSelector('#pills-'+section+'.show');
      await noOverflow(page);await shot(page,`settings-${section}-${width}`);
    }
    await page.close();
  }
});
test('all nine management surfaces render empty/populated at five target viewports',async()=>{
  for(const width of [320,390,1280,1440,1920]) {
    for(const populated of [false,true]) {
      for(const tab of ['client','status','user','firewall','history','cert']) {
        const {page,errors}=await openPage({width,tab,populated});await settled(page);
        await page.waitForFunction(()=>document.querySelector('#vtableContainer').dataset.state==='ready');
        await noOverflow(page);
        assert.equal(await page.locator('.dt-search input').getAttribute('aria-label'),'搜索'+await page.locator('#consoleTitle').textContent());
        if(!populated) assert.ok((await page.locator('.dt-empty').boundingBox()).height<=144,'compact mobile/desktop empty state');
        if(populated) assert.equal(await page.evaluate(()=>vtable.rows().count()),1);
        await shot(page,`${tab}-${populated?'populated':'empty'}-${width}`);
        assert.deepEqual(errors,[],`${tab} ${width}`);await page.close();
      }
    }
    const {page,errors,state}=await openPage({width,populated:true});await settled(page);
    for(const [id,modal,name] of [['sconfig','editServerModal','config'],['settings','settingsModal','settings'],['restartSrv','restartSrvModal','restart']]) {
      if(width<992){await page.getByRole('button',{name:'打开导航',exact:true}).click();await page.waitForSelector('#offcanvasNavbar.show');}
      await page.locator('#'+id).click();await page.waitForSelector('#'+modal+'.show');
      await noOverflow(page);
      assert.ok(await page.locator('#'+modal+' .modal-content').evaluate(e=>e.scrollWidth<=e.clientWidth));
      await shot(page,`${name}-${width}`);
      await page.locator('#'+modal+' [data-bs-dismiss="modal"]').first().click();
      await page.waitForFunction(id=>!document.getElementById(id).classList.contains('show'),modal);
    }
    assert.equal(state.mutations.filter(m=>m.body?.includes('restartSrv')).length,0,'opening restart never restarts');
    assert.deepEqual(errors,[]);await page.close();
  }
});
test('light shell and client empty state follow compact enterprise geometry',async()=>{
  const {page,errors}=await openPage();await settled(page);
  assert.equal(await page.locator('.console-sidebar').evaluate(e=>getComputedStyle(e).backgroundColor),'rgb(255, 255, 255)');
  assert.equal((await page.locator('.console-sidebar').boundingBox()).width,220);
  assert.equal((await page.locator('.console-topbar').boundingBox()).height,56);
  assert.equal(await page.locator('.console-main').evaluate(e=>getComputedStyle(e).paddingLeft),'24px');
  assert.equal(await page.locator('#consoleTitle').evaluate(e=>getComputedStyle(e).fontSize),'24px');
  assert.ok((await page.locator('.dt-empty').boundingBox()).height<=112,'empty state is compact');
  assert.match(await page.locator('.dt-empty').textContent(),/添加.*客户端/);
  assert.equal(await page.locator('.dt-paging').isVisible(),false);
  assert.equal(await page.locator('.dt-length').isVisible(),false);
  const search=await page.locator('.dt-search input').boundingBox();
  const add=await page.getByRole('button',{name:'添加',exact:true}).boundingBox();
  assert.equal(search.width,240);assert.equal(search.height,36);assert.ok(Math.abs(search.y-add.y)<=2);
  await shot(page,'client-empty-1440');await noOverflow(page);assert.deepEqual(errors,[]);await page.close();
});
