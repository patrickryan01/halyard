// The search box keeps what the person has typed, and its focus, when the page refreshes behind it (a sync ends, the status
// poll answers). Types part of a word, makes the page refresh, types the rest and searches.
// Usage: PLAYWRIGHT=... CHROME=... node search_keep.mjs HALYARDD_DEMO
import { spawn } from 'node:child_process';
import { idle } from './wait.mjs';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
const { chromium } = await import(process.env.PLAYWRIGHT);
const dir = mkdtempSync(path.join(tmpdir(), 'search-keep-'));
const p = spawn(process.argv[2], ['--data', dir, '--demo', '3'], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_MCP_ADDR: '127.0.0.1:0' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1300, height: 800 } });
const errors = [];
pg.on('pageerror', e => errors.push('pageerror ' + e.message));
await pg.goto(url); await pg.waitForSelector('.row'); await idle(pg);
await pg.keyboard.press('/'); await pg.keyboard.type('pri');
await pg.evaluate(() => { refreshSoon(); });
await idle(pg);
const mid = await pg.evaluate(() => ({ v: document.querySelector('#q').value, focus: document.activeElement === document.querySelector('#q') }));
console.log((mid.v === 'pri' && mid.focus ? 'PASS' : 'FAIL') + ' the search box kept its text and focus across a refresh: ' + JSON.stringify(mid));
if (!(mid.v === 'pri' && mid.focus)) errors.push('search box was rebuilt');
await pg.keyboard.type('ya'); await pg.keyboard.press('Enter'); await idle(pg);
const rows = await pg.locator('.row').count();
console.log((rows === 1 ? 'PASS' : 'FAIL') + ' the whole word searched, one conversation: ' + rows);
if (rows !== 1) errors.push('search did not narrow');
for (const e of errors) console.log('FAILED: ' + e);
await b.close(); p.kill();
console.log(errors.length ? 'SEARCH KEEP FAILED' : 'search keep ok');
process.exit(errors.length ? 1 : 0);
