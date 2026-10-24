// Run: NODE_PATH=<directory containing playwright> node --test tests/frontend-security.cjs
const { test, before, after } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');
const root = path.resolve(__dirname, '../src/openvpn-web/templates');
const attack = `"><img src=x onerror="window.pwned=1"><svg onload="window.pwned=1">&'`;
let browser;
before(async () => { browser = await chromium.launch({headless: true, args: ['--no-sandbox']}); });
after(async () => { await browser?.close(); });
async function pageFor(file, api = {}) {
  const page = await browser.newPage();
  const requests = [];
  await page.route('http://vpn.test/**', async route => {
    const req = route.request();
    const url = new URL(req.url());
    if (url.pathname.startsWith('/static/') || url.pathname.endsWith('.html')) {
      const local = path.join(root, url.pathname);
      if (fs.existsSync(local)) {
        const ext = path.extname(local);
        return route.fulfill({body: fs.readFileSync(local), contentType: {'.js':'application/javascript','.html':'text/html','.json':'application/json','.css':'text/css'}[ext] || 'application/octet-stream'});
      }
    }
    requests.push({path:url.pathname, method:req.method(), headers:req.headers(), body:req.postData()});
    const result = typeof api[url.pathname] === 'function' ? api[url.pathname](req) : api[url.pathname];
    return route.fulfill({json: result ?? {}});
  });
  await page.goto(`http://vpn.test/${file}`);
  await page.waitForFunction(() => window.request && window.message);
  return {page, requests};
}
const userRecord = {id:1, gid:1, username:attack, name:attack, password:'NEVER-DISPLAY', mfaSecret:'NEVER-SEED', mfaEnabled:true, ipAddr:attack, ovpnConfig:attack, email:attack, expireDate:'2030-01-01', createdAt:'2026-01-01', updatedAt:'2026-01-01', isEnable:true};
async function adminPage() {
  const result = await pageFor('index.html?tab=user', {
    '/ovpn/group/null/users':{users:[userRecord],authUser:true},
    '/ovpn/group/1/users':{users:[userRecord],authUser:true},
    '/ovpn/group':[{id:1,parent_id:null,name:attack}], '/ovpn/group/1':{name:attack},
    '/ovpn/client':[{name:attack, fullName:attack, file:'javascript:window.pwned=1',date:'2026-01-01'}],
    '/ovpn/firewall':[{id:1,sip:attack,dip:'',sg:[{name:attack}],dg:[],policy:attack,status:true,comment:attack,createdAt:'2026-01-01'}],
    '/ovpn/certs':[{name:attack,type:attack,status:attack}],
  });
  await result.page.waitForFunction(() => window.vtable && vtable.rows().count() === 1);
  return result;
}
test('user rows, details, group tree and attributes keep attacker input inert', async () => {
  const { page } = await adminPage();
  assert.equal(await page.locator('#vtable #showUserOffcanvas').textContent(), attack);
  assert.equal(await page.locator('#treeMenu .text-truncate').textContent(), attack);
  assert.equal(await page.locator('#vtable .btn-delete').getAttribute('data-delete-name'), attack);
  await page.locator('#vtable #showUserOffcanvas').click();
  await page.waitForFunction(() => document.querySelector('#userOffcanvas').classList.contains('show'));
  assert.ok((await page.locator('#userOffcanvas').textContent()).includes(attack));
  assert.ok(!(await page.locator('body').textContent()).includes('NEVER-DISPLAY'));
  assert.ok(!(await page.locator('body').textContent()).includes('NEVER-SEED'));
  assert.equal(await page.locator('#copyPass').count(), 0);
  assert.equal(await page.locator('#vtable img, #treeMenu img, #userOffcanvas img').count(), 0);
  assert.equal(await page.evaluate(() => window.pwned), undefined);
  await page.close();
});
test('client download URLs reject executable schemes and popover text is escaped twice safely', async () => {
  const { page } = await adminPage();
  await page.locator('#showClient').dispatchEvent('click');
  await page.waitForFunction(() => document.querySelector('#vtable a[download]'));
  const link = page.locator('#vtable a[download]');
  assert.equal(await link.getAttribute('download'), attack);
  assert.ok(!(await link.getAttribute('href')).startsWith('javascript:'));
  await page.locator('#vtable .btn-delete').click();
  assert.equal(await page.locator('.popover strong').textContent(), attack);
  assert.equal(await page.locator('.popover img, #vtable img').count(), 0);
  assert.equal(await page.evaluate(() => window.pwned), undefined);
  await page.close();
});
test('firewall remarks and group names plus certificate fields are literal text', async () => {
  const { page } = await adminPage();
  for (const tab of ['#showFirewall','#manageCert']) {
    await page.locator(tab).dispatchEvent('click');
    await page.waitForFunction(value => document.querySelector('#vtable tbody').textContent.includes(value), attack);
    assert.equal(await page.locator('#vtable img, #vtable svg[onload]').count(), 0);
  }
  assert.equal(await page.evaluate(() => window.pwned), undefined);
  await page.close();
});
test('CSRF token covers login/MFA, mutations, multipart and POST logout; rotates with authentication', async () => {
  let token = 'anonymous-token';
  let stage = 0;
  const {page, requests} = await pageFor('login.html', {
    '/csrf': () => ({csrfToken:token}),
    '/login': () => (++stage === 1 ? {mfaPending:true, message:'需要MFA验证'} : (token='authenticated-token', {message:'ok',user:{id:1,isFirstLogin:true},redirect:'/admin'})),
    '/logout': () => (token='logged-out-token', {redirect:'/login'}),
  });
  await page.locator('#login input[name=username]').fill('test-user');
  await page.locator('#login input[name=password]').fill('test-only-password');
  await page.locator('#login form').evaluate(form => form.requestSubmit());
  await page.waitForFunction(() => document.querySelector('#mfaValidateModal').classList.contains('show'));
  await page.locator('#mfaValidateModal input[name=passcode]').fill('123456');
  await page.locator('#mfaValidateModal form').evaluate(form => form.requestSubmit());
  await page.waitForFunction(() => document.querySelector('#modifyPassModal').classList.contains('show'));
  await page.evaluate(async () => {
    for (const method of ['post','put','patch','delete']) await request[method]('/mutation', {value:'x'});
    const form = new FormData(); form.set('file', new Blob(['csv']), 'users.csv');
    await request.post('/upload', form);
    await request.post('/logout');
  });
  const loginRequests = requests.filter(r => r.path === '/login');
  assert.equal(loginRequests.length,2);
  assert.ok(loginRequests.every(r => r.headers['x-csrf-token'] === 'anonymous-token'));
  assert.equal(new URLSearchParams(loginRequests[1].body).has('username'), false);
  const mutations = requests.filter(r => ['/mutation','/upload','/logout'].includes(r.path));
  assert.equal(mutations.length,6);
  assert.ok(mutations.every(r => r.headers['x-csrf-token'] === 'authenticated-token'));
  assert.ok(mutations.find(r=>r.path==='/upload').headers['content-type'].startsWith('multipart/form-data; boundary='));
  await page.evaluate(() => request.post('/mutation'));
  assert.equal(requests.at(-1).headers['x-csrf-token'],'logged-out-token');
  await page.close();
});
test('missing CSRF tokens fail closed without sending a mutation', async () => {
  const {page, requests} = await pageFor('client.html', {'/csrf':{}});
  const failed = await page.evaluate(async () => {try {await request.post('/mutation'); return false;} catch (_) {return true;}});
  assert.equal(failed,true);
  assert.equal(requests.filter(r=>r.path==='/mutation').length,0);
  await page.close();
});
test('MFA seed is displayed only from explicit pending enrollment, then cleared', async () => {
  let enabled = true;
  const {page, requests} = await pageFor('client.html', {
    '/csrf':{csrfToken:'test-token'},
    '/client/userinfo':{username:attack},
    '/client/mfa':req => req.method()==='GET' ? {mfaEnable:enabled,user:{id:1,username:attack,mfaSecret:'EXISTING-SEED'}} : {message:'ok'},
    '/client/mfa/enroll':{mfaSecret:'PENDING-TEST-SEED'},
  });
  await page.locator('#mfa').click();
  await page.waitForFunction(() => document.querySelector('#mfaModal').classList.contains('show'));
  assert.equal((await page.locator('#mfaModal code').textContent()).trim(),'');
  assert.equal(await page.locator('#qrcode').isVisible(),false);
  await page.locator('#mfaModal .btn-close').click();
  await page.waitForFunction(() => !document.querySelector('#mfaModal').classList.contains('show'));
  enabled = false;
  await page.locator('#mfa').click();
  await page.locator('#mfaModal input[name=currentPass]').fill('test-only-password');
  await page.locator('#startMfaEnrollment').click();
  await page.waitForFunction(() => document.querySelector('#mfaModal code').textContent==='PENDING-TEST-SEED');
  await page.locator('#mfaModal input[name=passcode]').fill('123456');
  await page.locator('#enableMfa').click();
  await page.waitForURL('http://vpn.test/login');
  assert.ok(!(await page.locator('body').textContent()).includes('PENDING-TEST-SEED'));
  const confirm = requests.find(r=>r.path==='/client/mfa' && r.method==='POST');
  const body = new URLSearchParams(confirm.body);
  assert.equal(body.has('mfaSecret'),false);
  assert.equal(body.has('id'),false);
  assert.equal(body.get('currentPass'),'test-only-password');
  assert.equal(confirm.headers['x-csrf-token'],'test-token');
  assert.equal(await page.evaluate(()=>window.pwned),undefined);
  await page.close();
});
test('settings never populate stored secrets and require current password for changes', async () => {
  const {page, requests} = await pageFor('index.html?tab=user', {
    '/csrf':{csrfToken:'test-token'}, '/ovpn/group/null/users':{users:[],authUser:true}, '/ovpn/group':[],
    '/settings': req => req.method()==='GET' ? {system:{base:{admin_password:'OLD-ADMIN'},ldap:{ldap_bind_password:'OLD-LDAP'},email:{password:'OLD-SMTP'}},client:{client_url:{},mfa_app_url:{}},openvpn:{}} : {message:'ok'},
  });
  await page.waitForFunction(()=>window.tables?.user);
  await page.locator('#settings').dispatchEvent('click');
  await page.waitForFunction(()=>document.querySelector('#settingsModal').classList.contains('show'));
  for (const selector of ['#adminPassword','#ldapBindPassword','#emailSmtpPassword']) assert.equal(await page.locator(selector).inputValue(),'');
  await page.locator('#settingsCurrentPass').fill('test-only-password');
  await page.locator('#siteUrl').fill('https://vpn.test');
  await page.locator('#siteUrl').blur();
  await page.waitForFunction(()=>document.querySelector('#message').textContent.includes('ok'));
  const mutation = requests.find(r=>r.path==='/settings' && r.method==='POST');
  assert.equal(new URLSearchParams(mutation.body).get('currentPass'),'test-only-password');
  assert.equal(mutation.headers['x-csrf-token'],'test-token');
  await page.close();
});
test('group/options and LDAP settings preserve literal attribute values with TLS warnings', async () => {
  const {page} = await adminPage();
  await page.locator('#vtable #editUser').dispatchEvent('click');
  await page.waitForFunction(()=>document.querySelector('#editUserModal select[name=ovpnConfig] option'));
  assert.equal(await page.locator('#editUserModal option').first().getAttribute('value'),attack);
  assert.equal(await page.locator('#editUserModal option').first().textContent(),attack);
  assert.equal(await page.locator('#editUserModal .group-select-item').first().getAttribute('data-name'),attack);
  assert.equal(await page.locator('#editUserModal img').count(),0);
  assert.ok((await page.locator('#ldapTlsWarning').textContent()).includes('StartTLS'));
  assert.ok((await page.locator('#ldapTlsWarning').textContent()).includes('LDAP_TLS_INSECURE_SKIP_VERIFY'));
  await page.close();
});
test('administrator credential settings route to login, never GET logout', async () => {
  const {page,requests} = await pageFor('index.html?tab=user', {
    '/csrf':{csrfToken:'test-token'}, '/ovpn/group/null/users':{users:[],authUser:true}, '/ovpn/group':[],
    '/settings':req=>req.method()==='GET'?{system:{base:{admin_username:'old-name'},ldap:{},email:{}},client:{client_url:{},mfa_app_url:{}},openvpn:{}}:{message:'ok'},
  });
  page.on('dialog',dialog=>dialog.accept());
  await page.waitForFunction(()=>window.tables?.user);
  await page.locator('#settings').dispatchEvent('click');
  await page.waitForFunction(()=>document.querySelector('#settingsModal').classList.contains('show'));
  await page.locator('#settingsCurrentPass').fill('test-only-password');
  await page.locator('#adminUsername').fill('changed-name');
  await page.locator('#adminUsername').blur();
  await page.waitForURL('http://vpn.test/login');
  assert.equal(requests.filter(r=>r.path==='/logout' && r.method==='GET').length,0);
  await page.close();
});
test('server messages are literal text, not executable markup', async () => {
  const { page } = await pageFor('client.html');
  await page.evaluate(value => message.error(value, 0), attack);
  assert.equal(await page.locator('#message .message-content > span:last-child').textContent(), attack);
  assert.equal(await page.locator('#message img, #message svg[onload]').count(), 0);
  assert.equal(await page.evaluate(() => window.pwned), undefined);
  await page.close();
});
