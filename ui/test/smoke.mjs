// UI smoke test: drives the real UI in Chromium against a demo build of the daemon and fails on
// any page error, failed request, or missing behavior.  Usage: CHROME=... node smoke.mjs DAEMON
const { chromium } = await import(process.env.PLAYWRIGHT || '/home/ryan/.local/share/mise/installs/playwright/latest/node_modules/playwright/index.mjs');
import { spawn } from 'node:child_process';
import { idle, until } from './wait.mjs';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const dir = mkdtempSync(path.join(tmpdir(), 'ui-smoke-'));
const p = spawn(process.argv[2], ['--data', dir, '--demo', '3'], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_MCP_ADDR: '127.0.0.1:0' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1300, height: 800 } });
const errors = [];
pg.on('pageerror', e => errors.push('pageerror ' + e.message));
pg.on('response', r => { if (r.status() >= 400) errors.push('http ' + r.status() + ' ' + r.url()); });
const must = (c, m) => { if (!c) { errors.push('FAILED: ' + m); } };
await pg.goto(url); await pg.waitForSelector('.row');
must((await pg.locator('.row').count()) >= 10, 'unified inbox lists threads');
// open a thread, trim toggle, back
await pg.click('.row >> text=Q4 line 3'); await pg.waitForSelector('.convo');
must((await pg.locator('.msg').count()) === 3, 'three messages in the thread');
must(await pg.locator('iframe').count() >= 1, 'body renders in a sandboxed iframe');
must((await pg.locator('iframe').first().getAttribute('sandbox')) === 'allow-same-origin', 'iframe is sandboxed without scripts');
await pg.keyboard.press('u'); await pg.waitForSelector('.row');
// keyboard: cursor, star, search shortcut
await pg.keyboard.press('j'); await pg.keyboard.press('x');
must((await pg.locator('.row.checked').count()) === 1, 'x selects');
await pg.keyboard.press('/'); await pg.keyboard.type('priya'); await pg.keyboard.press('Enter'); await idle(pg);
must((await pg.locator('.row').count()) === 1, 'search narrows to one conversation');
// composer, draft autosave through the core
await pg.click('#compose'); await pg.waitForSelector('.composer');
await pg.fill('[data-f=to] input', 'dana@millwork.example'); await pg.keyboard.press('Enter');
await pg.fill('[data-subj]', 'smoke draft');
await pg.setInputFiles('[data-file]', { name: 'note.txt', mimeType: 'text/plain', buffer: Buffer.from('attached text') });
await until(pg, 'the draft to be saved by the core', async () => { const d = await api('drafts'); if (d.length !== 1) return false; const x = typeof d[0].data === 'string' ? JSON.parse(d[0].data) : d[0].data; return d[0].subject === 'smoke draft' && (x.attachments || []).length === 1; });
const drafts = await (await fetch(url.replace(/\/\?.*/, '/api/drafts?token=' + new URL(url).searchParams.get('token')))).json();
must(drafts.length === 1 && drafts[0].subject === 'smoke draft', 'draft was saved by the core');
const dd = drafts.length ? (typeof drafts[0].data === 'string' ? JSON.parse(drafts[0].data) : drafts[0].data) : {};
must((dd.attachments || []).length === 1 && dd.attachments[0].filename === 'note.txt', 'the draft kept its attachment');
await pg.click('[data-x]'); await idle(pg);
// Drafts view lists it
await pg.keyboard.press('Escape');
await pg.click('[data-nav="role:drafts"]'); await idle(pg);
must((await pg.locator('[data-draft]').count()) === 1, 'Drafts view lists the saved draft');
await pg.click('[data-draft]'); await pg.waitForSelector('.composer');
must((await pg.locator('.composer .cfiles .att').count()) === 1, 'reopened draft shows its attachment');
await pg.click('.composer [data-x]'); await idle(pg);
// settings shows the MCP endpoint
await pg.click('#settings'); await pg.waitForSelector('.dialog'); await idle(pg);
must((await pg.locator('#mcpbox').innerText()).includes('/mcp'), 'settings shows the MCP endpoint');
// add account: type an address, the provider is found. Gmail with no built-in client walks through an app password
await pg.click('#addacct'); await pg.waitForSelector('#em');
await pg.fill('#em', 'someone@gmail.com'); await pg.click('#go'); await pg.waitForSelector('#pw');
must((await pg.locator('#step').innerText()).includes('app password'), 'a Gmail address shows the app password steps');
must((await pg.inputValue('#ih')) === 'imap.gmail.com' && (await pg.inputValue('#sh')) === 'smtp.gmail.com', 'Gmail servers are filled in');
must(!(await pg.locator('#oauth').isVisible()), 'no sign-in app is asked for on a normal add');
await pg.fill('#em', 'someone@outlook.com'); await pg.click('#go'); await pg.waitForSelector('#oauth');
must((await pg.locator('#oauth').innerText()).includes('Microsoft') && !(await pg.locator('#cid').isVisible()), 'a Microsoft address is one click and shows no client fields');
await pg.keyboard.press('Escape'); await pg.waitForSelector('#addacct', { state: 'detached' }).catch(() => {});
await pg.click('#settings').catch(() => {}); await pg.waitForSelector('.dialog'); await idle(pg);
// a customer's own Google sign-in app is set up in the app and kept
await pg.click('#signinapps summary'); await pg.fill('#sg_id', 'smoke-id.apps.googleusercontent.com'); await pg.fill('#sg_sec', 'smoke-secret');
await pg.click('#save'); await idle(pg);
const st = await (await fetch(url.replace(/\/\?.*/, '/api/state'), { headers: { 'X-Token': new URL(url).searchParams.get('token') } })).json();
must(st.signInApps.google.clientId === 'smoke-id.apps.googleusercontent.com' && st.signInApps.google.hasSecret === true, 'the Google sign-in app is saved from Settings');
must(JSON.stringify(st).indexOf('smoke-secret') < 0, 'the saved secret never comes back out of the core');
await pg.click('#settings'); await pg.waitForSelector('.dialog'); await pg.click('#addacct'); await pg.waitForSelector('#em');
await pg.fill('#em', 'someone@gmail.com'); await pg.click('#go'); await pg.waitForSelector('#oauth'); await pg.click('#adv summary'); await pg.waitForSelector('#cid');
must((await pg.inputValue('#cid')) === 'smoke-id.apps.googleusercontent.com', 'account setup shows the saved Google client ID');
await pg.keyboard.press('Escape');
// a theme change made elsewhere (the MCP server does this) reaches the open window without a reload
const tk = new URL(url).searchParams.get('token');
await fetch(url.replace(/\/\?.*/, '/api/settings'), { method: 'POST', headers: { 'X-Token': tk, 'Content-Type': 'application/json' }, body: JSON.stringify({ theme: 'dark' }) });
await pg.waitForFunction(() => document.documentElement.dataset.theme === 'dark', null, { timeout: 8000 }).catch(() => errors.push('FAILED: the window did not follow a theme change'));
// shortcuts help
await pg.keyboard.press('?'); await pg.waitForSelector('.dialog .kmap');
must((await pg.locator('.kmap').innerText()).includes('Compose a new message'), 'the shortcut list shows the commands');
await pg.keyboard.press('Escape');
await b.close(); p.kill();
if (errors.length) { console.log(errors.join('\n')); process.exit(1); }
console.log('ui smoke ok');
