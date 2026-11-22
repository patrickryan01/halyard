// Several accounts at once and the Sync now button, live: three accounts on the lab server (two over IMAP, one over JMAP) in one window,
// the combined inbox, the quick switch between accounts, and Sync now with its progress and last-synced time. Optionally takes the
// README screenshots into SHOTS. Usage:
//   lab/stalwart.sh up; HALYARD_NO_KEYRING=1 PLAYWRIGHT=... CHROME=... [SHOTS=dir] node ui/test/multi_sync.mjs HALYARDD
import { spawn, execFileSync } from 'node:child_process';
import { idle, until } from './wait.mjs';
import { mkdtempSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
const { chromium } = await import(process.env.PLAYWRIGHT);
const dir = mkdtempSync(path.join(tmpdir(), 'multi-sync-'));
const shots = process.env.SHOTS || '';
if (shots) mkdirSync(shots, { recursive: true });
const p = spawn(process.argv[2], ['--data', dir], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_NO_BROWSER: '1', HALYARD_MCP_ADDR: '127.0.0.1:0' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const tk = new URL(url).searchParams.get('token'), base = url.replace(/\/\?.*/, '');
const call = async (route, body) => (await fetch(base + '/api/' + route, { method: body ? 'POST' : 'GET', headers: { 'X-Token': tk, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined })).json();
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1280, height: 800 } });
const errors = [];
pg.on('pageerror', e => errors.push('pageerror ' + e.message));
pg.on('response', r => { if (r.status() >= 400) errors.push('http ' + r.status() + ' ' + r.url()); });
const must = (c, m) => { console.log((c ? 'PASS ' : 'FAIL ') + m); if (!c) errors.push(m); };
const shot = async name => { if (shots) { await until(pg, 'the sync spinner to stop', async () => !document.querySelector('#syncbtn.spin'), null, 60000); await idle(pg); await pg.screenshot({ path: path.join(shots, name + '.png') }); } };
const send = (from, pw, to, subject, body, name) => execFileSync('python3', ['-c', `import smtplib,ssl,sys;from email.message import EmailMessage as E
m=E();m['From']='${name} <${from}>';m['To']='${to}';m['Subject']=sys.argv[1];m.set_content(sys.argv[2])
c=ssl._create_unverified_context();s=smtplib.SMTP_SSL('127.0.0.1',18465,context=c);s.login('${from}','${pw}');s.send_message(m);s.quit()`, subject, body]);
const alice = ['alice@lab.test', 'labpass-alice', 'Alice Carter'], bob = ['bob@lab.test', 'labpass-bob', 'Bob Nguyen'], gm = ['gmailstyle@lab.test', 'qwfpgjluyarstdhn', 'Gabi Moreno'];
// mail to read before any account exists: each person has a few conversations
send(...alice.slice(0, 2), bob[0], 'Press check for the Friday run', 'Bob, the press check is booked for 14:00 on Friday. Bring the revised proof and the swatch book.\n\nAlice', alice[2]);
send(...bob.slice(0, 2), alice[0], 'Re: Press check for the Friday run', 'Booked on my side too. I will bring the proof.\n\nBob', bob[2]);
send(...gm.slice(0, 2), alice[0], 'Your freight quote is ready', 'Hi Alice, the quote for the pallet to Dayton is attached to this thread. It holds for ten days.\n\nGabi', gm[2]);
send(...alice.slice(0, 2), gm[0], 'Dock times next week', 'Gabi, can you send me the dock windows for Monday to Wednesday?\n\nAlice', alice[2]);
send(...bob.slice(0, 2), gm[0], 'Samples are on the way', 'The sample box left this morning. Tracking is in the label photo.\n\nBob', bob[2]);

await pg.goto(url); await pg.waitForSelector('#em');
await shot('welcome');
// add three accounts the way the add-account screen does, against the lab
const imap = { imapHost: '127.0.0.1', imapPort: 18993, imapSecurity: 'ssl', smtpHost: '127.0.0.1', smtpPort: 18465, smtpSecurity: 'ssl', skipVerify: true };
await call('accounts/password', { kind: 'imap', email: alice[0], name: alice[2], password: alice[1], ...imap });
await call('accounts/password', { kind: 'jmap', email: bob[0], name: bob[2], password: bob[1], jmapUrl: 'https://127.0.0.1:18443', skipVerify: true });
await call('accounts/password', { kind: 'imap', email: gm[0], name: gm[2], password: gm[1], ...imap });
await pg.reload(); await pg.waitForSelector('.row', { timeout: 60000 });
await until(pg, 'all three accounts to have synced', async () => { const s = await api('syncstatus'); return !s.running && s.accounts.length === 3 && s.accounts.every(a => a.lastSync && !a.lastSync.startsWith('0001')); }, null, 90000);
await until(pg, 'the combined inbox to hold mail from all three', async () => { const t = await api('threads?folder=role:inbox&limit=50'); return JSON.stringify(t).includes('Friday run') && JSON.stringify(t).includes('freight quote') && JSON.stringify(t).includes('Samples are on the way'); }, null, 60000);
await pg.reload(); await pg.waitForSelector('.row'); await idle(pg);

