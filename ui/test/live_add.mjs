// Live add-account proof, through the real UI against real servers. Usage:
//   HALYARD_NO_BROWSER=1 PLAYWRIGHT=... CHROME=... node live_add.mjs HALYARDD   (the lab up: lab/stalwart.sh up; the Microsoft step reads the sign-in password from stdin)
import { spawn, execFileSync } from 'node:child_process';
import { idle, until } from './wait.mjs';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
const { chromium } = await import(process.env.PLAYWRIGHT);
const msPw = readFileSync(0, 'utf8').trim();
const msUser = process.env.HALYARD_LIVE_MS_USER || '';
const dir = mkdtempSync(path.join(tmpdir(), 'live-add-'));
const p = spawn(process.argv[2], ['--data', dir], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_NO_BROWSER: '1', HALYARD_MCP_ADDR: '127.0.0.1:0' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const tk = new URL(url).searchParams.get('token'), base = url.replace(/\/\?.*/, '');
const state = async () => (await fetch(base + '/api/state', { headers: { 'X-Token': tk } })).json();
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1300, height: 800 } });
const errors = [];
pg.on('pageerror', e => errors.push('pageerror ' + e.message));
const must = (c, m) => { console.log((c ? 'PASS ' : 'FAIL ') + m); if (!c) errors.push(m); };
const openAdd = async () => { await pg.click('#settings'); await pg.waitForSelector('.dialog'); await pg.click('#addacct'); await pg.waitForSelector('#em'); };
await pg.goto(url); await pg.waitForSelector('#em');

// 1. an IMAP lab account: the address goes in, no provider can be found, the server fields are filled by hand
await pg.fill('#em', 'alice@lab.test'); await pg.click('#go'); await pg.waitForSelector('#pw');
await pg.fill('#pw', 'labpass-alice'); await pg.fill('#ih', '127.0.0.1'); await pg.fill('#ip', '18993'); await pg.fill('#sh', '127.0.0.1'); await pg.fill('#sp', '18465'); await pg.selectOption('#ss', 'ssl');
await pg.check('#sk'); await pg.click('#add');
await until(pg, 'the IMAP account to appear', async () => (await api('state')).accounts.some(a => a.email === 'alice@lab.test'));
let st = await state();
must(st.accounts.some(a => a.email === 'alice@lab.test' && a.kind === 'imap'), 'IMAP lab account added through the add-account screen');

// 2. the Gmail path with an app password, pointed at the lab: Google shows the password in groups of four, so spaces are typed
await openAdd(); await pg.fill('#em', 'gmailstyle@gmail.com'); await pg.click('#go'); await pg.waitForSelector('#pw');
must((await pg.locator('#step').innerText()).includes('app password'), 'a Gmail address shows the app password steps');
must((await pg.inputValue('#ih')) === 'imap.gmail.com', 'Gmail IMAP server was filled in');
await pg.evaluate(() => { document.querySelectorAll('#step details').forEach(d => { if (d.querySelector('#ih, #jurl')) d.open = true; }); });
await pg.fill('#pw', 'qwfp gjlu yars tdhn'); await pg.fill('#ih', '127.0.0.1'); await pg.fill('#ip', '18993'); await pg.fill('#sh', '127.0.0.1'); await pg.fill('#sp', '18465');
await pg.fill('#un', 'gmailstyle@lab.test'); await pg.check('#sk'); await pg.click('#add');
await until(pg, 'the Gmail style account to appear', async () => (await api('state')).accounts.some(a => a.email === 'gmailstyle@gmail.com')); st = await state();
must(st.accounts.some(a => a.email === 'gmailstyle@gmail.com'), 'password login with a spaced app password works against the lab');

