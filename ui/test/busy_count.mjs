// The page's count of unfinished work (window.__busy, what idle() waits on) comes back to exactly zero. A refresh asked for while
// another refresh was still running used to take one count and give two back, leaving the count at -1 so the page never looked idle.
// Slows the state answer, asks for a second refresh during the first, and reads the count when everything has settled.
// Usage: PLAYWRIGHT=... CHROME=... node busy_count.mjs HALYARDD_DEMO
import { spawn } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
const { chromium } = await import(process.env.PLAYWRIGHT);
const dir = mkdtempSync(path.join(tmpdir(), 'busy-count-'));
const p = spawn(process.argv[2], ['--data', dir, '--demo', '3'], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_MCP_ADDR: '127.0.0.1:0' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1300, height: 800 } });
const errors = [];
pg.on('pageerror', e => errors.push('pageerror ' + e.message));
await pg.goto(url); await pg.waitForSelector('.row');
await pg.waitForFunction(() => (window.__busy || 0) === 0, null, { timeout: 20000 });
let slowed = 0;
await pg.route(/\/api\/state(\?.*)?$/, async route => { slowed++; await new Promise(r => setTimeout(r, 900)); await route.continue(); });
await pg.evaluate(() => refreshSoon());
await new Promise(r => setTimeout(r, 600)); // the first refresh is running now, waiting on the slow answer
await pg.evaluate(() => refreshSoon());
await new Promise(r => setTimeout(r, 4000)); // both have finished
const busy = await pg.evaluate(() => window.__busy);
console.log((slowed >= 2 ? 'PASS' : 'FAIL') + ' two refreshes ran, the second asked for while the first was in flight: ' + slowed);
console.log((busy === 0 ? 'PASS' : 'FAIL') + ' the count of unfinished work is zero afterwards: ' + busy);
if (slowed < 2) errors.push('the refreshes did not overlap');
if (busy !== 0) errors.push('busy count is ' + busy);
for (const e of errors) console.log('FAILED: ' + e);
await b.close(); p.kill();
console.log(errors.length ? 'BUSY COUNT FAILED' : 'busy count ok');
process.exit(errors.length ? 1 : 0);
