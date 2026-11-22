// Visits the screens in English and lists every piece of text the window shows, so the catalogs can be checked against
// what is really on screen. Usage: PLAYWRIGHT=... CHROME=... node i18n-tour.mjs HALYARDD > shown.json
const { chromium } = await import(process.env.PLAYWRIGHT || '/home/ryan/.local/share/mise/installs/playwright/latest/node_modules/playwright/index.mjs');
import { spawn } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { idle } from '../ui/test/wait.mjs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const dir = mkdtempSync(path.join(tmpdir(), 'i18n-tour-'));
const p = spawn(process.argv[2], ['--data', dir, '--demo', '2'], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_MCP_ADDR: '127.0.0.1:0', HALYARD_NO_BROWSER: '1', HALYARD_LANG: process.env.TOUR_LANG || '' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1300, height: 900 } });
pg.on('dialog', d => d.dismiss());
const seen = new Set();
const SKIP = '.mid,.from,.who,.snip,.editor,.bodyhost,.chip,.ac,.convo h1,.ttl,.att,.invtitle,textarea,pre,script,style,[data-no-i18n],.avatar,.kbd';
async function grab() {
  // wait for the translation pass to have run (the page says so), not for a fixed time
  await pg.waitForFunction(() => !(window.H && H.i18n && H.i18n.pending()), null, { timeout: 8000 });
  await pg.evaluate(() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))));
  await pg.waitForFunction(() => !(window.H && H.i18n && H.i18n.pending()), null, { timeout: 8000 });
  const got = await pg.evaluate(skip => {
    const out = [];
    const tw = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT, { acceptNode: n => n.nodeType === 1 ? (n.matches(skip) ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_SKIP) : NodeFilter.FILTER_ACCEPT });
    let n; while ((n = tw.nextNode())) { const v = n.nodeValue.trim(); if (v && /[A-Za-z]/.test(v)) out.push(v); }
    document.querySelectorAll('[title],[placeholder],[aria-label]').forEach(el => { if (el.closest(skip)) return; for (const a of ['title', 'placeholder', 'aria-label']) { const v = el.getAttribute(a); if (v && /[A-Za-z]/.test(v)) out.push(v); } });
    return out;
  }, SKIP);
  got.forEach(g => seen.add(g.replace(/\s+/g, ' ')));
}
async function palette(q) { await pg.keyboard.press('Control+k'); await pg.waitForSelector('#pq'); await pg.fill('#pq', process.env.TOUR_LANG ? await pg.evaluate(id => t(H.commands.find(c => c.id === id).title), ID[q]) : q); await idle(pg); await grab(); await pg.keyboard.press('Enter'); await idle(pg); }
const ID = { 'identities': 'identities', 'filters': 'rules', 'templates': 'templates', 'vacation': 'vacation', 'encryption keys': 'security', 'import mail': 'importExport', 'more settings': 'general', 'open contacts': 'contacts', 'add an account': 'addAccount' };
const tk = new URL(url).searchParams.get('token');
if (process.env.TOUR_LANG) await fetch(url.replace(/\/\?.*/, '/api/settings'), { method: 'POST', headers: { 'X-Token': tk, 'Content-Type': 'application/json' }, body: JSON.stringify({ language: process.env.TOUR_LANG }) });
await pg.goto(url); await pg.waitForSelector('.row'); await idle(pg); await grab();
await pg.keyboard.press('Control+k'); await idle(pg); await grab(); await pg.keyboard.press('Escape');
await pg.keyboard.press('?'); await idle(pg); await grab(); await pg.keyboard.press('Escape');
await pg.click('.row >> nth=0'); await pg.waitForSelector('.convo'); await idle(pg); await grab();
await pg.click('.toolbar [aria-label=More]').catch(() => {}); await idle(pg); await grab(); await pg.keyboard.press('Escape'); await pg.keyboard.press('u'); await idle(pg);
await pg.keyboard.press('x'); await idle(pg); await grab(); await pg.keyboard.press('x');
await pg.click('.toolbar [aria-label=More]').catch(() => {}); await idle(pg); await grab(); await pg.keyboard.press('Escape');
await pg.keyboard.press('c'); await pg.waitForSelector('.composer'); await idle(pg); await grab();
await pg.click('[data-opts]'); await pg.click('[data-later]'); await idle(pg); await grab(); await pg.keyboard.press('Escape'); await pg.click('[data-tpl]').catch(() => {}); await idle(pg); await grab();
await pg.click('.composer [data-x]'); await idle(pg);
for (const [q, extra] of [['identities', null], ['filters', 'rlnew'], ['templates', 'tpnew'], ['vacation', null], ['encryption keys', 'ky_make'], ['import mail', null], ['more settings', null], ['open contacts', 'ct_new'], ['add an account', null]]) {
  await palette(q); await idle(pg); await grab();
  if (extra) { await pg.click('#' + extra).catch(() => {}); await idle(pg); await grab(); await pg.keyboard.press('Escape'); }
  await pg.keyboard.press('Escape'); await pg.keyboard.press('Escape'); await idle(pg);
}
// the add-account screen for each kind of address
await pg.click('#settings'); await idle(pg); await grab();
await pg.click('#signinapps summary').catch(() => {}); await idle(pg); await grab();
await pg.click('#addacct'); await pg.waitForSelector('#em'); await grab();
for (const addr of ['a@gmail.com', 'a@outlook.com', 'a@lab.test']) { await pg.fill('#em', addr); await pg.click('#go'); await idle(pg); await grab(); await pg.click('details summary').catch(() => {}); await grab(); }
await b.close(); p.kill();
console.log(JSON.stringify([...seen].sort(), null, 0));
