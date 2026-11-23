// A new composer's first-field focus must not pull the cursor away from a field the person has already clicked into. The composer
// focuses its first field 30 ms after it opens; on a slow machine that fired after the person had started on the Subject line and
// their text landed in the message body instead. Opens a composer and puts the cursor in Subject in the same turn, then waits
// past the timer and types.
// Usage: PLAYWRIGHT=... CHROME=... node composer_focus.mjs HALYARDD_DEMO
import { spawn } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
const { chromium } = await import(process.env.PLAYWRIGHT);
const dir = mkdtempSync(path.join(tmpdir(), 'composer-focus-'));
const p = spawn(process.argv[2], ['--data', dir, '--demo', '3'], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_MCP_ADDR: '127.0.0.1:0' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1300, height: 800 } });
const errors = [];
pg.on('pageerror', e => errors.push('pageerror ' + e.message));
await pg.goto(url); await pg.waitForSelector('.row');
await pg.evaluate(() => { openComposer({ mode: 'new' }); document.querySelector('.composer [data-subj]').focus(); });
await new Promise(r => setTimeout(r, 300)); // well past the 30 ms timer
await pg.keyboard.type('subject first');
const st = await pg.evaluate(() => ({ subj: document.querySelector('.composer [data-subj]').value, body: document.querySelector('.composer .editor').innerText.trim().slice(0, 40) }));
const ok = st.subj === 'subject first' && !st.body.includes('subject first');
console.log((ok ? 'PASS' : 'FAIL') + ' the cursor stayed in Subject and the text went there: ' + JSON.stringify(st));
if (!ok) errors.push('focus was pulled away from the Subject field');
// and with nothing clicked yet, the first field still gets the cursor
await pg.evaluate(() => { document.querySelector('.composer [data-discard]').click(); });
await pg.evaluate(() => openComposer({ mode: 'new' }));
await new Promise(r => setTimeout(r, 300));
const first = await pg.evaluate(() => { const a = document.activeElement; return !!(a && a.closest('[data-f=to]')); });
console.log((first ? 'PASS' : 'FAIL') + ' with nothing clicked yet the To field takes the cursor');
if (!first) errors.push('the first field did not take focus');
for (const e of errors) console.log('FAILED: ' + e);
await b.close(); p.kill();
console.log(errors.length ? 'COMPOSER FOCUS FAILED' : 'composer focus ok');
process.exit(errors.length ? 1 : 0);
