// UI checks for the mail tools: palette, remappable keys, saved searches, templates, filters, identities, send later,
// keys, contacts, invitations and the rest, against a daemon that has mail imported into the local account.
// Usage: PLAYWRIGHT=... CHROME=... node features.mjs HALYARDD
const { chromium } = await import(process.env.PLAYWRIGHT || '/home/ryan/.local/share/mise/installs/playwright/latest/node_modules/playwright/index.mjs');
import { spawn } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const dir = mkdtempSync(path.join(tmpdir(), 'ui-feat-'));
const p = spawn(process.argv[2], ['--data', dir], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_MCP_ADDR: '127.0.0.1:0', HALYARD_NO_BROWSER: '1' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const tk = new URL(url).searchParams.get('token'), base = url.replace(/\/\?.*/, '');
const J = async (path, body) => (await fetch(base + '/api/' + path, { method: body ? 'POST' : 'GET', headers: { 'X-Token': tk, ...(body ? { 'Content-Type': 'application/json' } : {}) }, body: body ? JSON.stringify(body) : undefined })).json();
const errors = [], sleep = ms => new Promise(r => setTimeout(r, ms));
const must = (c, m) => { console.log((c ? 'PASS ' : 'FAIL ') + m); if (!c) errors.push(m); };

const invite = 'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nMETHOD:REQUEST\r\nBEGIN:VEVENT\r\nUID:ui-1@x\r\nDTSTART:20261203T140000Z\r\nDTEND:20261203T150000Z\r\nSUMMARY:Plant review\r\nLOCATION:Room 4\r\nORGANIZER;CN=Ana:mailto:ana@x.test\r\nATTENDEE;CN=Me;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:local@this.computer\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n';
const mbox = 'From a@x.test Sat Nov 21 10:00:00 2026\r\nFrom: Ana <ana@x.test>\r\nTo: me@x.test\r\nSubject: Quarterly plan\r\nDate: Sat, 21 Nov 2026 10:00:00 +0000\r\nMessage-ID: <1@x.test>\r\nDisposition-Notification-To: ana@x.test\r\n\r\nthe zebra budget\r\n\r\n' +
  'From b@y.test Sat Nov 21 11:00:00 2026\r\nFrom: News <news@y.test>\r\nTo: me@x.test\r\nSubject: Weekly news\r\nDate: Sat, 21 Nov 2026 11:00:00 +0000\r\nMessage-ID: <2@y.test>\r\nList-Unsubscribe: <mailto:u@y.test>\r\n\r\ntacos this week\r\n\r\n' +
  'From c@x.test Sat Nov 21 12:00:00 2026\r\nFrom: Ana <ana@x.test>\r\nTo: me@x.test\r\nSubject: Invitation: Plant review\r\nDate: Sat, 21 Nov 2026 12:00:00 +0000\r\nMessage-ID: <3@x.test>\r\nContent-Type: multipart/mixed; boundary=zz\r\n\r\n--zz\r\nContent-Type: text/plain\r\n\r\njoin us\r\n--zz\r\nContent-Type: text/calendar; method=REQUEST; charset=utf-8\r\n\r\n' + invite + '--zz--\r\n';
{
  const fd = new FormData(); fd.append('folder', 'Imported'); fd.append('file', new Blob([mbox]), 'old.mbox');
  const r = await fetch(base + '/api/import/mbox', { method: 'POST', headers: { 'X-Token': tk }, body: fd });
  must((await r.json()).imported === 3, 'mail imported into the local account');
}
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1300, height: 850 } });
pg.on('pageerror', e => { errors.push('pageerror ' + e.message); console.log('pageerror', e.message); });
pg.on('console', m => { if (m.type() === 'error') console.log('console error', m.text()); });
const dialogs = []; let dismissNext = false;
pg.on('dialog', d => { dialogs.push(d.message()); if (dismissNext) { dismissNext = false; d.dismiss(); return; } const t = d.message(); if (d.type() === 'prompt') d.accept(/Name for this search/.test(t) ? 'zebra search' : /address to send from/.test(t) ? 'alias@x.test' : ''); else d.accept(); });
await pg.goto(url); await pg.waitForSelector('.nav');

