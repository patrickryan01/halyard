// Renders the real UI (served by a demo build of the daemon, with made up mail) into PNGs.
// Usage: node render.mjs DAEMON_BINARY OUTDIR LOGOSDIR
import { chromium } from '/home/ryan/.local/share/mise/installs/playwright/latest/node_modules/playwright/index.mjs';
import { spawn } from 'node:child_process';
import { mkdtempSync, mkdirSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const [bin, out, logos] = process.argv.slice(2);
mkdirSync(out, { recursive: true });

function start(demo) {
  const dir = mkdtempSync(path.join(tmpdir(), 'mailrender-'));
  const p = spawn(bin, ['--data', dir, '--demo', String(demo)], { env: { ...process.env, MAILCLIENT_NO_KEYRING: '1' } });
  return new Promise((res, rej) => {
    let buf = '';
    p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/[^\s]+)/); if (m) res({ url: m[1], p }); });
    p.stderr.on('data', d => { buf += d; });
    p.on('exit', c => rej(new Error('daemon exited ' + c + ' ' + buf)));
  });
}
const sleep = ms => new Promise(r => setTimeout(r, ms));
const SIZES = { desktop: { width: 1360, height: 860 }, narrow: { width: 420, height: 820 } };

async function nav(p, size, sel) { if (size === 'narrow') await p.click('#menu'); await p.click(sel); await sleep(600); if (size === 'narrow') await p.click('#menu', { force: true }); await sleep(300); }
async function shot(browser, url, theme, size, brand, name, act, opts = {}) {
  const ctx = await browser.newContext({ viewport: SIZES[size], deviceScaleFactor: 2, colorScheme: theme });
  const page = await ctx.newPage();
  const markFile = path.join(logos, brand, 'mark-mono.svg');
  await page.route('**/mark.svg', r => r.fulfill({ contentType: 'image/svg+xml', body: readFileSync(markFile) }));
  await page.route('**/favicon.svg', r => r.fulfill({ contentType: 'image/svg+xml', body: readFileSync(path.join(logos, brand, 'favicon.svg')) }));
  await page.addInitScript(([t, b]) => { document.addEventListener('DOMContentLoaded', () => { document.documentElement.dataset.theme = t; document.documentElement.dataset.brand = b; }); }, [theme, brand]);
  await page.goto(url);
  await page.waitForSelector(opts.wait || '.row, .welcome', { timeout: 15000 });
  await page.evaluate(([t, b]) => { document.documentElement.dataset.theme = t; document.documentElement.dataset.brand = b; }, [theme, brand]);
  await sleep(300);
  if (act) await act(page);
  await sleep(500);
  await page.screenshot({ path: path.join(out, `${name}-${theme}-${size}.png`) });
  await ctx.close();
}

const main = await start(3);
const empty = await start(0);
const browser = await chromium.launch({ executablePath: process.env.CHROME });
try {
  for (const theme of ['light', 'dark']) {
    for (const size of ['desktop', 'narrow']) {
      const b = 'halyard';
      await shot(browser, main.url, theme, size, b, '1-inbox-threads', async p => { await nav(p, size, '[data-nav="role:inbox"][data-acct="g1"]'); });
      await shot(browser, main.url, theme, size, b, '2-open-thread', async p => { await nav(p, size, '[data-nav="role:inbox"][data-acct="g1"]'); await p.click('.row >> text=Q4 line 3'); await p.waitForSelector('.convo'); await sleep(900); });
      await shot(browser, main.url, theme, size, b, '3-composer', async p => {
        await p.click(size === 'narrow' ? '#fab' : '#compose'); await p.waitForSelector('.composer');
        await p.fill('[data-f=to] input', 'dana@millwork.example'); await p.keyboard.press('Enter');
        await p.fill('[data-subj]', 'Saturday window for the line 3 retrofit');
        await p.evaluate(() => { document.querySelector('.editor').innerHTML = '<p>Dana,</p><p>Saturday is confirmed. Maintenance will cover the <b>6 hour window</b>. Please send the lockout checklist ahead of time.</p><p>Thanks,<br>Alex</p>'; });
        await p.click('.editor');
      });
      await shot(browser, main.url, theme, size, b, '4-search-results', async p => { await p.fill('#q', 'invoice'); await p.keyboard.press('Enter'); await sleep(600); });
      await shot(browser, main.url, theme, size, b, '5-unified-inbox', async () => {});
      await shot(browser, empty.url, theme, size, b, '6-account-setup', async p => { await p.fill('#em', 'alex@riverafamily.example'); await p.click('[data-p=other]'); await p.waitForSelector('#add'); await p.fill('#ih', 'imap.riverafamily.example'); await p.fill('#sh', 'smtp.riverafamily.example'); }, { wait: '.welcome' });
      await shot(browser, main.url, theme, size, b, '7-settings', async p => { await p.click('#settings'); await p.waitForSelector('.dialog'); });
    }
  }
} finally {
  await browser.close(); main.p.kill(); empty.p.kill();
}
console.log('done');
