// A thread reload that is still in flight when the person leaves the thread must not break the page: it used to write the
// answer into an open thread that no longer existed. Delays the answer, leaves the thread meanwhile, and fails on any page
// error. Usage: PLAYWRIGHT=... CHROME=... node open_race.mjs DAEMON_DEMO
import { spawn } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
const { chromium } = await import(process.env.PLAYWRIGHT);
const dir = mkdtempSync(path.join(tmpdir(), 'open-race-'));
const p = spawn(process.argv[2], ['--data', dir, '--demo', '3'], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_MCP_ADDR: '127.0.0.1:0' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1300, height: 800 } });
const errors = [];
pg.on('pageerror', e => errors.push('pageerror ' + e.message));
await pg.goto(url); await pg.waitForSelector('.row');
await pg.click('.row >> text=Q4 line 3'); await pg.waitForSelector('.convo');
let held = 0;
await pg.route('**/thread?id=*', async route => { held++; await new Promise(r => setTimeout(r, 1200)); await route.continue(); });
// a reload starts (a sync ended, say), and the person goes back to the list before it answers
await pg.evaluate(() => { window.__reload = reloadOpen(); });
await pg.keyboard.press('u'); await pg.waitForSelector('.row');
await pg.evaluate(() => window.__reload);
await new Promise(r => setTimeout(r, 300));
const still = await pg.evaluate(() => S.open === null && document.querySelectorAll('.row').length > 0);
console.log((held > 0 ? 'PASS' : 'FAIL') + ' the reload was in flight while the thread was closed');
console.log((still ? 'PASS' : 'FAIL') + ' the list is still showing and no thread was reopened by the late answer');
if (held === 0) errors.push('reload never ran');
if (!still) errors.push('list lost');
for (const e of errors) console.log('FAILED: ' + e);
await b.close(); p.kill();
console.log(errors.length ? 'OPEN RACE FAILED' : 'open race ok');
process.exit(errors.length ? 1 : 0);