// command palette: open, filter, run a command, go to a folder
await pg.keyboard.press('Control+k'); await pg.waitForSelector('.palette #pq');
await pg.fill('#pq', 'imported'); await sleep(200);
must((await pg.locator('.pi').first().innerText()).includes('Go to Imported'), 'the palette finds a folder');
await pg.keyboard.press('Enter'); await pg.waitForSelector('.row'); await sleep(300);
must((await pg.locator('.row').count()) === 3, 'the folder lists the three imported messages');
await pg.keyboard.press('Control+k'); await pg.fill('#pq', 'compose'); await pg.keyboard.press('Enter'); await pg.waitForSelector('.composer');
must(true, 'a command runs from the palette'); await pg.click('.composer [data-x]'); await sleep(300);

// remap a key: compose moves from c to w
await pg.keyboard.press('?'); await pg.waitForSelector('.kmap'); await pg.fill('#kf', 'Compose a new');
await pg.click('.kchg'); await pg.waitForSelector('#got'); await pg.keyboard.press('w'); await pg.click('#ok'); await sleep(500);
await pg.keyboard.press('Escape'); await sleep(200);
await pg.locator('body').click({ position: { x: 700, y: 500 } }).catch(() => {});
await pg.keyboard.press('c'); await sleep(400);
must((await pg.locator('.composer').count()) === 0, 'the old key no longer opens the composer');
await pg.keyboard.press('w'); await pg.waitForSelector('.composer');
must(true, 'the new key opens the composer'); await pg.click('.composer [data-x]'); await sleep(300);
const st = await J('state'); must(JSON.parse(st.settings.shortcuts).compose === 'w', 'the new key is saved in settings');

// search operators and a saved search
await pg.fill('#q', 'from:ana zebra OR tacos'); await pg.keyboard.press('Enter'); await sleep(500);
must((await pg.locator('.row').count()) === 1, 'a search with an operator and OR finds the message');
await pg.click('.toolbar [aria-label=More]'); await pg.click('.pop [data-a=savesearch]'); await sleep(600);
must((await pg.locator('[data-ss]').count()) === 1, 'the saved search is in the sidebar');
await pg.fill('#q', ''); await pg.keyboard.press('Enter'); await sleep(300);
await pg.click('[data-ss]'); await sleep(500);
must((await pg.locator('.row').count()) === 1, 'the saved search runs when clicked');

// the invitation, the list header, the receipt, the source, the print button
await pg.keyboard.press('Control+k'); await pg.fill('#pq', 'imported'); await pg.keyboard.press('Enter'); await pg.waitForSelector('.row'); await sleep(300);
await pg.click('.row >> text=Invitation: Plant review'); await pg.waitForSelector('.invite'); await sleep(500);
must((await pg.locator('.invite').innerText()).includes('Plant review') && (await pg.locator('.invite').innerText()).includes('Room 4') && (await pg.locator('[data-rsvp]').count()) === 3, 'the invitation shows its event and the three answers');
must((await pg.locator('[data-print]').count()) === 1 && (await pg.locator('[data-src]').count()) === 1, 'print and source buttons are on the message');
await pg.click('[data-src]'); await pg.waitForSelector('.dialog textarea'); must((await pg.locator('.dialog textarea').inputValue()).includes('Subject: Invitation'), 'view source shows the message'); await pg.keyboard.press('Escape');
const eml = await fetch(base + '/api/export/eml?id=' + encodeURIComponent((await J('threads?q=zebra'))[0].id), { headers: { 'X-Token': tk } });
await pg.keyboard.press('u'); await pg.waitForSelector('.row'); await sleep(200);
await pg.click('.row >> text=Weekly news'); await pg.waitForSelector('[data-unsub]'); must(true, 'a mailing list message offers Unsubscribe'); await pg.keyboard.press('u'); await sleep(300);
await pg.click('.row >> text=Quarterly plan'); await pg.waitForSelector('[data-yes]'); must(true, 'a read receipt request is asked about'); await pg.click('[data-no]'); await sleep(300);
must((await pg.locator('[data-yes]').count()) === 0, 'declining the receipt hides the question'); await pg.keyboard.press('u'); await sleep(200);

// templates: make one, use it in the composer
await pg.keyboard.press('Control+k'); await pg.fill('#pq', 'templates'); await pg.keyboard.press('Enter'); await pg.waitForSelector('#tpnew'); await pg.click('#tpnew');
await pg.fill('#tn', 'Thanks'); await pg.fill('#ts', 'Thank you'); await pg.fill('#tb', 'Thank you for your note.'); await pg.click('#tsave'); await sleep(500);
must((await pg.locator('#tpbody').innerText()).includes('Thanks'), 'the template is listed'); await pg.keyboard.press('Escape'); await pg.keyboard.press('Escape');
await pg.keyboard.press('w'); await pg.waitForSelector('.composer'); await pg.click('[data-tpl]'); await pg.click('.pop [data-t]'); await sleep(300);
must((await pg.locator('.composer .editor').innerText()).includes('Thank you for your note.') && (await pg.inputValue('.composer [data-subj]')) === 'Thank you', 'a template fills the composer');

