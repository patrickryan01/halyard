// Visits the screens in English and lists every piece of text the window shows, so the catalogs can be checked against
// what is really on screen. Usage: PLAYWRIGHT=... CHROME=... node i18n-tour.mjs HALYARDD > shown.json
const { chromium } = await import(process.env.PLAYWRIGHT || '/home/ryan/.local/share/mise/installs/playwright/latest/node_modules/playwright/index.mjs');
import { spawn } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const dir = mkdtempSync(path.join(tmpdir(), 'i18n-tour-'));
const p = spawn(process.argv[2], ['--data', dir, '--demo', '2'], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_MCP_ADDR: '127.0.0.1:0', HALYARD_NO_BROWSER: '1', HALYARD_LANG: process.env.TOUR_LANG || '' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1300, height: 900 } });
pg.on('dialog', d => d.dismiss());
const sleep = ms => new Promise(r => setTimeout(r, ms));
const seen = new Set();
const SKIP = '.mid,.from,.who,.snip,.editor,.bodyhost,.chip,.ac,.convo h1,.ttl,.att,.invtitle,textarea,pre,script,style,[data-no-i18n],.avatar,.kbd';
async function grab() {
  const got = await pg.evaluate(skip => {
    const out = [];
    const tw = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT, { acceptNode: n => n.nodeType === 1 ? (n.matches(skip) ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_SKIP) : NodeFilter.FILTER_ACCEPT });
    let n; while ((n = tw.nextNode())) { const v = n.nodeValue.trim(); if (v && /[A-Za-z]/.test(v)) out.push(v); }
    document.querySelectorAll('[title],[placeholder],[aria-label]').forEach(el => { if (el.closest(skip)) return; for (const a of ['title', 'placeholder', 'aria-label']) { const v = el.getAttribute(a); if (v && /[A-Za-z]/.test(v)) out.push(v); } });
    return out;
  }, SKIP);
  got.forEach(g => seen.add(g.replace(/\s+/g, ' ')));
}
async function palette(q) { await pg.keyboard.press('Control+k'); await pg.waitForSelector('#pq'); await pg.fill('#pq', process.env.TOUR_LANG ? await pg.evaluate(id => t(H.commands.find(c => c.id === id).title), ID[q]) : q); await sleep(150); await grab(); await pg.keyboard.press('Enter'); await sleep(500); }
const ID = { 'identities': 'identities', 'filters': 'rules', 'templates': 'templates', 'vacation': 'vacation', 'encryption keys': 'security', 'import mail': 'importExport', 'more settings': 'general', 'open contacts': 'contacts', 'add an account': 'addAccount' };
const tk = new URL(url).searchParams.get('token');
if (process.env.TOUR_LANG) await fetch(url.replace(/\/\?.*/, '/api/settings'), { method: 'POST', headers: { 'X-Token': tk, 'Content-Type': 'application/json' }, body: JSON.stringify({ language: process.env.TOUR_LANG }) });
await pg.goto(url); await pg.waitForSelector('.row'); await sleep(process.env.TOUR_LANG ? 1500 : 300); await grab();
await pg.keyboard.press('Control+k'); await sleep(200); await grab(); await pg.keyboard.press('Escape');
await pg.keyboard.press('?'); await sleep(300); await grab(); await pg.keyboard.press('Escape');
await pg.click('.row >> nth=0'); await pg.waitForSelector('.convo'); await sleep(500); await grab();
await pg.click('.toolbar [aria-label=More]').catch(() => {}); await sleep(200); await grab(); await pg.keyboard.press('Escape'); await pg.keyboard.press('u'); await sleep(300);
await pg.keyboard.press('x'); await sleep(200); await grab(); await pg.keyboard.press('x');
await pg.click('.toolbar [aria-label=More]').catch(() => {}); await sleep(200); await grab(); await pg.keyboard.press('Escape');
await pg.keyboard.press('c'); await pg.waitForSelector('.composer'); await sleep(300); await grab();
await pg.click('[data-opts]'); await pg.click('[data-later]'); await sleep(200); await grab(); await pg.keyboard.press('Escape'); await pg.click('[data-tpl]').catch(() => {}); await sleep(200); await grab();
await pg.click('.composer [data-x]'); await sleep(300);
for (const [q, extra] of [['identities', null], ['filters', 'rlnew'], ['templates', 'tpnew'], ['vacation', null], ['encryption keys', 'ky_make'], ['import mail', null], ['more settings', null], ['open contacts', 'ct_new'], ['add an account', null]]) {
  await palette(q); await sleep(500); await grab();
  if (extra) { await pg.click('#' + extra).catch(() => {}); await sleep(300); await grab(); await pg.keyboard.press('Escape'); }
  await pg.keyboard.press('Escape'); await pg.keyboard.press('Escape'); await sleep(200);
}
// the add-account screen for each kind of address
await pg.click('#settings'); await sleep(300); await grab();
await pg.click('#signinapps summary').catch(() => {}); await sleep(100); await grab();
await pg.click('#addacct'); await pg.waitForSelector('#em'); await grab();
for (const addr of ['a@gmail.com', 'a@outlook.com', 'a@lab.test']) { await pg.fill('#em', addr); await pg.click('#go'); await sleep(2500); await grab(); await pg.click('details summary').catch(() => {}); await grab(); }
await b.close(); p.kill();
console.log(JSON.stringify([...seen].sort(), null, 0));
