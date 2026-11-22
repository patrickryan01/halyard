'use strict';
/* Languages. The program is written in English; every other language is a catalog that maps an English phrase to its
   translation (ui/web/i18n/<code>.json). A phrase with a value in it is written with a name in braces, for example
   "Added {x}". After the screen is drawn, the text on it is looked up and replaced, so a new screen needs no special
   code: its phrases go in the catalogs. Names of people, mail and addresses are never looked up. */

(function () {
  const SUPPORTED = ['en', 'es', 'fr', 'it', 'de', 'nb', 'bg', 'el', 'ko', 'ja', 'vi', 'hi', 'zh-Hans', 'ar'];
  const LOCALE = { en: 'en', es: 'es', fr: 'fr', it: 'it', de: 'de', nb: 'nb', bg: 'bg', el: 'el', ko: 'ko', ja: 'ja', vi: 'vi', hi: 'hi', 'zh-Hans': 'zh-Hans', ar: 'ar' };
  const SKIP = '.mid,.from,.who,.snip,.editor,.bodyhost,.chip,.ac,.convo h1,.ttl,.att,.invtitle,.sect span:last-child,textarea,pre,script,style,[data-no-i18n],.avatar,.kbd';
  const i18n = H.i18n = { lang: 'en', locale: 'en', cat: {}, pats: [], supported: SUPPORTED };

  function resolve(l) {
    if (!l || l === 'auto') {
      const n = (navigator.languages && navigator.languages[0]) || navigator.language || 'en';
      l = n;
    }
    const low = String(l).toLowerCase();
    if (low.startsWith('zh')) return 'zh-Hans';
    if (low.startsWith('nb') || low.startsWith('no') || low.startsWith('nn')) return 'nb';
    const base = low.split('-')[0];
    return SUPPORTED.includes(base) ? base : 'en';
  }
  i18n.resolve = resolve;

  function compile(cat) {
    const pats = [];
    for (const k of Object.keys(cat)) {
      if (k.indexOf('{') < 0) continue;
      const names = [];
      const re = '^' + k.replace(/[.*+?^$()|[\]\\]/g, '\\$&').replace(/\{(\w+)\}/g, (m, n) => { names.push(n); return '(.+?)'; }) + '$';
      pats.push({ re: new RegExp(re, 's'), names, to: cat[k] });
    }
    return pats;
  }

  // t translates one phrase; vars fill the braces. Unknown phrases come back unchanged.
  i18n.t = function (s, vars) {
    if (i18n.lang === 'en' || !s) return vars ? fill(s, vars) : s;
    const hit = i18n.cat[s];
    if (hit !== undefined) return vars ? fill(hit, vars) : hit;
    for (const p of i18n.pats) {
      const m = s.match(p.re);
      if (m) { const v = {}; p.names.forEach((n, i) => v[n] = tr(m[i + 1])); return fill(p.to, v); }
    }
    return s;
  };
  const tr = x => (i18n.cat[x] !== undefined ? i18n.cat[x] : x);
  const fill = (s, v) => String(s).replace(/\{(\w+)\}/g, (m, n) => (v[n] !== undefined ? v[n] : m));
  window.t = i18n.t;

  // ---- the screen pass
  const ATTRS = ['title', 'placeholder', 'aria-label', 'alt'];
  function skipped(el) { return el.closest && el.closest(SKIP); }
  function walk(root) {
    if (i18n.lang === 'en') return;
    const tw = document.createTreeWalker(root, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT, {
      acceptNode: n => {
        if (n.nodeType === 1) return (n.matches && n.matches(SKIP)) ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_SKIP;
        return NodeFilter.FILTER_ACCEPT;
      },
    });
    const nodes = []; let n;
    while ((n = tw.nextNode())) nodes.push(n);
    for (const t of nodes) {
      const raw = t.nodeValue; const trimmed = raw.trim();
      if (!trimmed || !/[A-Za-z]/.test(trimmed)) continue;
      const out = i18n.t(trimmed);
      if (out !== trimmed) t.nodeValue = raw.replace(trimmed, out);
    }
    const els = (root.querySelectorAll ? Array.from(root.querySelectorAll('[title],[placeholder],[aria-label]')) : []);
    if (root.getAttribute) els.push(root);
    for (const el of els) {
      if (skipped(el)) continue;
      for (const a of ATTRS) {
        const v = el.getAttribute && el.getAttribute(a);
        if (v) { const o = i18n.t(v); if (o !== v) el.setAttribute(a, o); }
      }
    }
  }
  let obs = null, queued = false;
  function schedule() {
    if (queued) return; queued = true;
    requestAnimationFrame(() => { queued = false; if (obs) obs.disconnect(); try { walk(document.body); } finally { start(); } });
  }
  function start() { if (obs && i18n.lang !== 'en') obs.observe(document.body, { childList: true, subtree: true, characterData: true }); }
  obs = new MutationObserver(schedule);

  i18n.set = async function (setting) {
    const l = resolve(setting);
    i18n.locale = (l === 'en' && (!setting || setting === 'auto')) ? undefined : (LOCALE[l] || 'en');
    document.documentElement.lang = l;
    document.documentElement.dir = l === 'ar' ? 'rtl' : 'ltr';
    if (l === 'en') { i18n.lang = 'en'; i18n.cat = {}; i18n.pats = []; obs.disconnect(); if (i18n.shown) { i18n.shown = false; } return; }
    try {
      const r = await fetch('i18n/' + l + '.json');
      const cat = await r.json();
      i18n.cat = cat; i18n.pats = compile(cat); i18n.lang = l; i18n.shown = true;
    } catch (e) { i18n.lang = 'en'; i18n.cat = {}; i18n.pats = []; return; }
    schedule();
  };

  // dates and times follow the language
  for (const m of ['toLocaleString', 'toLocaleDateString', 'toLocaleTimeString']) {
    const orig = Date.prototype[m];
    Date.prototype[m] = function (l, ...rest) { return orig.call(this, (l === undefined || (Array.isArray(l) && l.length === 0)) ? i18n.locale : l, ...rest); };
  }
})();