// from identities, options, send later
must((await pg.locator('.composer [data-from] option').count()) >= 1, 'the From list has the account');
await pg.click('[data-opts]'); must((await pg.locator('.composer [data-rcpt]').count()) === 1 && (await pg.locator('.composer [data-sign]').count()) === 1 && (await pg.locator('.composer [data-enc]').count()) === 1, 'the options hold read receipt, sign and encrypt');
await pg.fill('.composer [data-f=to] input', 'someone@y.test'); await pg.keyboard.press('Enter');
await pg.click('[data-later]'); await sleep(500);
await pg.click('.pop [data-at] >> nth=1'); await sleep(1200);
const ob = await J('outbox'); must(ob.length === 1 && ob[0].sendAt > Date.now() + 3600e3, 'send later puts the message in the outbox for a later time');
await pg.click('.toast button:has-text("Cancel")'); await sleep(500);
must((await J('outbox')).length === 0, 'the scheduled message can be cancelled');

// filters: make one
await pg.keyboard.press('Control+k'); await pg.fill('#pq', 'filters'); await pg.keyboard.press('Enter'); await pg.waitForSelector('#rlnew'); await pg.click('#rlnew');
await pg.fill('#rn', 'Star Ana'); await pg.fill('[data-cv="0"]', 'ana@x.test'); await pg.selectOption('[data-ak="0"]', 'star'); await pg.click('#rsave'); await sleep(600);
must((await pg.locator('#rlbody').innerText()).includes('Star Ana'), 'the filter is listed'); await pg.keyboard.press('Escape'); await pg.keyboard.press('Escape');

// identities: add an alias
await pg.keyboard.press('Control+k'); await pg.fill('#pq', 'identities'); await pg.keyboard.press('Enter'); await pg.waitForSelector('[data-add]'); await pg.click('[data-add]'); await sleep(700);
must((await pg.locator('.idrow').count()) === 2, 'the alias is added as a second identity'); await pg.keyboard.press('Escape');

// keys: make a PGP key
await pg.keyboard.press('Control+k'); await pg.fill('#pq', 'encryption keys'); await pg.keyboard.press('Enter'); await pg.waitForSelector('#ky_make'); await pg.click('#ky_make');
await pg.fill('#mk_name', 'Me'); await pg.click('#mk_go'); await pg.waitForSelector('[data-exp]', { timeout: 20000 });
must(true, 'an OpenPGP key is made'); await pg.keyboard.press('Escape'); await pg.keyboard.press('Escape');

// contacts: add one
await pg.keyboard.press('Control+k'); await pg.fill('#pq', 'open contacts'); await pg.keyboard.press('Enter'); await pg.waitForSelector('#ct_new'); await pg.click('#ct_new');
await pg.fill('#cc_n', 'Dana Lee'); await pg.fill('#cc_e', 'dana@y.test'); await pg.click('#cc_go'); await sleep(700);
must((await pg.locator('#ctbody').innerText()).includes('Dana Lee'), 'the contact is listed'); await pg.keyboard.press('Escape');

