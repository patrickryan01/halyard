// UI smoke test: drives the real UI in Chromium against a demo build of the daemon and fails on
// any page error, failed request, or missing behavior.  Usage: CHROME=... node smoke.mjs DAEMON
import { chromium } from '/home/ryan/.local/share/mise/installs/playwright/latest/node_modules/playwright/index.mjs';
import { spawn } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const dir = mkdtempSync(path.join(tmpdir(), 'ui-smoke-'));
const p = spawn(process.argv[2], ['--data', dir, '--demo', '3'], { env: { ...process.env, HALYARD_NO_KEYRING: '1', HALYARD_MCP_ADDR: '127.0.0.1:0' } });
let buf = '';
const url = await new Promise(r => p.stdout.on('data', d => { buf += d; const m = buf.match(/open (http:\/\/\S+)/); if (m) r(m[1]); }));
const b = await chromium.launch({ executablePath: process.env.CHROME });
const pg = await b.newPage({ viewport: { width: 1300, height: 800 } });
const errors = [];
pg.on('pageerror', e => errors.push('pageerror ' + e.message));
pg.on('response', r => { if (r.status() >= 400) errors.push('http ' + r.status() + ' ' + r.url()); });
const sleep = ms => new Promise(r => setTimeout(r, ms));
const must = (c, m) => { if (!c) { errors.push('FAILED: ' + m); } };
await pg.goto(url); await pg.waitForSelector('.row');
must((await pg.locator('.row').count()) >= 10, 'unified inbox lists threads');
// open a thread, trim toggle, back
await pg.click('.row >> text=Q4 line 3'); await pg.waitForSelector('.convo');
must((await pg.locator('.msg').count()) === 3, 'three messages in the thread');
must(await pg.locator('iframe').count() >= 1, 'body renders in a sandboxed iframe');
must((await pg.locator('iframe').first().getAttribute('sandbox')) === 'allow-same-origin', 'iframe is sandboxed without scripts');
await pg.keyboard.press('u'); await pg.waitForSelector('.row');
// keyboard: cursor, star, search shortcut
await pg.keyboard.press('j'); await pg.keyboard.press('x');
must((await pg.locator('.row.checked').count()) === 1, 'x selects');
await pg.keyboard.press('/'); await pg.keyboard.type('priya'); await pg.keyboard.press('Enter'); await sleep(500);
must((await pg.locator('.row').count()) === 1, 'search narrows to one conversation');
// composer, draft autosave through the core
await pg.click('#compose'); await pg.waitForSelector('.composer');
await pg.fill('[data-f=to] input', 'dana@millwork.example'); await pg.keyboard.press('Enter');
await pg.fill('[data-subj]', 'smoke draft');
await sleep(3800);
const drafts = await (await fetch(url.replace(/\/\?.*/, '/api/drafts?token=' + new URL(url).searchParams.get('token')))).json();
must(drafts.length === 1 && drafts[0].subject === 'smoke draft', 'draft was saved by the core');
await pg.click('[data-x]'); await sleep(300);
// Drafts view lists it
await pg.keyboard.press('Escape');
await pg.click('[data-nav="role:drafts"]'); await sleep(600);
must((await pg.locator('[data-draft]').count()) === 1, 'Drafts view lists the saved draft');
// settings shows the MCP endpoint
await pg.click('#settings'); await pg.waitForSelector('.dialog'); await sleep(500);
must((await pg.locator('#mcpbox').innerText()).includes('/mcp'), 'settings shows the MCP endpoint');
await pg.keyboard.press('Escape');
// shortcuts help
await pg.keyboard.press('?'); await pg.waitForSelector('.dialog .help');
await b.close(); p.kill();
if (errors.length) { console.log(errors.join('\n')); process.exit(1); }
console.log('ui smoke ok');
