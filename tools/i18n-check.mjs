// Proves the window is translated: tours the screens in English and in each language, and fails for any English phrase
// that still shows in a language whose catalog has a different word for it.
// Usage: PLAYWRIGHT=... CHROME=... node i18n-check.mjs HALYARDD [lang ...]
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
const daemon = process.argv[2];
const LANGS = process.argv.length > 3 ? process.argv.slice(3) : ['es', 'fr', 'it', 'de', 'nb', 'bg', 'el', 'ko', 'ja', 'vi', 'hi', 'zh-Hans', 'ar'];
const tour = (lang) => JSON.parse(execFileSync('node', ['tools/i18n-tour.mjs', daemon], { env: { ...process.env, TOUR_LANG: lang || '' }, maxBuffer: 1 << 26, timeout: 240000 }).toString());
const keys = JSON.parse(readFileSync('ui/web/i18n/keys.json', 'utf8'));
const patKeys = keys.filter(k => k.includes('{')).map(k => ({ k, re: new RegExp('^' + k.replace(/[.*+?^$()|[\]\\]/g, '\\$&').replace(/\{\w+\}/g, '.+?') + '$') }));
const pats = patKeys.map(p => p.re);
const ignorable = s => /^[\w.+-]+@[\w.-]+$/.test(s) || /^[\d:]+ ?[AP]?M?$/.test(s) || /^(Mon|Tue|Wed|Thu|Fri|Sat|Sun)/.test(s) || /^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d/.test(s) || /^(Ctrl|Shift|Alt)\+/.test(s) || s.length <= 1 || /^g then \w$/.test(s) ||
  ['Halyard', 'English', 'Deutsch', 'Español', 'Français', 'Italiano', 'Norsk bokmål', 'Tiếng Việt', 'OpenPGP', 'S/MIME', 'SSL/TLS', 'common', 'demo', 'GitHub', 'Tx', 'U', 'Enter', 'Billing', 'Newsletter', 'Payroll', 'Projects', 'Suppliers'].includes(s) || /^[A-Z][a-z]+ [A-Z][a-z]+( <.*)?$/.test(s) || s.includes('@') || s.startsWith('/');
const en = tour('').filter(s => !ignorable(s));
let failed = 0;
for (const lang of LANGS) {
  const cat = JSON.parse(readFileSync(`ui/web/i18n/${lang}.json`, 'utf8'));
  const shown = new Set(tour(lang));
  const left = [];
  for (const s of en) {
    const pk = patKeys.find(p => p.re.test(s));
    const key = cat[s] !== undefined ? s : (pk ? s : null);
    if (key === null) continue; // not a catalog phrase: a name or a figure
    if (cat[s] === undefined && pk && cat[pk.k] === pk.k) continue; // a phrase with a value in it that reads the same in that language
    const want = cat[s];
    if (want === s) continue; // the same word in that language, on purpose
    if (want !== undefined && shown.has(want)) continue; // translated
    if (want === undefined) { if (!shown.has(s)) continue; left.push(s); continue; } // a pattern phrase
    if (shown.has(s)) left.push(s);
  }
  const rtl = lang === 'ar';
  console.log(`${lang}: ${left.length ? 'FAIL ' + left.length + ' left in English: ' + left.slice(0, 8).join(' | ') : 'ok'}${rtl ? ' (right to left page checked in the browser test)' : ''}`);
  if (left.length) failed++;
}
console.log(failed ? 'I18N FAILED' : 'i18n ok');
process.exit(failed ? 1 : 0);