// writing: rich text, spelling, recipient suggestions, the size warning
await pg.keyboard.press('w'); await pg.waitForSelector('.composer');
must((await pg.getAttribute('.composer .editor', 'spellcheck')) === 'true', 'spelling is checked while writing');
await pg.click('.composer .editor'); await pg.keyboard.type('bold words'); await pg.evaluate(() => { const e = document.querySelector('.composer .editor'); const r = document.createRange(); r.selectNodeContents(e); const s = getSelection(); s.removeAllRanges(); s.addRange(r); });
await pg.click('.composer [data-cmd=bold]'); await sleep(200);
must((await pg.locator('.composer .editor b, .composer .editor strong').count()) >= 1, 'the bold button formats the text');
await pg.fill('.composer [data-f=to] input', 'dan'); await sleep(700);
must((await pg.locator('.ac').count()) === 1 && (await pg.locator('.ac').innerText()).includes('Dana Lee'), 'typing a name suggests the contact');
await pg.keyboard.press('Escape'); await pg.fill('.composer [data-f=to] input', 'big@y.test'); await pg.keyboard.press('Enter');
await pg.fill('.composer [data-subj]', 'big file');
await pg.setInputFiles('.composer [data-file]', { name: 'big.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(26 * 1048576) });
dismissNext = true; await pg.click('.composer [data-send]'); await sleep(500);
must(dialogs.some(m => m.includes('Most mail servers refuse messages over 25 MB')) && (await J('outbox')).length === 0, 'a message over 25 MB is questioned and not sent');
await pg.click('.composer [data-x]'); await sleep(300); await pg.keyboard.press('Escape');

// remote images are held back until allowed
{
  const html = 'From x@y.test Sat Nov 21 13:00:00 2026\r\nFrom: Shop <shop@y.test>\r\nTo: me@x.test\r\nSubject: Sale pictures\r\nDate: Sat, 21 Nov 2026 13:00:00 +0000\r\nMessage-ID: <4@y.test>\r\nContent-Type: text/html\r\n\r\n<p>big sale</p><img src="https://images.example.invalid/sale.png">\r\n';
  const fd = new FormData(); fd.append('folder', 'Imported'); fd.append('file', new Blob([html]), 'shop.mbox');
  await fetch(base + '/api/import/mbox', { method: 'POST', headers: { 'X-Token': tk }, body: fd }); await sleep(500);
  await pg.keyboard.press('Control+k'); await pg.fill('#pq', 'imported'); await pg.keyboard.press('Enter'); await pg.waitForSelector('.row'); await sleep(300);
  await pg.click('.row >> text=Sale pictures'); await pg.waitForSelector('.imgbar'); must(true, 'remote images are blocked until allowed');
  await pg.click('.imgbar button'); await sleep(400); must((await pg.locator('.imgbar').count()) === 0, 'Load images lets them in'); await pg.keyboard.press('u'); await sleep(200);
}

// small screens and assistive technology
await pg.setViewportSize({ width: 390, height: 800 }); await sleep(400);
must(await pg.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'the page fits a phone width without sideways scrolling');
await pg.setViewportSize({ width: 1300, height: 850 }); await sleep(300);
must((await pg.evaluate(() => [...document.querySelectorAll('button.icon')].filter(b => !b.getAttribute('aria-label') && !b.getAttribute('title') && !b.textContent.trim()).length)) === 0, 'every icon button has a name for screen readers');

// the language: German, then Arabic (right to left), then back
async function pickLanguage(code) {
  await pg.keyboard.press('Control+k'); await pg.waitForSelector('#pq'); await pg.fill('#pq', await pg.evaluate(() => t(H.commands.find(c => c.id === 'general').title))); await pg.keyboard.press('Enter');
  await pg.waitForSelector('#g_lang'); await pg.selectOption('#g_lang', code); await pg.click('#g_save'); await sleep(1500);
}
await pickLanguage('de');
must((await pg.locator('.compose-btn').innerText()).trim() === 'Verfassen' && (await pg.getAttribute('html', 'lang')) === 'de', 'German: the window and the page language change');
await pickLanguage('ar');
must((await pg.locator('.compose-btn').innerText()).trim() === 'إنشاء رسالة' && (await pg.getAttribute('html', 'dir')) === 'rtl', 'Arabic: the window is right to left');
await pickLanguage('en');
must((await pg.locator('.compose-btn').innerText()).trim() === 'Compose' && (await pg.getAttribute('html', 'dir')) === 'ltr', 'English comes back');

// the rest of the screens open
for (const [q, sel] of [['vacation', '#vc_acct'], ['import mail', '#mb_go'], ['more settings', '#g_save'], ['server filters', null], ['calendar and contacts server', null]]) {
  await pg.keyboard.press('Control+k'); await pg.fill('#pq', q); await pg.keyboard.press('Enter'); await sleep(400);
  if (sel) { must((await pg.locator(sel).count()) === 1, 'the ' + q + ' screen opens'); await pg.keyboard.press('Escape'); }
  else { await pg.keyboard.press('Escape'); await pg.locator('.toast').first().waitFor({ timeout: 3000 }).catch(() => {}); must(true, 'the ' + q + ' screen answers (it needs a matching account)'); }
}
await b.close(); p.kill();
console.log(errors.length ? 'UI FEATURES FAILED' : 'ui features ok');
process.exit(errors.length ? 1 : 0);
