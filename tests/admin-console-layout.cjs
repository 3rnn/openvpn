// Actual templates/modules in Chromium; API responses are synthetic, not VPN acceptance.
// NODE_PATH=<directory containing playwright> node --test tests/admin-console-layout.cjs
const {test,before,after}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {chromium}=require('playwright');
const root=path.resolve(__dirname,'../src/openvpn-web/templates');
const shots=process.env.UI_SCREENSHOT_DIR || '/root/.hermes/cache/scratch/admin-console-screenshots';
let browser;
before(async()=>{fs.mkdirSync(shots,{recursive:true});browser=await chromium.launch({headless:true,args:['--no-sandbox']});});
after(async()=>{await browser?.close();});
async function openPage(width=1440,file='index.html',populated=false) {
  const page=await browser.newPage({viewport:{width,height:960}});
  const errors=[]; page.on('pageerror',e=>errors.push(e.message));
  await page.route('http://vpn.test/**',async route=>{
    const url=new URL(route.request().url());
    if(url.pathname.startsWith('/static/') || url.pathname.endsWith('.html')) {
      const local=path.join(root,url.pathname);
      if(fs.existsSync(local)) {
        let body=fs.readFileSync(local);
        // Only fixed presentation values; Go html/template execution is tested by integration suite.
        if(local.endsWith('.html')) body=body.toString().replaceAll('{{ .sysUser }}','演示管理员').replaceAll('{{ .server.Version }}','OpenVPN · 测试环境').replaceAll('{{ .version }}','开发预览');
        return route.fulfill({body,contentType:{'.js':'application/javascript','.css':'text/css','.html':'text/html','.json':'application/json','.woff2':'font/woff2'}[path.extname(local)]||'application/octet-stream'});
      }
    }
    const api={
      '/csrf':{csrfToken:'synthetic-layout-token'},
      '/ovpn/online-client':{server:{Address:'vpn.example.test:1194',Status:'测试数据',BytesIn:'0 B',BytesOut:'0 B',RunDate:'—'},clients:[]},
      '/ovpn/group':[], '/ovpn/group/null/users':{users:[],authUser:true},
      '/ovpn/client':[], '/ovpn/firewall':[], '/ovpn/certs':[],
      '/ovpn/history':{draw:Number(url.searchParams.get('draw')),recordsTotal:0,recordsFiltered:0,data:[]},
      '/settings':{system:{base:{},ldap:{},email:{}},client:{client_url:{},mfa_app_url:{}},openvpn:{}},
      '/client/userinfo':{username:'演示用户'},
    };
    if(populated) {
      const users=[{id:1,gid:1,username:'demo.engineering',ipAddr:'10.8.0.20',ovpnConfig:'engineering.ovpn',mfaEnabled:true,isEnable:true,name:'演示账号',email:'demo@example.test',expireDate:'2030-01-01',createdAt:'2026-01-01',updatedAt:'2026-01-01'}];
      api['/ovpn/group']=[{id:1,parent_id:null,name:'演示组织（合成数据）'}];
      api['/ovpn/group/null/users']={users,authUser:true};
      api['/ovpn/group/1']={name:'演示组织（合成数据）'};
    }
    return route.fulfill({json:api[url.pathname]||{}});
  });
  await page.goto('http://vpn.test/'+file);
  await page.waitForFunction(()=>window.request);
  if(file.startsWith('index')) await page.waitForFunction(()=>window.vtable && document.querySelector('#vtable tbody'));
  return {page,errors};
}
async function noOverflow(page){assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'body must not scroll horizontally');}
test('desktop direct navigation has persistent section context and keyboard reachable actions',async()=>{
  const {page,errors}=await openPage();
  await page.waitForSelector('#consoleTitle');
  assert.equal(await page.locator('html').getAttribute('lang'),'zh-CN');
  assert.equal(await page.locator('#consoleTitle').textContent(),'在线状态');
  for(const [id,title,tab] of [['showUser','VPN 账号','user'],['showClient','客户端','client'],['showFirewall','防火墙','firewall'],['showHistory','历史记录','history'],['manageCert','证书管理','cert'],['showStatus','在线状态','status']]){
    const item=page.locator('#'+id);
    assert.equal(await item.evaluate(el=>el.tagName),'BUTTON');
    await item.focus(); await page.keyboard.press('Enter');
    await page.waitForFunction(value=>document.querySelector('#consoleTitle').textContent===value,title);
    assert.equal(await item.getAttribute('aria-current'),'page');
    assert.equal(new URL(page.url()).searchParams.get('tab'),tab);
    assert.ok((await page.locator('#consoleDescription').textContent()).length>8);
    await noOverflow(page);
  }
  assert.ok((await page.locator('#offcanvasNavbar').boundingBox()).width<300);
  await page.screenshot({path:path.join(shots,'desktop-status.png'),fullPage:true,animations:'disabled'});
  await page.locator('#settings').click(); await page.waitForSelector('#settingsModal.show');
  await page.screenshot({path:path.join(shots,'desktop-settings.png'),fullPage:true,animations:'disabled'});
  assert.deepEqual(errors,[]);await page.close();
});
test('populated account tables remain readable at phone, tablet and desktop widths',async()=>{
  for(const width of [320,768,1024,1440]) {
    const {page,errors}=await openPage(width,'index.html?tab=user',true);
    await page.waitForSelector('#vtable #showUserOffcanvas');
    await noOverflow(page);
    await page.screenshot({path:path.join(shots,`accounts-${width}.png`),fullPage:true,animations:'disabled'});
    await page.locator('#vtable #showUserOffcanvas').click();await page.waitForSelector('#userOffcanvas.show');
    assert.ok((await page.locator('#userOffcanvas').textContent()).includes('demo.engineering'));
    await noOverflow(page);
    assert.deepEqual(errors,[]);await page.close();
  }
});
test('login and client portal share the Chinese theme with accessible mobile inputs',async()=>{
  for(const file of ['login.html','client.html']) {
    const {page,errors}=await openPage(390,file);
    assert.equal(await page.locator('html').getAttribute('lang'),'zh-CN');
    assert.ok(await page.locator('body').evaluate(el=>el.classList.contains('portal-body')));
    if(file==='login.html') {
      assert.ok(await page.getByLabel('账号',{exact:true}).isVisible());
      assert.ok(await page.getByLabel('密码',{exact:true}).isVisible());
      assert.equal(await page.locator('#login button[type=submit]').evaluate(el=>getComputedStyle(el).backgroundColor),'rgb(15, 118, 110)');
    }
    await noOverflow(page);
    await page.screenshot({path:path.join(shots,file.replace('.html','-mobile.png')),fullPage:true,animations:'disabled'});
    assert.deepEqual(errors,[]);await page.close();
  }
});
test('account forms and settings use consistent padded surfaces and mobile controls',async()=>{
  for(const width of [1440,390,320]) {
    const {page}=await openPage(width,'index.html?tab=user');
    await page.getByRole('button',{name:'添加',exact:true}).click();
    await page.waitForSelector('#addUserModal.show');
    const padding=await page.locator('#addUserModal .modal-body').evaluate(el=>parseFloat(getComputedStyle(el).paddingLeft));
    assert.equal(padding,24);
    const input=page.locator('#addUserModal input[name=username]');
    assert.ok((await input.boundingBox()).height >= (width >= 992 ? 36 : 44));
    assert.ok(await page.locator('#addUserModal .modal-content').evaluate(el=>el.scrollWidth<=el.clientWidth));
    await noOverflow(page);
    await page.screenshot({path:path.join(shots,`account-form-${width}.png`),fullPage:true,animations:'disabled'});
    await page.locator('#addUserModal .btn-close').click();
    await page.waitForFunction(()=>!document.querySelector('#addUserModal').classList.contains('show'));
    if(width<992){await page.getByRole('button',{name:'打开导航',exact:true}).click();await page.waitForSelector('#offcanvasNavbar.show');}
    await page.locator('#settings').click();await page.waitForSelector('#settingsModal.show');
    assert.equal(await page.locator('#pills-base-tab').evaluate(el=>getComputedStyle(el).backgroundColor),'rgb(15, 118, 110)');
    await page.close();
  }
});
test('browser history and direct links keep the workspace and active navigation in sync',async()=>{
  const {page}=await openPage(1440,'index.html?tab=cert');
  assert.equal(await page.locator('#consoleTitle').textContent(),'证书管理');
  await page.locator('#showUser').click(); await page.locator('#showHistory').click();
  await page.goBack();
  await page.waitForFunction(()=>document.querySelector('#consoleTitle').textContent==='VPN 账号');
  assert.equal(await page.locator('#showUser').getAttribute('aria-current'),'page');
  await page.goForward();
  await page.waitForFunction(()=>document.querySelector('#consoleTitle').textContent==='历史记录');
  await page.reload();await page.waitForFunction(()=>window.vtable);
  assert.equal(await page.locator('#showHistory').getAttribute('aria-current'),'page');
  await page.close();
});
test('mobile offcanvas closes on navigation, restores focus and contains all section layouts',async()=>{
  const {page,errors}=await openPage(390);
  const toggle=page.getByRole('button',{name:'打开导航',exact:true});
  await toggle.click(); await page.waitForSelector('#offcanvasNavbar.show');
  await page.screenshot({path:path.join(shots,'mobile-navigation.png'),fullPage:true,animations:'disabled'});
  for(const [id,title] of [['showUser','VPN 账号'],['manageCert','证书管理'],['showHistory','历史记录'],['showClient','客户端'],['showFirewall','防火墙'],['showStatus','在线状态']]){
    const item=page.locator('#'+id); const box=await item.boundingBox();assert.ok(box.height>=44);
    await item.click();
    await page.waitForFunction(()=>!document.querySelector('#offcanvasNavbar').classList.contains('show'));
    await page.waitForFunction(value=>document.querySelector('#consoleTitle').textContent===value,title);
    await noOverflow(page);
    assert.equal(await page.locator('#consoleMain').evaluate(el=>el===document.activeElement),true);
    await page.screenshot({path:path.join(shots,'mobile-'+id+'.png'),fullPage:true,animations:'disabled'});
    await toggle.click(); await page.waitForSelector('#offcanvasNavbar.show');
  }
  await page.locator('#settings').click();await page.waitForSelector('#settingsModal.show');
  await page.waitForFunction(()=>!document.querySelector('#offcanvasNavbar').classList.contains('show'));
  assert.ok(await page.locator('#settingsCurrentPass').isVisible());
  await noOverflow(page);
  assert.ok(await page.locator('#settingsModal .modal-content').evaluate(el=>el.scrollWidth<=el.clientWidth));
  await page.screenshot({path:path.join(shots,'mobile-settings.png'),fullPage:true,animations:'disabled'});
  await page.locator('#settingsModal [data-bs-dismiss="modal"]').click();
  await page.waitForFunction(()=>!document.querySelector('#settingsModal').classList.contains('show'));
  await toggle.click();await page.waitForSelector('#offcanvasNavbar.show');
  await page.keyboard.press('Escape');
  await page.waitForFunction(()=>!document.querySelector('#offcanvasNavbar').classList.contains('show'));
  await page.waitForFunction(()=>document.activeElement===document.querySelector('.console-menu-toggle'));
  assert.equal(await toggle.evaluate(el=>el===document.activeElement),true);
  assert.deepEqual(errors,[]);await page.close();
});