// 1. several accounts at once: all three listed, one combined inbox, a quick switch to each
let st = await call('state');
must(st.accounts.length === 3 && ['imap', 'jmap'].every(k => st.accounts.some(a => a.kind === k)), 'three accounts (two IMAP, one JMAP) are signed in together');
const side = (await pg.locator('.side').innerText()).toLowerCase();
must([alice[0], bob[0], gm[0]].every(e => side.includes(e)), 'the sidebar lists every account: ' + JSON.stringify(side.slice(0, 300)));
const all = await pg.locator('.row').count();
must(all >= 4, 'the combined inbox lists conversations (' + all + ')');
const rowText = await pg.locator('.list, .content').first().innerText();
must(rowText.includes('Friday run') && rowText.includes('freight quote') && rowText.includes('Samples are on the way'), 'the combined inbox holds mail from all three accounts');
await shot('inbox');
await pg.click('.nav[data-acct="' + st.accounts.find(a => a.email === bob[0]).id + '"]'); await idle(pg);
const bobRows = await pg.locator('.content').first().innerText();
must(bobRows.includes('Friday run') && !bobRows.includes('freight quote'), 'the quick switch narrows the list to one account');
await shot('accounts');
await pg.click('.nav[data-nav="role:inbox"][data-acct=""]'); await idle(pg);
must((await pg.locator('.row').count()) === all, 'switching back restores the combined inbox');

// 2. reading
await pg.click('.row >> text=freight quote'); await pg.waitForSelector('.convo');
await until(pg, 'the message body to draw', async () => { const f = document.querySelector('.convo iframe'); return !!(f && f.contentDocument && f.contentDocument.body.innerText.includes('pallet to Dayton')); });
must(true, 'a conversation opens and its message reads');
await shot('reading');
await pg.keyboard.press('u'); await pg.waitForSelector('.row');

// 3. writing
await pg.click('#compose'); await pg.waitForSelector('.composer');
await pg.fill('[data-f=to] input', bob[0]); await pg.keyboard.press('Enter');
await pg.click('[data-subj]'); await pg.fill('[data-subj]', 'Dock windows for next week');
await pg.click('.composer .editor'); await pg.keyboard.type('Bob, the dock windows are Monday 08:00, Tuesday 10:30 and Wednesday 13:00. Tell me if any of them clash with the press check.');
await shot('compose');
await pg.click('.composer >> text=Discard'); await pg.waitForSelector('.composer', { state: 'detached' });

// 4. Sync now: progress across every account, then a last-synced time
await pg.evaluate(() => { window.__lbl = []; new MutationObserver(() => { const l = document.querySelector('#synclbl'); if (l) window.__lbl.push(l.textContent); }).observe(document.body, { childList: true, characterData: true, subtree: true }); });
const idleLbl = await pg.locator('#synclbl').innerText();
must(/Last synced/.test(idleLbl), 'before a sync the button says when mail was last brought up to date: ' + idleLbl);
await new Promise(r => setTimeout(r, 1100)); // the stored time has one second resolution
send(...gm.slice(0, 2), alice[0], 'Pickup confirmed for Thursday', 'The truck is confirmed for Thursday at 09:15.\n\nGabi', gm[2]);
const pressed = pg.waitForResponse(r => r.url().includes('/api/sync') && r.request().method() === 'POST');
await pg.click('#syncbtn');
console.log('press answered', JSON.stringify(await (await pressed).json()).slice(0, 400));
if (shots) await pg.screenshot({ path: path.join(shots, 'syncing.png') });
await until(pg, 'the sync to finish with a last-synced time', async () => { const l = document.querySelector('#synclbl').textContent; return /Last synced/.test(l) && !document.querySelector('#syncbtn').classList.contains('spin'); }, null, 60000);
const seen = await pg.evaluate(() => window.__lbl);
must(seen.some(l => /^Syncing \d of 3$/.test(l)), 'Sync now shows its progress across the three accounts: ' + JSON.stringify([...new Set(seen)]));
await until(pg, 'Sync now to bring in the mail that arrived while it was idle', async () => document.body.innerText.includes('Pickup confirmed for Thursday'), null, 60000);
const done = await pg.locator('#synclbl').innerText();
must(/^Last synced /.test(done), 'afterwards the button says when it was last synced: ' + done);
for (let i = 0; i < 100; i++) { st = await call('syncstatus'); if (!st.running) break; await new Promise(r => setTimeout(r, 200)); }
must(st.accounts.length === 3 && !st.running && st.accounts.every(a => !a.lastSync.startsWith('0001') && !a.syncing), 'every account has a last-synced time and none is still syncing');
await pg.hover('#synclbl');
await shot('sync');

await b.close(); p.kill();
console.log(errors.length ? 'MULTI SYNC FAILED: ' + errors.join('; ') : 'multi sync ok');
process.exit(errors.length ? 1 : 0);
