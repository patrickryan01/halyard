// Drives the Microsoft sign-in page for the oauthproof tool with the test user, in headless Chromium.
// Usage: CHROME=... node drive-ms.mjs OAUTHPROOF_BINARY OUTDIR
import { chromium } from '/home/ryan/.local/share/mise/installs/playwright/latest/node_modules/playwright/index.mjs';
import { spawn } from 'node:child_process';
import { readFileSync, mkdirSync } from 'node:fs';
import { homedir } from 'node:os';
import path from 'node:path';

const [bin, out] = process.argv.slice(2);
mkdirSync(out, { recursive: true });
const user = 'account@fireballindustries.onmicrosoft.com';
const pw = readFileSync(path.join(homedir(), '.config/house/pw'), 'utf8').trim();
const p = spawn(bin, ['microsoft']);
let log = '';
let url = '';
const done = new Promise((res, rej) => {
  p.stdout.on('data', d => { log += d; const m = log.match(/URL (\S+)/); if (m && !url) url = m[1]; if (/OK signed in|FAIL/.test(log)) res(); });
  p.on('exit', () => res());
});
for (let i = 0; i < 50 && !url; i++) await new Promise(r => setTimeout(r, 100));
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1000, height: 800 } });
await pg.goto(url);
await pg.fill('input[name=loginfmt]', user); await pg.click('#idSIButton9');
await pg.waitForSelector('input[name=passwd]', { timeout: 20000 });
await pg.fill('input[name=passwd]', pw); await pg.click('#idSIButton9');
await pg.screenshot({ path: path.join(out, 'ms-signin-after-password.png') });
for (let i = 0; i < 6; i++) {
  await new Promise(r => setTimeout(r, 2500));
  if (/OK signed in|FAIL/.test(log)) break;
  const stay = pg.locator('#idSIButton9, #idBtn_Back, input[type=submit][value=Yes], input[type=submit][value=Accept]');
  if (await stay.count()) { await pg.screenshot({ path: path.join(out, 'ms-signin-step' + i + '.png') }); const acc = pg.locator('input[value=Accept], #idSIButton9'); if (await acc.count()) await acc.first().click().catch(() => {}); }
}
await done;
await pg.screenshot({ path: path.join(out, 'ms-signin-final.png') });
await b.close();
console.log(log.trim().split('\n').filter(l => !l.startsWith('URL')).join('\n'));
process.exit(/OK signed in/.test(log) ? 0 : 1);
