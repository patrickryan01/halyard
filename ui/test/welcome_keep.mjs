// The add-account form keeps what the person typed while the page refreshes behind it. Types slowly across several refreshes;
// fails if the form is rebuilt. Usage: PLAYWRIGHT=... CHROME=... node welcome_keep.mjs HALYARDD
import { spawn } from 'node:child_process';
import { until } from './wait.mjs';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
const { chromium } = await import(process.env.PLAYWRIGHT);
const dir = mkdtempSync(path.join(tmpdir(), 'welcome-keep-'));
const p = spawn(process.argv[2], ['--data', dir], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_NO_BROWSER: '1', HALYARD_MCP_ADDR: '127.0.0.1:0' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1200, height: 800 } });
const errors = [];
pg.on('pageerror', e => errors.push('pageerror ' + e.message));
await pg.goto(url); await pg.waitForSelector('#em');
await pg.evaluate(() => { document.querySelector('#em').dataset.mark = 'kept'; });
const addr = 'alice@lab.test';
for (const ch of addr) { await pg.keyboard.type(ch); await new Promise(r => setTimeout(r, 120)); } // about two seconds, many refresh windows
// the refreshes the page asks for on its own: a sync event, an accounts event, the status poll
await pg.evaluate(() => { refreshSoon(); pollSync(); });
await new Promise(r => setTimeout(r, 900));
const st = await pg.evaluate(() => ({ mark: document.querySelector('#em') && document.querySelector('#em').dataset.mark, value: document.querySelector('#em') && document.querySelector('#em').value }));
const ok = st.mark === 'kept' && st.value === addr;
console.log((ok ? 'PASS' : 'FAIL') + ' the add-account form kept its field and its text across refreshes: ' + JSON.stringify(st));
if (!ok) errors.push('form was rebuilt');
await b.close(); p.kill();
console.log(errors.length ? 'WELCOME KEEP FAILED' : 'welcome keep ok');
process.exit(errors.length ? 1 : 0);
