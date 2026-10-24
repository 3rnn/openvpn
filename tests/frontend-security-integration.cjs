// Run with Playwright installed: NODE_PATH=... node --test tests/frontend-security-integration.cjs
const {test,before,after} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const {spawn} = require('node:child_process');
const {chromium} = require('playwright');
let server, browser, origin, scratch, output = '';
before(async () => {
  const base = process.env.TMPDIR || path.join(os.homedir(),'.hermes/cache/scratch');
  scratch = fs.mkdtempSync(path.join(base,'frontend-security-integration-'));
  const source = path.join(scratch,'source');
  fs.cpSync(path.resolve(__dirname,'../src/openvpn-web'),source,{recursive:true});
  fs.copyFileSync(path.join(__dirname,'frontend-security-server.go.fixture'),path.join(source,'frontend_security_server_test.go'));
  fs.mkdirSync(path.join(scratch,'data'));
  const ready = path.join(scratch,'ready');
  server = spawn('go',['test','-run','^TestFrontendSecurityServer$','-count=1','-timeout=180s','-v','.'],{
    cwd:source, env:{...process.env,OVPN_DATA:path.join(scratch,'data'),FRONTEND_SECURITY_READY:ready,OPENVPN_UI_ADMIN_PASSWORD:'BootstrapPassword!123',OPENVPN_UI_COOKIE_SECURE:'false'},
  });
  server.stdout.on('data',chunk => output += chunk);
  server.stderr.on('data',chunk => output += chunk);
  const deadline = Date.now()+120000;
  while (!fs.existsSync(ready)) {
    if (server.exitCode !== null || Date.now()>deadline) throw new Error('Isolated backend did not start:\n'+output);
    await new Promise(resolve=>setTimeout(resolve,100));
  }
  origin = fs.readFileSync(ready,'utf8');
  browser = await chromium.launch({headless:true,args:['--no-sandbox']});
});
after(async () => {
  await browser?.close();
  if (scratch) fs.writeFileSync(path.join(scratch,'ready.stop'),'stop');
  if (server && server.exitCode===null) await new Promise(resolve=>{server.once('exit',resolve);setTimeout(()=>{server.kill('SIGTERM');resolve();},10000).unref();});
  assert.equal(server?.exitCode,0,'isolated backend test failed: '+output);
});
async function login(page,name,password) {
  await page.goto(origin+'/login');
  await page.waitForFunction(()=>window.request);
  await page.locator('#login input[name=username]').fill(name);
  await page.locator('#login input[name=password]').fill(password);
  await page.locator('#login form').evaluate(form=>form.requestSubmit());
}
function otp(secret) {
  const alphabet='ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';let bits='';
  for (const char of secret.toUpperCase().replace(/=+$/,'')) bits += alphabet.indexOf(char).toString(2).padStart(5,'0');
  const key=Buffer.from(bits.match(/.{8}/g).map(byte=>parseInt(byte,2)));
  const counter=Buffer.alloc(8);counter.writeBigUInt64BE(BigInt(Math.floor(Date.now()/30000)));
  const digest=crypto.createHmac('sha1',key).update(counter).digest();const offset=digest[19]&15;
  return ((digest.readUInt32BE(offset)&0x7fffffff)%1000000).toString().padStart(6,'0');
}
test('real backend enforces bootstrap admin change; UI submits authenticated change and returns to login',async()=>{
  const page=await browser.newPage();
  const denied=await page.request.post(origin+'/login',{form:{username:'browser-admin',password:'BootstrapPassword!123'}});
  assert.equal(denied.status(),403);
  await login(page,'browser-admin','BootstrapPassword!123');
  await page.waitForFunction(()=>document.querySelector('#modifyPassModal').classList.contains('show'));
  // The admin HTML shell may load; privileged/account actions must stay blocked.
  const blocked=await page.request.get(origin+'/client/mfa',{maxRedirects:0});
  assert.equal(blocked.status(),403);
  await page.locator('#modifyPassModal input[name=currentPass]').fill('BootstrapPassword!123');
  await page.locator('#modifyPassModal input[name=newPass]').fill('ChangedPassword!456');
  await page.locator('#modifyPassModal input[name=newPassAgain]').fill('ChangedPassword!456');
  const changed=page.waitForResponse(res=>res.url().endsWith('/client/modifyPass'));
  await page.locator('#modifyPassModal form').evaluate(form=>form.requestSubmit());
  assert.equal((await changed).status(),200);
  await page.waitForURL(origin+'/login');
  assert.notEqual((await page.request.get(origin+'/admin',{maxRedirects:0})).status(),200);
  await login(page,'browser-admin','ChangedPassword!456');
  await page.waitForURL(origin+'/admin');
  assert.equal((await page.request.get(origin+'/admin')).status(),200);
  await page.close();
});
test('real user enrollment, MFA second stage, seed redaction, session revocation and POST logout',async()=>{
  const page=await browser.newPage();
  await login(page,'browser-user','BrowserPassword!123');
  await page.waitForURL(origin+'/');
  await page.waitForFunction(()=>window.request);
  assert.equal(await page.locator('#welcome img').count(),0);
  assert.ok((await page.locator('#welcome').textContent()).includes('<img'));
  const info=await page.evaluate(()=>request.get('/client/userinfo'));
  assert.equal('password' in info,false); assert.equal('mfaSecret' in info,false);
  await page.locator('#mfa').click();
  await page.locator('#mfaModal input[name=currentPass]').fill('BrowserPassword!123');
  await page.locator('#startMfaEnrollment').click();
  await page.waitForFunction(()=>document.querySelector('#mfaModal code').textContent.trim().length>10);
  const seed=(await page.locator('#mfaModal code').textContent()).trim();
  assert.equal('mfaSecret' in (await page.evaluate(()=>request.get('/client/mfa'))).user,false);
  await page.locator('#mfaModal input[name=passcode]').fill(otp(seed));
  const enrolled=page.waitForResponse(res=>res.url().endsWith('/client/mfa') && res.request().method()==='POST');
  await page.locator('#enableMfa').click();
  assert.equal((await enrolled).status(),200);
  await page.waitForURL(origin+'/login');
  assert.notEqual((await page.request.get(origin+'/client/userinfo',{maxRedirects:0})).status(),200);
  await login(page,'browser-user','BrowserPassword!123');
  await page.waitForFunction(()=>document.querySelector('#mfaValidateModal').classList.contains('show'));
  await page.locator('#mfaValidateModal input[name=passcode]').fill(otp(seed));
  await page.locator('#mfaValidateModal form').evaluate(form=>form.requestSubmit());
  await page.waitForURL(origin+'/');
  await page.locator('#mfa').click();
  await page.waitForFunction(()=>document.querySelector('#mfaModal').classList.contains('show'));
  assert.equal((await page.locator('#mfaModal code').textContent()).trim(),'');
  assert.equal(await page.locator('#qrcode').isVisible(),false);
  await page.locator('#mfaModal .btn-close').click();
  await page.locator('a[href="/logout"]').click();
  await page.waitForURL(origin+'/login');
  assert.notEqual((await page.request.get(origin+'/client/userinfo',{maxRedirects:0})).status(),200);
  await page.close();
});