// 3. POP3: bob is added over POP3 after alice mails him
execFileSync('python3', ['-c', `import smtplib,ssl;from email.message import EmailMessage as E
m=E();m['From']='alice@lab.test';m['To']='bob@lab.test';m['Subject']='pop3 proof';m.set_content('hello over pop3')
c=ssl._create_unverified_context();s=smtplib.SMTP_SSL('127.0.0.1',18465,context=c);s.login('alice@lab.test','labpass-alice');s.send_message(m);s.quit()`]);
await pg.keyboard.press('Escape'); await openAdd().catch(() => {});
await pg.fill('#em', 'bob@lab.test'); await pg.click('#go'); await pg.waitForSelector('#pw');
await pg.fill('#pw', 'labpass-bob'); await pg.evaluate(() => { document.querySelectorAll('#step details').forEach(d => { if (d.querySelector('#ih, #jurl')) d.open = true; }); }); await pg.selectOption('#pr', 'pop3');
await pg.fill('#ih', '127.0.0.1'); await pg.fill('#ip', '18995'); await pg.selectOption('#is', 'ssl'); await pg.fill('#sh', '127.0.0.1'); await pg.fill('#sp', '18465'); await pg.selectOption('#ss', 'ssl'); await pg.check('#sk'); await pg.click('#add');
await until(pg, 'the POP3 account to appear', async () => (await api('state')).accounts.some(a => a.email === 'bob@lab.test'), null, 60000); st = await state();
const bob = st.accounts.find(a => a.email === 'bob@lab.test');
must(bob && bob.kind === 'pop3', 'POP3 lab account added');
let q = null;
try { await until(pg, 'the POP3 mail to be downloaded', async (u) => JSON.stringify(await (await fetch(u.url, { headers: { 'X-Token': u.tk } })).json()).includes('pop3 proof'), { url: base + '/api/threads?q=' + encodeURIComponent('pop3 proof'), tk }, 60000); q = 'pop3 proof'; } catch (e) { console.log(String(e)); }
must(q === 'pop3 proof', 'the mail was downloaded over POP3');

// 4. Microsoft: one click, a real sign-in on login.microsoftonline.com, the page is driven here because no browser is opened
if (msPw && msUser) {
  await pg.keyboard.press('Escape'); await openAdd().catch(() => {});
  const started = pg.waitForResponse(r => r.url().includes('/api/accounts/oauth/start'));
  await pg.fill('#em', msUser); await pg.click('#go'); await pg.waitForSelector('#oauth');
  must(!(await pg.locator('#cid').isVisible()), 'Microsoft shows one button and no client fields');
  await pg.click('#oauth');
  const { url: authUrl } = await (await started).json();
  const mp = await b.newPage();
  await mp.goto(authUrl);
  await mp.fill('input[name=loginfmt]', msUser); await mp.click('#idSIButton9');
  await mp.waitForSelector('input[name=passwd]', { state: 'visible' }); await mp.waitForFunction(() => document.querySelector('input[name=passwd]') && !document.querySelector('input[name=passwd]').disabled);
  await mp.fill('input[name=passwd]', msPw); await mp.click('#idSIButton9');
  for (let i = 0; i < 20 && !/localhost|127\.0\.0\.1/.test(mp.url()); i++) {
    await mp.waitForLoadState('domcontentloaded').catch(() => {});
    for (const sel of ['#idSIButton9', '#idBtn_Back']) { if (/localhost|127\.0\.0\.1/.test(mp.url())) break; const l = mp.locator(sel); if (await l.count() && await l.first().isVisible().catch(() => false)) { await l.first().click().catch(() => {}); break; } }
  }
  await until(pg, 'the Microsoft account to appear', async () => (await api('state')).accounts.some(a => a.kind === 'microsoft'), null, 90000); st = await state();
  must(st.accounts.some(a => a.kind === 'microsoft' && a.email.toLowerCase() === msUser.toLowerCase()), 'Microsoft account added with one click and a real sign-in');
}
await b.close(); p.kill();
console.log(errors.length ? 'LIVE ADD FAILED' : 'live add ok');
process.exit(errors.length ? 1 : 0);
