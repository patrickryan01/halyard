'use strict';
/* Mail client front end. No build step: plain modules of functions over one state object.
   Everything talks to the Go core through /api; nothing here knows what a provider is. */

const TOKEN = new URLSearchParams(location.search).get('token') || (() => { try { return sessionStorage.getItem('tok') || ''; } catch (e) { return ''; } })();
try { if (TOKEN) sessionStorage.setItem('tok', TOKEN); } catch (e) {}
const BRAND = 'Halyard';

async function api(path, body, opt) {
  const o = Object.assign({ method: body === undefined ? 'GET' : 'POST', headers: {} }, opt || {});
  if (TOKEN) o.headers['X-Token'] = TOKEN;
  if (body !== undefined && !(body instanceof FormData)) { o.headers['Content-Type'] = 'application/json'; o.body = JSON.stringify(body); }
  else if (body instanceof FormData) o.body = body;
  // window.__busy counts work the page has started and not finished (requests, the debounced refresh, the suggestion
  // lookup), so a test can wait for the page to be at rest instead of for a fixed time. The long poll for events is not work.
  const counted = !path.startsWith('events');
  if (counted) window.__busy = (window.__busy || 0) + 1;
  try {
    const r = await fetch('/api/' + path, o);
    const j = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(j.error || ('request failed (' + r.status + ')'));
    return j;
  } finally { if (counted) window.__busy--; }
}
const tokQ = () => TOKEN ? '&token=' + encodeURIComponent(TOKEN) : '';

const esc = s => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const $ = (s, r) => (r || document).querySelector(s);
const $$ = (s, r) => Array.from((r || document).querySelectorAll(s));

const IC = {
  search: '<svg viewBox="0 0 24 24"><path d="M15.5 14h-.8l-.3-.3A6.5 6.5 0 1 0 14 15.5l.3.3v.8l5 5 1.5-1.5-5-5zm-6 0a4.5 4.5 0 1 1 0-9 4.5 4.5 0 0 1 0 9z"/></svg>',
  menu: '<svg viewBox="0 0 24 24"><path d="M3 18h18v-2H3v2zm0-5h18v-2H3v2zm0-7v2h18V6H3z"/></svg>',
  inbox: '<svg viewBox="0 0 24 24"><path d="M19 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V5a2 2 0 0 0-2-2zm0 12h-4a3 3 0 0 1-6 0H5V5h14v10z"/></svg>',
  star: '<svg viewBox="0 0 24 24"><path d="M12 17.3l-5.5 3.3 1.5-6.3L3 9.9l6.4-.5L12 3.5l2.6 5.9 6.4.5-5 4.4 1.5 6.3z"/></svg>',
  clock: '<svg viewBox="0 0 24 24"><path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm.5 5v5.2l4.5 2.7-.8 1.3L11 13V7h1.5z"/></svg>',
  send: '<svg viewBox="0 0 24 24"><path d="M2 21l21-9L2 3v7l15 2-15 2z"/></svg>',
  draft: '<svg viewBox="0 0 24 24"><path d="M3 17.3V21h3.7l10.9-10.9-3.7-3.7L3 17.3zM20.7 7a1 1 0 0 0 0-1.4l-2.3-2.3a1 1 0 0 0-1.4 0l-1.8 1.8 3.7 3.7L20.7 7z"/></svg>',
  mail: '<svg viewBox="0 0 24 24"><path d="M20 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2zm0 4l-8 5-8-5V6l8 5 8-5v2z"/></svg>',
  spam: '<svg viewBox="0 0 24 24"><path d="M15.7 3H8.3L3 8.3v7.4L8.3 21h7.4l5.3-5.3V8.3L15.7 3zM13 17h-2v-2h2v2zm0-4h-2V7h2v6z"/></svg>',
  trash: '<svg viewBox="0 0 24 24"><path d="M6 19a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/></svg>',
  archive: '<svg viewBox="0 0 24 24"><path d="M20.5 5.2l-1.4-1.7A1.5 1.5 0 0 0 18 3H6c-.5 0-.9.2-1.1.5L3.5 5.200A2 2 0 0 0 3 6.500V19a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6.500c0-.5-.2-.9-.5-1.300zM12 17.500L6.500 12H10v-2h4v2h3.500L12 17.500zM5.100 5l.9-1h12l.9 1H5.100z"/></svg>',
  folder: '<svg viewBox="0 0 24 24"><path d="M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2z"/></svg>',
  label: '<svg viewBox="0 0 24 24"><path d="M17.6 5.600A2 2 0 0 0 16 5L5 5.010C3.900 5.010 3 5.900 3 7v10c0 1.100.9 1.990 2 1.990L16 19a2 2 0 0 0 1.600-.8L22 12l-4.400-6.400z"/></svg>',
  read: '<svg viewBox="0 0 24 24"><path d="M21.990 8c0-.7-.3-1.300-.9-1.700L12 1 2.900 6.300C2.300 6.700 2 7.300 2 8v10a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2l-.010-10zM12 13L3.700 7.800 12 3l8.300 4.800L12 13z"/></svg>',
  back: '<svg viewBox="0 0 24 24"><path d="M20 11H7.800l5.600-5.600L12 4l-8 8 8 8 1.400-1.400L7.800 13H20v-2z"/></svg>',
  gear: '<svg viewBox="0 0 24 24"><path d="M19.400 13a7.500 7.500 0 0 0 0-2l2.100-1.600-2-3.500-2.500 1a7.400 7.400 0 0 0-1.700-1L15 3h-4l-.4 2.900c-.6.300-1.200.6-1.700 1l-2.500-1-2 3.500L6.600 11a7.500 7.500 0 0 0 0 2l-2.100 1.600 2 3.500 2.500-1c.5.400 1.100.7 1.700 1L11 21h4l.4-2.900c.6-.3 1.200-.6 1.700-1l2.500 1 2-3.500L19.400 13zM13 15.500a3.500 3.500 0 1 1 0-7 3.500 3.500 0 0 1 0 7z"/></svg>',
  clip: '<svg viewBox="0 0 24 24" style="width:14px;height:14px;fill:currentColor"><path d="M16.500 6v11.500a4 4 0 0 1-8 0V5a2.500 2.500 0 0 1 5 0v10.500a1 1 0 0 1-2 0V6H10v9.500a2.500 2.500 0 0 0 5 0V5a4 4 0 0 0-8 0v12.500a5.500 5.500 0 0 0 11 0V6h-1.500z"/></svg>',
  plus: '<svg viewBox="0 0 24 24" style="width:22px;height:22px;fill:currentColor"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>',
};
const starSvg = '<svg viewBox="0 0 24 24"><path d="M12 17.3l-5.5 3.3 1.5-6.3L3 9.9l6.4-.5L12 3.5l2.6 5.9 6.4.5-5 4.4 1.5 6.3z"/></svg>';

const ROLES = [
  ['role:inbox', 'Inbox', 'inbox'], ['starred', 'Starred', 'star'], ['snoozed', 'Snoozed', 'clock'], ['role:sent', 'Sent', 'send'],
  ['role:drafts', 'Drafts', 'draft'], ['role:archive', 'Archive', 'archive'], ['role:all', 'All Mail', 'mail'], ['role:spam', 'Spam', 'spam'], ['role:trash', 'Trash', 'trash'],
];

const S = {
  data: null, folder: 'role:inbox', account: '', q: '', threads: [], sel: new Set(), cursor: 0, open: null, expanded: new Set(),
  seq: 0, online: navigator.onLine, side: window.innerWidth > 820, loading: false, banner: '', lastAct: null, gmode: false, offset: 0, more: false, composers: [],
};

// ---------- helpers ----------
function toast(text, opts) {
  opts = opts || {};
  const el = document.createElement('div');
  el.className = 'toast' + (opts.err ? ' err' : '');
  el.innerHTML = '<span></span>';
  el.firstChild.textContent = text;
  if (opts.action) { const b = document.createElement('button'); b.textContent = opts.action.label; b.onclick = () => { opts.action.fn(); el.remove(); }; el.appendChild(b); }
  $('#toasts').appendChild(el);
  setTimeout(() => el.remove(), opts.ms || 6000);
  return el;
}
function fmtDate(ts) {
  const d = new Date(ts), n = new Date();
  if (d.toDateString() === n.toDateString()) return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  if (d.getFullYear() === n.getFullYear()) return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
  return d.toLocaleDateString([], { year: '2-digit', month: 'numeric', day: 'numeric' });
}
function fmtFull(ts) { return new Date(ts).toLocaleString([], { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' }); }
function fmtSize(n) { if (n < 1024) return n + ' B'; if (n < 1048576) return Math.round(n / 1024) + ' KB'; return (n / 1048576).toFixed(1) + ' MB'; }
const acctOf = id => (S.data.accounts || []).find(a => a.id === id);
const myEmails = () => (S.data.accounts || []).map(a => a.email.toLowerCase());
function initials(n) { const p = String(n || '?').replace(/[<>"]/g, '').trim().split(/\s+/); return ((p[0] || '?')[0] + (p.length > 1 ? p[p.length - 1][0] : '')).toUpperCase(); }
function colorFor(s) { let h = 0; for (const c of String(s)) h = (h * 31 + c.charCodeAt(0)) >>> 0; return 'hsl(' + (h % 360) + ' 45% 42%)'; }
function addrStr(a) { return a.name ? a.name + ' <' + a.email + '>' : a.email; }
function addrFmt(a) { return a.name ? '"' + a.name.replace(/"/g, '') + '" <' + a.email + '>' : a.email; }
function typing(e) { const t = e.target; return t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable); }

// ---------- data ----------
async function refreshState() {
  S.data = await api('state');
  S.data.accounts = S.data.accounts || []; S.data.folders = S.data.folders || []; S.data.outbox = S.data.outbox || [];
  applyTheme();
}
function applyTheme() {
  const t = S.data.settings.theme;
  if (t === 'light' || t === 'dark') document.documentElement.dataset.theme = t; else delete document.documentElement.dataset.theme;
  document.documentElement.dataset.density = S.data.settings.density;
}
async function loadThreads(append) {
  if (S.folder === 'role:drafts' && !S.q) { try { S.drafts = await api('drafts'); } catch (e) {} }
  if (!append) { S.offset = 0; }
  const p = new URLSearchParams();
  if (S.account) p.set('account', S.account);
  if (S.q) p.set('q', S.q); else p.set('folder', S.folder === 'starred' ? '' : S.folder);
  if (!S.q && S.folder === 'starred') p.set('q', 'is:starred');
  p.set('limit', '100'); p.set('offset', String(S.offset));
  if (S.data.settings.conversations === 'false') p.set('flat', '1');
  const t = await api('threads?' + p.toString());
  S.threads = append ? S.threads.concat(t) : t;
  S.more = t.length === 100;
  S.cursor = Math.min(S.cursor, Math.max(0, S.threads.length - 1));
  const keep = new Set(S.threads.map(x => x.id));
  S.sel = new Set([...S.sel].filter(x => keep.has(x)));
}
let refreshTimer = null;
function refreshSoon() {
  if (refreshTimer) clearTimeout(refreshTimer); else window.__busy = (window.__busy || 0) + 1;
  refreshTimer = setTimeout(async () => {
    // the pending timer is spent and its count passes to this run; a refresh asked for while this runs is a new pending one
    refreshTimer = null;
    try {
      await refreshState();
      if (!S.open) await loadThreads(false); else await reloadOpen();
      render();
    } catch (e) { /* the next event retries */ }
    window.__busy--;
  }, 250);
}

// ---------- rendering ----------
function folderCount(key, acct) {
  let n = 0;
  for (const f of S.data.folders) {
    if (acct && f.accountId !== acct) continue;
    if (key.startsWith('role:') && f.role === key.slice(5)) n += f.unread;
  }
  return n;
}
// what the search box held when a render began, put back once the new box is bound
let QKEEP = null;
function render() {
  const d = S.data;
  const app = $('#app');
  if (!d.accounts || d.accounts.length === 0) {
    // the add-account form is a screen the person is typing into: a refresh must not rebuild it and wipe what they entered
    if ($('#addhost') && app.contains($('#addhost'))) return;
    app.innerHTML = welcomeHtml(); bindWelcome(); return;
  }
  const multi = d.accounts.length > 1;
  const roles = new Set(d.folders.map(f => 'role:' + f.role));
  const nav = (key, label, icon, count, extra) => '<button class="nav ' + (S.folder === key && !S.q && S.account === (extra || '') ? 'active' : '') + '" data-nav="' + esc(key) + '" data-acct="' + esc(extra || '') + '">' + IC[icon] + '<span>' + esc(label) + '</span>' + (count ? '<span class="n">' + count + '</span>' : '') + '</button>';
  let side = '<button class="compose-btn" id="compose">' + IC.plus + 'Compose</button>';
  for (const [k, l, ic] of ROLES) {
    if (k.startsWith('role:') && !roles.has(k) && k !== 'role:inbox') continue;
    side += nav(k, l, ic, k === 'role:inbox' || k === 'role:drafts' ? folderCount(k, '') : 0);
  }
  for (const a of d.accounts) {
    const fs = d.folders.filter(f => f.accountId === a.id && f.role === 'custom');
    if (!fs.length && !multi) continue;
    side += '<div class="sect"><span class="dot" style="width:8px;height:8px;border-radius:50%;background:' + esc(a.color) + '"></span>' + esc(a.email) + '</div>';
    if (multi) side += nav('role:inbox', 'Inbox', 'inbox', folderCount('role:inbox', a.id), a.id);
    for (const f of fs) side += '<button class="nav ' + (S.folder === f.id ? 'active' : '') + '" data-nav="' + esc(f.id) + '" data-acct="">' + IC[f.isLabel ? 'label' : 'folder'] + '<span>' + esc(f.name) + '</span>' + (f.unread ? '<span class="n">' + f.unread + '</span>' : '') + '</button>';
  }
  const errAcct = d.accounts.find(a => a.lastError);
  let banner = '';
  if (!S.online) banner = '<div class="banner">You are offline. Your saved mail is here, and changes sync when you reconnect.</div>';
  else if (errAcct) banner = '<div class="banner err">' + esc(errAcct.email) + ': ' + esc(errAcct.lastError) + ' <button id="retrysync" class="btn">Retry</button></div>';
  const queued = (d.outbox || []).filter(o => o.state === 'failed');
  if (queued.length) banner += '<div class="banner err">' + queued.length + ' message(s) did not send: ' + esc(queued[0].error) + ' <button class="btn" data-retry="' + esc(queued[0].id) + '">Retry</button></div>';
  // the search box is a field the person may be typing into: a refresh keeps its text, caret and focus
  QKEEP = (() => { const q0 = $('#q'); return q0 && document.activeElement === q0 ? { v: q0.value, a: q0.selectionStart, b: q0.selectionEnd } : null; })();
  app.innerHTML =
    '<div class="top"><button class="icon" id="menu" aria-label="Menu">' + IC.menu + '</button><div class="brand"><i></i><span>' + esc(BRAND) + '</span></div>' +
    '<div class="search">' + IC.search + '<input id="q" placeholder="Search mail (from: to: subject: is:unread has:attachment before: larger:)" value="' + esc(S.q) + '" aria-label="Search mail"></div>' +
    '<div class="spacer"></div><span class="synclbl" id="synclbl" role="status" aria-live="polite"></span><button class="icon" id="syncbtn" title="Sync now" aria-label="Sync now"><svg viewBox="0 0 24 24"><path d="M17.650 6.350A8 8 0 1 0 19.730 14h-2.080A6 6 0 1 1 12 6c1.660 0 3.140.69 4.220 1.780L13 11h7V4l-2.350 2.350z"/></svg></button>' +
    '<button class="icon" id="settings" aria-label="Settings">' + IC.gear + '</button></div>' +
    '<div class="main"><nav class="side ' + (S.side ? '' : 'closed') + '" aria-label="Folders">' + side + '</nav><section class="content">' + banner + (S.open ? convoHtml() : listHtml()) + '</section></div><button class="fab" id="fab" aria-label="Compose">' + IC.plus + '</button>';
  bindMain();
  if (S.open) mountBodies();
  if (window.H && H.afterRender) H.afterRender();
}
function listTitle() {
  if (S.q) return 'Search results';
  const r = ROLES.find(r => r[0] === S.folder);
  if (r) return (S.account ? (acctOf(S.account) || {}).email + ' ' : '') + r[1];
  const f = S.data.folders.find(f => f.id === S.folder);
  return f ? f.name : 'Mail';
}
function toolbarHtml() {
  const n = S.sel.size;
  const b = (act, icon, tip) => '<button class="icon" data-bulk="' + act + '" title="' + tip + '" aria-label="' + tip + '">' + IC[icon] + '</button>';
  if (n) return '<div class="sel"><button class="icon" id="selnone" title="Clear selection" aria-label="Clear selection">' + IC.back + '</button><span class="pill" style="margin:0 8px">' + n + ' selected</span>' +
    b('archive', 'archive', 'Archive (e)') + b('spam', 'spam', 'Report spam (!)') + b('trash', 'trash', 'Delete (#)') + b('read', 'read', 'Mark as read (Shift+I)') + b('unread', 'mail', 'Mark as unread (Shift+U)') + b('snooze', 'clock', 'Snooze (b)') + b('move', 'folder', 'Move or label') + '</div>';
  return '<input type="checkbox" id="selall" aria-label="Select all" style="margin:0 12px"><span class="title">' + esc(listTitle()) + '</span><span class="pill">' + S.threads.length + (S.more ? '+' : '') + ' conversation' + (S.threads.length === 1 ? '' : 's') + '</span>';
}
function draftRows() {
  if (S.q || S.folder !== 'role:drafts' || !(S.drafts || []).length) return '';
  return S.drafts.map((d, i) => {
    let x = {};
    try { x = typeof d.data === 'string' ? JSON.parse(d.data) : d.data; } catch (e) {}
    if (x.remoteId) return '';
    return '<div class="row" data-draft="' + i + '"><span style="width:18px"></span><span class="from" style="color:var(--danger)">Draft</span><span class="mid">' + esc(x.subject || '(no subject)') + ' <span class="snip">- ' + esc((x.bodyText || '').slice(0, 120)) + '</span></span><span class="date">' + esc(fmtDate(d.updated)) + '</span></div>';
  }).join('');
}
function listHtml() {
  let rows = draftRows();
  const multi = S.data.accounts.length > 1;
  S.threads.forEach((t, i) => {
    const parts = (t.participants || []).slice(0, 3).join(', ') || '(unknown)';
    rows += '<div class="row ' + (t.unread ? 'unread' : '') + (i === S.cursor ? ' cursor' : '') + (S.sel.has(t.id) ? ' checked' : '') + '" data-i="' + i + '" role="row">' +
      '<input type="checkbox" data-chk="' + i + '" ' + (S.sel.has(t.id) ? 'checked' : '') + ' aria-label="Select conversation">' +
      '<button class="star ' + (t.starred ? 'on' : '') + '" data-star="' + i + '" aria-label="Star">' + starSvg + '</button>' +
      (multi ? '<span class="dot" title="' + esc((acctOf(t.accountId) || {}).email) + '" style="width:8px;height:8px;border-radius:50%;background:' + esc((acctOf(t.accountId) || {}).color) + ';flex:none"></span>' : '') +
      '<span class="from">' + esc(parts) + (t.count > 1 ? '<span class="cnt">' + t.count + '</span>' : '') + '</span>' +
      '<span class="mid">' + (t.snoozedUntil ? '<span class="tag">Snoozed until ' + esc(fmtDate(t.snoozedUntil)) + '</span>' : '') + esc(t.subject || '(no subject)') + ' <span class="snip">- ' + esc(t.snippet) + '</span></span>' +
      (t.hasAttachments ? '<span style="color:var(--muted)">' + IC.clip + '</span>' : '') +
      '<span class="date">' + esc(fmtDate(t.date)) + '</span>' +
      '<span class="acts"><button class="icon" data-ra="archive" data-i="' + i + '" title="Archive" aria-label="Archive">' + IC.archive + '</button><button class="icon" data-ra="trash" data-i="' + i + '" title="Delete" aria-label="Delete">' + IC.trash + '</button><button class="icon" data-ra="' + (t.unread ? 'read' : 'unread') + '" data-i="' + i + '" title="Mark read or unread" aria-label="Mark read or unread">' + IC.read + '</button><button class="icon" data-ra="snooze" data-i="' + i + '" title="Snooze" aria-label="Snooze">' + IC.clock + '</button></span></div>';
  });
  if (!rows) rows = '<div class="empty"><div><h2 style="font-weight:500">' + (S.q ? 'No messages match your search' : 'Nothing here') + '</h2><p>' + (S.q ? 'Try fewer words or another operator.' : 'This folder is empty.') + '</p></div></div>';
  else if (S.more) rows += '<div style="padding:14px;text-align:center"><button class="btn" id="more">Load more</button></div>';
  return '<div class="toolbar">' + toolbarHtml() + '</div><div class="list" id="list" tabindex="-1">' + rows + '</div>';
}
function convoHtml() {
  const o = S.open;
  const msgs = o.msgs;
  const subj = (msgs[0] && msgs[0].subject) || '(no subject)';
  const tb = (act, icon, tip) => '<button class="icon" data-cact="' + act + '" title="' + tip + '" aria-label="' + tip + '">' + IC[icon] + '</button>';
  let h = '<div class="toolbar"><button class="icon" id="back" title="Back (u)" aria-label="Back">' + IC.back + '</button>' + tb('archive', 'archive', 'Archive (e)') + tb('spam', 'spam', 'Report spam (!)') + tb('trash', 'trash', 'Delete (#)') +
    tb('unread', 'mail', 'Mark as unread (Shift+U)') + tb('snooze', 'clock', 'Snooze (b)') + tb('move', 'folder', 'Move or label') + '<span class="spacer"></span>' +
    '<button class="icon" id="newer" title="Newer (k)" aria-label="Newer">&#8593;</button><button class="icon" id="older" title="Older (j)" aria-label="Older">&#8595;</button></div>';
  h += '<div class="convo"><h1>' + esc(subj) + '</h1>';
  msgs.forEach((m, i) => {
    const open = S.expanded.has(m.id);
    const nm = m.from.name || m.from.email;
    h += '<div class="msg ' + (open ? '' : 'collapsed') + '" data-m="' + i + '">' +
      '<div class="mh" data-toggle="' + i + '"><div class="avatar" style="background:' + colorFor(m.from.email) + '">' + esc(initials(nm)) + '</div>' +
      '<div class="who"><b>' + esc(nm) + '</b> <span class="sub">' + (open ? '&lt;' + esc(m.from.email) + '&gt;' : esc(m.snippet)) + '</span>' +
      (open ? '<div class="sub">to ' + esc(m.to.map(a => a.name || a.email).join(', ') || 'me') + (m.cc.length ? ', cc ' + esc(m.cc.map(a => a.name || a.email).join(', ')) : '') + '</div>' : '') + '</div>' +
      '<span class="when">' + esc(open ? fmtFull(m.date) : fmtDate(m.date)) + '</span>' +
      (open ? '<button class="star ' + (m.starred ? 'on' : '') + '" data-mstar="' + i + '" aria-label="Star">' + starSvg + '</button>' : '') + '</div>';
    if (open) {
      h += '<div class="body"><div class="bodyhost" data-body="' + i + '"></div></div>';
      const atts = (m.attachments || []).filter(a => !a.inline);
      if (atts.length) h += '<div class="atts">' + atts.map(a => '<a class="att" href="' + attUrl(m, a) + '" download="' + esc(a.filename) + '" data-att>' + IC.clip + esc(a.filename) + ' <small>' + fmtSize(a.size) + '</small></a>').join('') + '</div>';
      h += '<div class="foot"><button class="btn" data-reply="' + i + '">Reply</button><button class="btn" data-replyall="' + i + '">Reply all</button><button class="btn" data-fwd="' + i + '">Forward</button></div>';
    }
    h += '</div>';
  });
  return h + '</div>';
}
function attUrl(m, a, inline) {
  return '/api/attachment?account=' + encodeURIComponent(m.accountId) + '&msg=' + encodeURIComponent(m.remoteId) + '&att=' + encodeURIComponent(a.id) +
    '&name=' + encodeURIComponent(a.filename) + '&ct=' + encodeURIComponent(a.contentType || '') + (inline ? '&inline=1' : '') + tokQ();
}

// ---------- message bodies ----------
function linkify(t) {
  return esc(t).replace(/(https?:\/\/[^\s<]+[^\s<.,;:!?)\]])/g, '<a href="$1">$1</a>');
}
function bodyDoc(m) {
  const loadImg = S.data.settings.loadImages === 'true' || S.imagesOk === m.id;
  const csp = "default-src 'none'; img-src 'self' data:" + (loadImg ? ' https: http:' : '') + "; style-src 'unsafe-inline'; font-src data:";
  let html;
  let blocked = false;
  if (m.bodyHtml) {
    html = m.bodyHtml.replace(/\bcid:([^"'\s)>]+)/gi, (x, cid) => {
      const a = (m.attachments || []).find(a => a.contentId === cid);
      return a ? attUrl(m, a, true) : x;
    });
    blocked = !loadImg && /<img[^>]+src=["']?https?:/i.test(html);
  } else {
    html = '<pre style="white-space:pre-wrap;font:inherit;margin:0">' + linkify(m.bodyText || '') + '</pre>';
  }
  const doc = '<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="' + csp + '"><base target="_blank"><style>' +
    'body{margin:0;padding:2px;font:14px/1.5 -apple-system,Segoe UI,Roboto,Arial,sans-serif;color:#202124;background:#fff;word-wrap:break-word;overflow-wrap:anywhere}img{max-width:100%;height:auto}table{max-width:100%}' +
    'a{color:#1a73e8}body.qh .gmail_quote,body.qh blockquote{display:none}</style></head><body class="qh">' + html + '</body></html>';
  return { doc, blocked, hasQuote: /gmail_quote|<blockquote/i.test(html) };
}
function mountBodies() {
  $$('[data-body]').forEach(host => {
    const m = S.open.msgs[+host.dataset.body];
    const b = bodyDoc(m);
    if (b.blocked) {
      const bar = document.createElement('div'); bar.className = 'imgbar';
      bar.innerHTML = 'Remote images are blocked to protect your privacy. <button class="btn" style="padding:2px 10px">Load images</button>';
      bar.querySelector('button').onclick = () => { S.imagesOk = m.id; render(); };
      host.appendChild(bar);
    }
    const f = document.createElement('iframe');
    f.setAttribute('sandbox', 'allow-same-origin');
    f.setAttribute('title', 'Message body');
    f.srcdoc = b.doc;
    f.onload = () => {
      const d = f.contentDocument; if (!d) return;
      const fit = () => { f.style.height = (d.documentElement.scrollHeight + 4) + 'px'; };
      fit(); setTimeout(fit, 250); setTimeout(fit, 1200);
      $$('img', d).forEach(i => i.addEventListener('load', fit));
      d.addEventListener('click', e => {
        const a = e.target.closest && e.target.closest('a'); if (!a) return;
        e.preventDefault();
        const href = a.getAttribute('href') || '';
        if (/^mailto:/i.test(href)) { openComposer({ mode: 'new', to: [{ email: href.replace(/^mailto:/i, '').split('?')[0] }] }); return; }
        if (/^https?:/i.test(href)) api('open', { url: href }).catch(e => toast(e.message, { err: true }));
      });
      d.addEventListener('keydown', onKey);
    };
    host.appendChild(f);
    if (b.hasQuote) {
      const t = document.createElement('button'); t.className = 'btn'; t.style.cssText = 'margin-top:6px;padding:0 10px'; t.textContent = '...'; t.title = 'Show trimmed content';
      t.onclick = () => { const d = f.contentDocument; d.body.classList.toggle('qh'); f.style.height = (d.documentElement.scrollHeight + 4) + 'px'; };
      host.appendChild(t);
    }
  });
}

// ---------- navigation and actions ----------
function go(folder, acct) { S.folder = folder; S.account = acct || ''; S.q = ''; S.open = null; S.sel.clear(); S.cursor = 0; loadThreads(false).then(render).catch(e => toast(e.message, { err: true })); }
async function search(q) { S.q = q.trim(); S.open = null; S.sel.clear(); S.cursor = 0; await loadThreads(false); render(); }
async function openThread(id) {
  const msgs = await api('thread?id=' + encodeURIComponent(id));
  if (!msgs.length) return;
  S.open = { id, msgs, idx: S.threads.findIndex(t => t.id === id) };
  S.expanded = new Set();
  msgs.forEach((m, i) => { if (!m.read || i === msgs.length - 1) S.expanded.add(m.id); });
  render();
  const convo = $('.convo'); if (convo) convo.scrollTop = 0;
  if (msgs.some(m => !m.read)) { await doAct('read', [id], { quiet: true }); }
}
// the open thread can be closed or replaced while the answer is on its way: the late answer then belongs to nothing
async function reloadOpen() { const cur = S.open; if (!cur) return; const m = await api('thread?id=' + encodeURIComponent(cur.id)); if (S.open !== cur) return; if (m.length) cur.msgs = m; else S.open = null; }
const INVERSE = { archive: 'untrash', trash: 'untrash', spam: 'untrash', read: 'unread', unread: 'read', star: 'unstar', unstar: 'star' };
const LABEL = { archive: 'Conversation archived.', trash: 'Conversation deleted.', spam: 'Reported as spam.', read: 'Marked as read.', unread: 'Marked as unread.', star: 'Starred.', unstar: 'Unstarred.', untrash: 'Moved to inbox.', move: 'Moved.', label: 'Label applied.' };
async function doAct(kind, ids, o) {
  o = o || {};
  try {
    await api('act', { kind, threadIds: ids, folderId: o.folderId || '', accountId: o.accountId || '' });
  } catch (e) { toast(e.message, { err: true }); return; }
  S.lastAct = INVERSE[kind] ? { kind: INVERSE[kind], ids } : null;
  const leaving = ['archive', 'trash', 'spam', 'untrash', 'move'].includes(kind);
  if (S.open && leaving) { S.open = null; }
  await refreshState();
  if (S.open) await reloadOpen();
  await loadThreads(false);
  S.sel.clear();
  render();
  if (!o.quiet && LABEL[kind]) toast(LABEL[kind], INVERSE[kind] ? { action: { label: 'Undo', fn: () => doAct(INVERSE[kind], ids, { quiet: true }) } } : {});
}
function targets() { if (S.open) return [S.open.id]; if (S.sel.size) return [...S.sel]; const t = S.threads[S.cursor]; return t ? [t.id] : []; }

function snoozeMenu(ev, ids) {
  const now = new Date();
  const at = (d, h) => { const x = new Date(d); x.setHours(h, 0, 0, 0); return x; };
  const later = new Date(now.getTime() + 3 * 3600e3);
  const tom = at(new Date(now.getTime() + 864e5), 8);
  const sat = at(new Date(now.getTime() + ((6 - now.getDay() + 7) % 7 || 7) * 864e5), 8);
  const mon = at(new Date(now.getTime() + ((1 - now.getDay() + 7) % 7 || 7) * 864e5), 8);
  const opts = [['Later today', later], ['Tomorrow', tom], ['This weekend', sat], ['Next week', mon]];
  popup(ev, opts.map(([l, d]) => '<button data-until="' + Math.floor(d / 1000) + '">' + l + '<small>' + d.toLocaleString([], { weekday: 'short', hour: 'numeric', minute: '2-digit' }) + '</small></button>').join('') +
    '<div style="padding:6px"><label class="pill">Pick date and time</label><input type="datetime-local" id="snoozeat" style="width:100%"><button class="btn primary" id="snoozego" style="margin-top:6px;justify-content:center">Snooze</button></div>',
    pop => {
      const go = async until => { closePop(); await api('snooze', { threadIds: ids, until }); S.open = null; await loadThreads(false); S.sel.clear(); render(); toast(t('Snoozed until {x}', { x: new Date(until * 1000).toLocaleString([], { weekday: 'short', hour: 'numeric', minute: '2-digit' }) }), { action: { label: 'Undo', fn: async () => { await api('snooze', { threadIds: ids, until: 0 }); refreshSoon(); } } }); };
      $$('[data-until]', pop).forEach(b => b.onclick = () => go(+b.dataset.until));
      $('#snoozego', pop).onclick = () => { const v = $('#snoozeat', pop).value; if (v) go(Math.floor(new Date(v) / 1000)); };
    });
}
function moveMenu(ev, ids) {
  const first = S.open ? S.open.msgs[0] : (S.threads.find(t => t.id === ids[0]) || {});
  const acct = first.accountId;
  const fs = S.data.folders.filter(f => f.accountId === acct && !['starred', 'sent', 'drafts', 'all'].includes(f.role));
  if (!fs.length) { toast('No folders for this account'); return; }
  const isLabel = fs.some(f => f.isLabel);
  popup(ev, '<div class="pill" style="padding:4px 10px">' + (isLabel ? 'Label as' : 'Move to') + '</div>' + fs.map(f => '<button data-f="' + esc(f.remoteId) + '">' + esc(f.name) + '</button>').join('') + '<button data-newf>' + (isLabel ? 'Create new label...' : 'Create new folder...') + '</button>', pop => {
    $('[data-newf]', pop).onclick = async () => {
      closePop();
      const n = prompt(t(isLabel ? 'New label name' : 'New folder name'));
      if (!n) return;
      try {
        const r = await api('folders/create', { accountId: acct, name: n });
        await new Promise(x => setTimeout(x, 1200));
        await refreshState();
        await doAct(isLabel ? 'label' : 'move', ids, { folderId: r.id, accountId: acct });
      } catch (e) { toast(e.message, { err: true }); }
    };
    $$('[data-f]', pop).forEach(b => b.onclick = () => { closePop(); doAct(isLabel ? 'label' : 'move', ids, { folderId: b.dataset.f, accountId: acct }); });
  });
}
let popEl = null;
function closePop() { if (popEl) { popEl.remove(); popEl = null; } }
function popup(ev, html, bind) {
  closePop();
  const el = document.createElement('div'); el.className = 'pop'; el.innerHTML = html; document.body.appendChild(el);
  const r = (ev && ev.currentTarget && ev.currentTarget.getBoundingClientRect) ? ev.currentTarget.getBoundingClientRect() : { left: 200, bottom: 120 };
  el.style.left = Math.min(r.left, innerWidth - 250) + 'px'; el.style.top = Math.min(r.bottom + 4, innerHeight - el.offsetHeight - 8) + 'px';
  popEl = el; bind(el);
  // each menu closes only itself: an older menu's listener must not close the one that replaced it
  setTimeout(() => document.addEventListener('click', function h(e) { if (popEl !== el) { document.removeEventListener('click', h); return; } if (!el.contains(e.target)) { closePop(); document.removeEventListener('click', h); } }), 0);
}


// ---------- Sync now ----------
// The button refreshes every account at once. While it runs the label counts the accounts that have finished; when it is done
// the label says when every account was last brought up to date.
function syncTime(iso) {
  const d = new Date(iso);
  if (!iso || isNaN(d) || d.getFullYear() < 2000) return '';
  const today = new Date().toDateString() === d.toDateString();
  return d.toLocaleString([], today ? { hour: 'numeric', minute: '2-digit' } : { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
}
function paintSync() {
  const b = $('#syncbtn'), l = $('#synclbl'), s = S.sync;
  if (!b || !l) return;
  const run = !!(s && s.running);
  b.classList.toggle('spin', run);
  b.setAttribute('aria-busy', run ? 'true' : 'false');
  let text = '';
  if (run) text = s.total > 0 ? t('Syncing {done} of {total}', { done: Math.min(s.done + 1, s.total), total: s.total }) : t('Syncing...');
  else if (s) text = syncTime(s.lastSync) ? t('Last synced {time}', { time: syncTime(s.lastSync) }) : t('Not synced yet');
  l.textContent = text;
  l.title = s && s.accounts ? s.accounts.map(a => a.email + ': ' + (a.syncing ? t('Syncing...') : (syncTime(a.lastSync) || t('Not synced yet')))).join('\n') : '';
}
let syncTimer = null;
async function pollSync() {
  clearTimeout(syncTimer);
  const was = !!(S.sync && S.sync.running);
  try { S.sync = await api('syncstatus'); } catch (e) { return; }
  paintSync();
  if (S.sync.running) syncTimer = setTimeout(pollSync, 400);
  else if (was) refreshSoon(); // only when a sync has just ended, never on a quiet start
}
async function syncNow() {
  try { S.sync = await api('sync', {}); } catch (e) { toast(e.message, { err: true }); return; }
  paintSync();
  pollSync();
}

function bindMain() {
  $('#menu').onclick = () => { S.side = !S.side; render(); };
  $('#settings').onclick = settingsDialog;
  $('#syncbtn').onclick = syncNow;
  paintSync();
  const rs = $('#retrysync'); if (rs) rs.onclick = () => api('sync', {});
  $$('[data-retry]').forEach(b => b.onclick = async () => { await api('outbox/retry', { id: b.dataset.retry }); refreshSoon(); });
  const q = $('#q'); q.onkeydown = e => { if (e.key === 'Enter') search(q.value); if (e.key === 'Escape') { q.blur(); } };
  if (QKEEP) { q.value = QKEEP.v; q.focus(); try { q.setSelectionRange(QKEEP.a, QKEEP.b); } catch (e) { /* a field that takes no selection */ } QKEEP = null; }
  $('#compose').onclick = () => openComposer({ mode: 'new' });
  $('#fab').onclick = () => openComposer({ mode: 'new' });
  $$('[data-nav]').forEach(b => b.onclick = () => go(b.dataset.nav, b.dataset.acct));
  const more = $('#more'); if (more) more.onclick = async () => { S.offset += 100; await loadThreads(true); render(); };
  if (S.open) {
    $('#back').onclick = () => { S.open = null; render(); };
    $$('[data-cact]').forEach(b => b.onclick = ev => { const a = b.dataset.cact; if (a === 'snooze') snoozeMenu(ev, [S.open.id]); else if (a === 'move') moveMenu(ev, [S.open.id]); else doAct(a, [S.open.id]); });
    $('#newer').onclick = () => stepConvo(-1); $('#older').onclick = () => stepConvo(1);
    $$('[data-toggle]').forEach(h => h.onclick = e => { if (e.target.closest('[data-mstar]')) return; const m = S.open.msgs[+h.dataset.toggle]; if (S.expanded.has(m.id)) S.expanded.delete(m.id); else S.expanded.add(m.id); render(); });
    $$('[data-mstar]').forEach(b => b.onclick = async e => { e.stopPropagation(); const m = S.open.msgs[+b.dataset.mstar]; await api('act', { kind: m.starred ? 'unstar' : 'star', threadIds: [S.open.id] }); await reloadOpen(); render(); });
    $$('[data-reply]').forEach(b => b.onclick = () => replyTo(+b.dataset.reply, 'reply'));
    $$('[data-replyall]').forEach(b => b.onclick = () => replyTo(+b.dataset.replyall, 'replyAll'));
    $$('[data-fwd]').forEach(b => b.onclick = () => replyTo(+b.dataset.fwd, 'forward'));
    return;
  }
  const sa = $('#selall'); if (sa) sa.onchange = () => { S.sel = sa.checked ? new Set(S.threads.map(t => t.id)) : new Set(); render(); };
  const sn = $('#selnone'); if (sn) sn.onclick = () => { S.sel.clear(); render(); };
  $$('[data-bulk]').forEach(b => b.onclick = ev => { const a = b.dataset.bulk, ids = [...S.sel]; if (a === 'snooze') snoozeMenu(ev, ids); else if (a === 'move') moveMenu(ev, ids); else doAct(a, ids); });
  $$('.row').forEach(r => {
    const i = +r.dataset.i;
    r.onclick = e => {
      if (e.target.closest('[data-chk],[data-star],[data-ra]')) return;
      S.cursor = i;
      if (S.folder === 'role:drafts' && !S.q) { api('thread?id=' + encodeURIComponent(S.threads[i].id)).then(ms => { const m = ms[ms.length - 1]; if (m && m.draft) openServerDraft(m.id); else openThread(S.threads[i].id); }); return; }
      openThread(S.threads[i].id);
    };
  });
  $$('[data-draft]').forEach(r => r.onclick = () => {
    const d = S.drafts[+r.dataset.draft];
    let x = {};
    try { x = typeof d.data === 'string' ? JSON.parse(d.data) : d.data; } catch (e) {}
    openComposer(draftToComposer(x));
  });
  $$('[data-chk]').forEach(c => c.onchange = () => { const t = S.threads[+c.dataset.chk]; if (c.checked) S.sel.add(t.id); else S.sel.delete(t.id); render(); });
  $$('[data-star]').forEach(b => b.onclick = e => { e.stopPropagation(); const t = S.threads[+b.dataset.star]; doAct(t.starred ? 'unstar' : 'star', [t.id], { quiet: true }); });
  $$('[data-ra]').forEach(b => b.onclick = ev => { ev.stopPropagation(); const t = S.threads[+b.dataset.i], a = b.dataset.ra; if (a === 'snooze') snoozeMenu(ev, [t.id]); else doAct(a, [t.id]); });
}
function stepConvo(d) {
  if (!S.open) return;
  const i = S.threads.findIndex(t => t.id === S.open.id) + d;
  if (i >= 0 && i < S.threads.length) { S.cursor = i; openThread(S.threads[i].id); }
}

// ---------- keyboard ----------
let gTimer = null;
function onKey(e) {
  if (window.H && H.key) { H.key(e); return; }
  if (e.ctrlKey || e.metaKey || e.altKey) return;
  if (e.key === 'Escape') { closePop(); const d = $('.scrim'); if (d && !d.dataset.sticky) { d.remove(); return; } if (S.open && !typing(e)) { S.open = null; render(); } return; }
  if (typing(e) || $('.scrim')) return;
  const k = e.key;
  if (S.gmode) {
    S.gmode = false; clearTimeout(gTimer);
    const map = { i: 'role:inbox', s: 'starred', t: 'role:sent', d: 'role:drafts', a: 'role:all', b: 'snoozed' };
    if (map[k]) { e.preventDefault(); go(map[k]); }
    return;
  }
  if (k === 'g') { S.gmode = true; gTimer = setTimeout(() => S.gmode = false, 1500); return; }
  if (k === 'c') { e.preventDefault(); openComposer({ mode: 'new' }); return; }
  if (k === '/') { e.preventDefault(); const q = $('#q'); if (q) q.focus(); return; }
  if (k === '?') { helpDialog(); return; }
  if (k === 'z' && S.lastAct) { doAct(S.lastAct.kind, S.lastAct.ids); S.lastAct = null; return; }
  if (S.open) {
    const id = S.open.id;
    const last = S.open.msgs.length - 1;
    switch (k) {
      case 'u': S.open = null; render(); break;
      case 'j': stepConvo(1); break; case 'k': stepConvo(-1); break;
      case 'e': doAct('archive', [id]); break; case '#': case 'Delete': doAct('trash', [id]); break; case '!': doAct('spam', [id]); break;
      case 's': doAct(S.open.msgs.some(m => m.starred) ? 'unstar' : 'star', [id], { quiet: true }); break;
      case 'U': doAct('unread', [id]); break;
      case 'r': replyTo(last, 'reply'); e.preventDefault(); break;
      case 'a': replyTo(last, 'replyAll'); e.preventDefault(); break;
      case 'f': replyTo(last, 'forward'); e.preventDefault(); break;
      case 'b': snoozeMenu({ currentTarget: $('[data-cact=snooze]') }, [id]); break;
      case 'n': case 'p': { const cur = S.open.msgs.findIndex(m => S.expanded.has(m.id)); const n = Math.max(0, Math.min(last, (k === 'n' ? cur + 1 : cur - 1))); S.expanded.add(S.open.msgs[n].id); render(); break; }
    }
    return;
  }
  const t = S.threads[S.cursor];
  switch (k) {
    case 'j': if (S.cursor < S.threads.length - 1) { S.cursor++; render(); keepCursor(); } break;
    case 'k': if (S.cursor > 0) { S.cursor--; render(); keepCursor(); } break;
    case 'o': case 'Enter': if (t) openThread(t.id); break;
    case 'x': if (t) { if (S.sel.has(t.id)) S.sel.delete(t.id); else S.sel.add(t.id); render(); } break;
    case 'e': if (targets().length) doAct('archive', targets()); break;
    case '#': case 'Delete': if (targets().length) doAct('trash', targets()); break;
    case '!': if (targets().length) doAct('spam', targets()); break;
    case 's': if (t) doAct(t.starred ? 'unstar' : 'star', targets(), { quiet: true }); break;
    case 'I': if (targets().length) doAct('read', targets()); break;
    case 'U': if (targets().length) doAct('unread', targets()); break;
    case 'b': if (targets().length) snoozeMenu({ currentTarget: $('.toolbar') }, targets()); break;
    case '*': break;
  }
}
function keepCursor() { const r = $('.row.cursor'); if (r) r.scrollIntoView({ block: 'nearest' }); }
function helpDialog() {
  const rows = [['c', 'Compose'], ['/', 'Search'], ['j / k', 'Next / previous'], ['o or Enter', 'Open conversation'], ['u', 'Back to list'], ['e', 'Archive'], ['# or Delete', 'Delete'], ['!', 'Report spam'], ['s', 'Star'], ['x', 'Select'], ['Shift+I', 'Mark read'], ['Shift+U', 'Mark unread'], ['r', 'Reply'], ['a', 'Reply all'], ['f', 'Forward'], ['b', 'Snooze'], ['z', 'Undo last action'], ['g then i', 'Go to Inbox'], ['g then s', 'Go to Starred'], ['g then t', 'Go to Sent'], ['g then d', 'Go to Drafts'], ['g then a', 'Go to All Mail'], ['Ctrl+Enter', 'Send (in the composer)'], ['?', 'This list']];
  dialog('<h2>Keyboard shortcuts</h2><div class="help">' + rows.map(r => '<div><span class="kbd">' + esc(r[0]) + '</span> ' + esc(r[1]) + '</div>').join('') + '</div><div class="btns"><button class="btn primary" data-close>Done</button></div>');
}

// ---------- dialogs ----------
function dialog(html, bind, sticky) {
  const el = document.createElement('div'); el.className = 'scrim'; if (sticky) el.dataset.sticky = '1';
  el.innerHTML = '<div class="dialog" role="dialog" aria-modal="true">' + html + '</div>';
  el.addEventListener('mousedown', e => { if (e.target === el && !sticky) el.remove(); });
  $$('[data-close]', el).forEach(b => b.onclick = () => el.remove());
  document.body.appendChild(el);
  if (bind) bind(el);
  return el;
}

// ---------- welcome and add account ----------
function welcomeHtml() {
  return '<div class="welcome"><h1>' + esc(t('Welcome to {x}', { x: BRAND })) + '</h1><p>Add your email address. It takes under a minute.</p><div id="addhost"></div></div>';
}
function bindWelcome() { addAccountFlow($('#addhost'), () => { refreshState().then(() => go('role:inbox')); }); }
function addAccountDialog() {
  const el = dialog('<h2>Add an account</h2><div id="addhost"></div><div class="btns"><button class="btn" data-close>Close</button></div>');
  addAccountFlow($('#addhost', el), () => { el.remove(); refreshState().then(() => go('role:inbox')); });
}
function addAccountFlow(host, done) {
  host.innerHTML = '<div class="row2" style="text-align:left"><label class="l" for="em">Email address</label><input id="em" type="email" autocomplete="email" placeholder="you@example.com" autofocus>' +
    '<div class="btns" style="margin-top:6px"><button class="btn primary" id="go">Continue</button></div></div><div id="step"></div>';
  const em = $('#em', host);
  const go1 = async () => {
    const v = em.value.trim(); if (!v) return;
    $('#go', host).innerHTML = '<span class="spin"></span>';
    try { const r = await api('discover', { email: v }); stepFor(host, r, v, done); }
    catch (e) { toast(e.message, { err: true }); }
    $('#go', host).textContent = 'Continue';
  };
  $('#go', host).onclick = go1; em.onkeydown = e => { if (e.key === 'Enter') go1(); };
  setTimeout(() => em.focus(), 50);
}
// the sign-in app fields live under Advanced: a normal add never needs them
function advancedApp(r, saved, help) {
  const g = r.kind === 'google';
  return '<details id="adv"><summary class="pill">Advanced: use my own sign-in app</summary><div class="pill" style="margin:6px 0">Only if your organization gave you its own app.</div><div class="pill" style="margin:6px 0">' + esc(help) + '</div>' +
    '<label class="l">Client ID</label><input id="cid" value="' + esc(saved.clientId || '') + '" autocomplete="off">' +
    (g ? '<label class="l">Client secret ' + (saved.hasSecret ? '(saved, leave empty to keep it)' : '') + '</label><input id="csec" type="password" autocomplete="off">' : '<label class="l">Directory (tenant), default: any</label><input id="ten" value="' + esc(saved.tenant || '') + '" placeholder="common">') +
    '<label class="l">Fixed port (only if the app registers exact redirect addresses)</label><input id="prt" value="' + esc(saved.ports || '') + '" placeholder="any free port"></details>';
}
function oauthButton(host, step, r, done) {
  const g = r.kind === 'google', name = g ? 'Google' : 'Microsoft';
  $('#oauth', step).onclick = async () => {
    const w = $('#wait', step);
    try {
      const typed = {};
      const cid = ($('#cid', step) || {}).value;
      if (cid && cid.trim()) {
        typed['oauth_' + r.kind + '_client_id'] = cid.trim();
        if (g && $('#csec', step).value) typed.oauth_google_client_secret = $('#csec', step).value;
        if (!g) typed.oauth_microsoft_tenant = $('#ten', step).value.trim();
        typed['oauth_' + r.kind + '_ports'] = $('#prt', step).value.trim();
        await api('settings', typed);
        await refreshState();
      }
      const st = await api('accounts/oauth/start', { kind: r.kind });
      w.innerHTML = '<span class="spin"></span> Waiting for you to approve in the browser... <a href="#" id="again">Open the page again</a> &middot; <a href="#" id="cancel">Cancel</a>';
      let live = true;
      $('#again', w).onclick = e => { e.preventDefault(); api('open', { url: st.url }); };
      $('#cancel', w).onclick = e => { e.preventDefault(); live = false; api('accounts/oauth/cancel?id=' + st.id, {}); w.textContent = ''; };
      while (live) {
        const res = await api('accounts/oauth/wait?id=' + st.id);
        if (res.pending) continue;
        toast(t('Added {x}', { x: res.email })); done(); return;
      }
    } catch (e) { w.innerHTML = ''; toast(e.message, { err: true }); }
  };
}
function stepFor(host, r, email, done) {
  const step = $('#step', host);
  const saved = (S.data.signInApps || {})[r.kind] || {};
  const builtIn = !!(S.data.oauth || {})[r.kind];
  if (r.kind === 'microsoft' || (r.kind === 'google' && (builtIn || saved.clientId))) {
    const g = r.kind === 'google', name = g ? 'Google' : 'Microsoft';
    const help = g
      ? t('Create an OAuth client in the Google Cloud console (type "Desktop app"), turn on the Gmail API and the People API, then paste its client ID and secret.')
      : t('Register an app in Microsoft Entra (any organizational directory and personal accounts), add a "Mobile and desktop" redirect of http://localhost, give it Mail.ReadWrite, Mail.Send, Contacts.ReadWrite and offline_access, then paste its application (client) ID.');
    step.innerHTML = '<div class="row2" style="text-align:left"><b>' + esc(t('{x} account', { x: r.provider || name })) + '</b>' +
      '<div class="pill">One click. A browser tab opens so you can approve access; nothing is typed in this app.</div>' +
      '<div class="btns"><button class="btn primary" id="oauth">' + esc(t('Sign in with {x}', { x: name })) + '</button></div><div id="wait"></div>' + advancedApp(r, saved, help) + '</div>';
    oauthButton(host, step, r, done);
    return;
  }
  const g = r.kind === 'google';
  const proto = (r.kind === 'pop3') ? 'pop3' : r.kind;
  const j = proto === 'jmap';
  const steps = g
    ? '<div class="pill" style="margin:6px 0"><b>' + esc(t('Gmail with an app password')) + '</b><ol style="margin:6px 0 6px 18px;padding:0"><li>' + esc(t('Turn on 2-Step Verification in your Google account (myaccount.google.com/security). If you use Google Workspace and it is off, ask your administrator.')) + '</li><li>' + esc(t('Open myaccount.google.com/apppasswords, give the password the name {x}, and create it.', { x: BRAND })) + '</li><li>' + esc(t('Paste the 16 letter password below, without the spaces if you like.')) + '</li></ol></div>'
    : '';
  const o = (v, cur, t) => '<option value="' + v + '"' + (cur === v ? ' selected' : '') + '>' + t + '</option>';
  step.innerHTML = '<div class="row2" style="text-align:left"><b>' + esc(r.provider || (j ? 'JMAP server' : 'Mail server')) + '</b>' + (r.note && !g ? '<div class="pill">' + esc(r.note) + '</div>' : '') + steps +
    '<label class="l" for="pw">' + (g ? 'App password' : 'Password') + '</label><input id="pw" type="password" autocomplete="current-password">' +
    '<details ' + ((!r.imapHost && !r.pop3Host && !r.jmapUrl) ? 'open' : '') + '><summary class="pill">Server settings</summary>' +
    (j ? '<label class="l">JMAP server address</label><input id="jurl" value="' + esc(r.jmapUrl || '') + '" placeholder="https://mail.example.com">'
      : '<label class="l">Incoming protocol</label><select id="pr">' + o('imap', proto, 'IMAP (keeps mail on the server)') + o('pop3', proto, 'POP3 (downloads mail)') + '</select>' +
        '<div class="grid2"><div><label class="l">Incoming server</label><input id="ih" value="' + esc((proto === 'pop3' ? r.pop3Host : r.imapHost) || '') + '"></div><div><label class="l">Port</label><input id="ip" value="' + esc((proto === 'pop3' ? r.pop3Port : r.imapPort) || (proto === 'pop3' ? 995 : 993)) + '"></div></div>' +
        '<label class="l">Incoming security</label><select id="is">' + o('ssl', (proto === 'pop3' ? r.pop3Security : r.imapSecurity) || 'ssl', 'SSL/TLS') + o('starttls', (proto === 'pop3' ? r.pop3Security : r.imapSecurity), 'STARTTLS') + o('none', (proto === 'pop3' ? r.pop3Security : r.imapSecurity), 'None') + '</select>' +
        '<div class="grid2"><div><label class="l">SMTP server</label><input id="sh" value="' + esc(r.smtpHost || '') + '"></div><div><label class="l">Port</label><input id="sp" value="' + esc(r.smtpPort || 587) + '"></div></div>' +
        '<label class="l">SMTP security</label><select id="ss">' + o('starttls', r.smtpSecurity || 'starttls', 'STARTTLS') + o('ssl', r.smtpSecurity, 'SSL/TLS') + o('none', r.smtpSecurity, 'None') + '</select>' +
        '<label style="display:block;margin-top:8px"><input type="checkbox" id="lv" checked style="width:auto"> POP3: leave mail on the server after downloading</label>') +
    '<label class="l">Username (if not the email address)</label><input id="un" value="' + esc(r.username && r.username !== email ? r.username : '') + '"><label style="display:block;margin-top:8px"><input type="checkbox" id="sk" style="width:auto"> Trust this server\'s certificate even if it is self-signed</label></details>' +
    (g ? advancedApp({ kind: 'google' }, saved, t('Gmail also signs in with your own Google client: paste its ID and secret, then use the button.')) + '<div class="btns"><button class="btn" id="oauth" style="display:none">Sign in with Google</button></div><div id="wait"></div>' : '') +
    '<div class="btns"><button class="btn primary" id="add">Add account</button></div></div>';
  const emailEl = $('#em', host);
  if (g) {
    const adv = $('#adv', step), ob = $('#oauth', step);
    adv.ontoggle = () => { ob.style.display = adv.open ? '' : 'none'; };
    oauthButton(host, step, r, done);
  }
  $('#add', step).onclick = async () => {
    const btn = $('#add', step); btn.innerHTML = '<span class="spin"></span>';
    const p = j ? 'jmap' : ($('#pr', step) ? $('#pr', step).value : 'imap');
    const body = { kind: p, email: emailEl.value.trim() || email, password: (g ? $('#pw', step).value.replace(/\s+/g, '') : $('#pw', step).value), username: $('#un', step).value, skipVerify: $('#sk', step).checked };
    if (j) body.jmapUrl = $('#jurl', step).value;
    else {
      body.smtpHost = $('#sh', step).value; body.smtpPort = +$('#sp', step).value; body.smtpSecurity = $('#ss', step).value;
      if (p === 'pop3') { body.pop3Host = $('#ih', step).value; body.pop3Port = +$('#ip', step).value; body.pop3Security = $('#is', step).value; body.leaveOnServer = $('#lv', step).checked; }
      else { body.imapHost = $('#ih', step).value; body.imapPort = +$('#ip', step).value; body.imapSecurity = $('#is', step).value; }
    }
    try { const a = await api('accounts/password', body); toast(t('Added {x}', { x: a.email })); done(); }
    catch (e) { toast(e.message, { err: true }); btn.textContent = 'Add account'; }
  };
}

// ---------- settings ----------
function settingsDialog() {
  const d = S.data, s = d.settings;
  s.signInApps = d.signInApps;
  const accts = d.accounts.map(a => '<div style="border:1px solid var(--line);border-radius:10px;padding:12px;margin:8px 0"><div style="display:flex;align-items:center;gap:8px"><span class="dot" style="width:10px;height:10px;border-radius:50%;background:' + esc(a.color) + '"></span><b>' + esc(a.email) + '</b><span class="pill">' + esc(a.kind) + '</span><span class="spacer"></span><button class="btn danger" data-rm="' + esc(a.id) + '">Remove</button></div>' +
    '<label class="l" style="margin-top:8px">Name on sent mail</label><input data-name="' + esc(a.id) + '" value="' + esc(a.name) + '"><label class="l" style="margin-top:8px">Signature</label><textarea rows="3" data-sig="' + esc(a.id) + '">' + esc(a.signature) + '</textarea></div>').join('');
  const el = dialog('<h2>Settings</h2><h3>Accounts</h3>' + accts + '<button class="btn" id="addacct">Add an account</button>' +
    '<h3>Appearance</h3><div class="grid2" style="grid-template-columns:1fr 1fr"><div><label class="l">Theme</label><select id="theme"><option value="auto">Match system</option><option value="light">Light</option><option value="dark">Dark</option></select></div><div><label class="l">Density</label><select id="density"><option value="comfortable">Comfortable</option><option value="compact">Compact</option></select></div></div>' +
    '<h3>Behavior</h3><div class="row2"><div><label class="l">Undo send delay</label><select id="undo"><option value="0">Off</option><option>5</option><option>10</option><option>20</option><option>30</option></select></div>' +
    '<label><input type="checkbox" id="imgs" style="width:auto"> Always load remote images (they can tell the sender you read the message)</label><label><input type="checkbox" id="notif" style="width:auto"> Desktop notifications for new mail</label></div>' +
    '<details id="signinapps"><summary><b>Advanced: sign-in apps</b></summary><div class="row2"><div class="pill">Only for an organization that gave you its own Google or Microsoft app. Leave empty to use the one built into this copy of the app.</div>' +
    '<label class="l">Google client ID</label><input id="sg_id" autocomplete="off"><label class="l">Google client secret</label><input id="sg_sec" type="password" autocomplete="off" placeholder="' + ((s.signInApps || {}).google && s.signInApps.google.hasSecret ? 'saved, leave empty to keep it' : '') + '">' +
    '<label class="l">Microsoft application (client) ID</label><input id="sm_id" autocomplete="off"><label class="l">Microsoft directory (tenant)</label><input id="sm_ten" placeholder="common"></div></details>' +
    '<h3>MCP server</h3><div class="row2"><div class="pill">The app runs a read and write MCP server on this computer so other programs can use your mail. It signs in as you and reaches every account above.</div><div id="mcpbox" class="pill">Loading...</div></div>' +
    '<h3>Mail tools</h3><div class="toolgrid">' + [['identities', 'Identities and signatures'], ['rules', 'Filters'], ['templates', 'Templates'], ['vacation', 'Vacation reply'], ['sieve', 'Server filters'], ['security', 'Encryption keys'], ['dav', 'Calendar and contacts server'], ['importExport', 'Import mail'], ['contacts', 'Contacts'], ['calendar', 'Calendar'], ['general', 'More settings']].map(t => '<button class="btn" data-tool="' + t[0] + '">' + t[1] + '</button>').join('') + '</div>' +
    '<div class="btns"><button class="btn" id="keys">Keyboard shortcuts</button><button class="btn primary" id="save">Save</button></div>');
  $$('[data-tool]', el).forEach(b => b.onclick = () => { el.remove(); const c = H.commands.find(c => c.id === b.dataset.tool); if (c) c.run(); });
  api('mcp').then(m => {
    const b = $('#mcpbox', el); if (!b) return;
    if (!m.enabled) { b.textContent = 'The MCP server is not running in this session.'; return; }
    const cfg = JSON.stringify({ mcpServers: { [BRAND.toLowerCase()]: { type: 'http', url: m.url, headers: { Authorization: 'Bearer ' + m.token } } } }, null, 2);
    b.innerHTML = '<div>Endpoint <span class="kbd">' + esc(m.url) + '</span></div><textarea rows="7" readonly style="width:100%;font-family:var(--font-mono);font-size:12px;margin-top:6px">' + esc(cfg) + '</textarea><button class="btn" id="copycfg" style="margin-top:6px">Copy client config</button>';
    $('#copycfg', el).onclick = () => { navigator.clipboard.writeText(cfg).then(() => toast('Copied')); };
  }).catch(() => { const b = $('#mcpbox', el); if (b) b.textContent = 'Not available here.'; });
  $('#theme', el).value = s.theme; $('#density', el).value = s.density; $('#undo', el).value = s.undoSeconds; $('#imgs', el).checked = s.loadImages === 'true'; $('#notif', el).checked = s.notifications === 'true';
  const sia = d.signInApps || {};
  $('#sg_id', el).value = (sia.google || {}).clientId || ''; $('#sm_id', el).value = (sia.microsoft || {}).clientId || ''; $('#sm_ten', el).value = (sia.microsoft || {}).tenant || '';
  $('#addacct', el).onclick = () => { el.remove(); addAccountDialog(); };
  $('#keys', el).onclick = () => { el.remove(); helpDialog(); };
  $$('[data-rm]', el).forEach(b => b.onclick = async () => { if (confirm(t('Remove this account and its saved mail from this computer? Mail on the server is not touched.'))) { await api('accounts/remove', { id: b.dataset.rm }); el.remove(); await refreshState(); go('role:inbox'); } });
  $('#save', el).onclick = async () => {
    for (const a of d.accounts) await api('accounts/update', { id: a.id, name: $('[data-name="' + a.id + '"]', el).value, signature: $('[data-sig="' + a.id + '"]', el).value });
    const apps = { oauth_google_client_id: $('#sg_id', el).value.trim(), oauth_microsoft_client_id: $('#sm_id', el).value.trim(), oauth_microsoft_tenant: $('#sm_ten', el).value.trim() };
    if ($('#sg_sec', el).value) apps.oauth_google_client_secret = $('#sg_sec', el).value;
    await api('settings', { ...apps, theme: $('#theme', el).value, density: $('#density', el).value, undoSeconds: $('#undo', el).value, loadImages: String($('#imgs', el).checked), notifications: String($('#notif', el).checked) });
    if ($('#notif', el).checked && window.Notification && Notification.permission === 'default') Notification.requestPermission();
    await refreshState(); el.remove(); render(); toast('Saved');
  };
}

// ---------- composer ----------
const fileToB64 = f => new Promise((res, rej) => { const r = new FileReader(); r.onload = () => res(String(r.result).split(',')[1] || ''); r.onerror = () => rej(r.error); r.readAsDataURL(f); });
const b64ToFile = (a) => { const bin = atob(a.base64 || ''); const u = new Uint8Array(bin.length); for (let i = 0; i < bin.length; i++) u[i] = bin.charCodeAt(i); return new File([u], a.filename || 'attachment', { type: a.contentType || 'application/octet-stream' }); };
function draftToComposer(x) {
  return { id: x.id, accountId: x.accountId, to: (x.to || []).flatMap(parseAddrs), cc: (x.cc || []).flatMap(parseAddrs), bcc: (x.bcc || []).flatMap(parseAddrs), subject: x.subject || '', html: x.bodyHtml || '', fromEmail: x.fromEmail || '', receipt: !!x.receiptRequested, protect: x.protect || '', sign: !!x.sign, encrypt: !!x.encrypt, fromDraft: true, inReplyTo: x.inReplyTo || '', refs: x.references || [], threadHint: x.threadHint || '', remoteId: x.remoteId || '', files: (x.attachments || []).map(b64ToFile) };
}
async function openServerDraft(messageId) {
  try {
    const d = await api('drafts/open?message=' + encodeURIComponent(messageId));
    await api('drafts', d); // keep a local record so a send or discard removes the server copy
    openComposer(draftToComposer(d));
  } catch (e) { toast(e.message, { err: true }); }
}
let cseq = 0;
function parseAddrs(s) {
  const out = [];
  String(s).split(/[,;]\s*(?=(?:[^"]*"[^"]*")*[^"]*$)/).forEach(p => {
    p = p.trim(); if (!p) return;
    const m = p.match(/^"?([^"<]*?)"?\s*<([^>]+)>$/);
    if (m) out.push({ name: m[1].trim(), email: m[2].trim() }); else out.push({ name: '', email: p.replace(/[<>]/g, '') });
  });
  return out;
}
const okMail = e => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(e);
function quoteHtml(m) {
  let inner;
  if (m.bodyHtml) {
    const d = new DOMParser().parseFromString(m.bodyHtml, 'text/html');
    $$('script,style,link,meta,iframe,object,embed,form', d).forEach(n => n.remove());
    $$('*', d).forEach(n => { for (const a of [...n.attributes]) if (/^on/i.test(a.name) || (a.name === 'src' && !/^data:/i.test(a.value)) || a.name === 'srcset') n.removeAttribute(a.name); });
    inner = d.body.innerHTML;
  } else inner = '<div>' + esc(m.bodyText || '').replace(/\n/g, '<br>') + '</div>';
  return '<br><div>' + esc(t('On {date}, {who} wrote:', { date: fmtFull(m.date), who: addrStr(m.from) })) + '</div><blockquote>' + inner + '</blockquote>';
}
async function replyTo(i, mode) {
  const m = S.open.msgs[i];
  const me = myEmails();
  const c = { mode, accountId: m.accountId, threadId: m.threadId, inReplyTo: m.messageId, refs: (m.references || []).concat(m.messageId ? [m.messageId] : []), threadHint: (m.threadId || '').split('/').slice(1).join('/') };
  const re = /^re:\s*/i, fw = /^fwd?:\s*/i;
  if (mode === 'forward') {
    c.subject = (fw.test(m.subject) ? '' : 'Fwd: ') + m.subject; c.inReplyTo = ''; c.refs = [];
    c.html = '<br><div>' + esc(t('---------- Forwarded message ----------')) + '<br>' + esc(t('From:')) + ' ' + esc(addrStr(m.from)) + '<br>' + esc(t('Date:')) + ' ' + esc(fmtFull(m.date)) + '<br>' + esc(t('Subject:')) + ' ' + esc(m.subject) + '<br>' + esc(t('To:')) + ' ' + esc(m.to.map(addrStr).join(', ')) + '</div>' + quoteHtml(m).replace(/^<br><div>[^<]*<\/div>/, '');
    c.files = [];
    for (const a of (m.attachments || []).filter(a => !a.inline)) {
      try { const r = await fetch(attUrl(m, a)); const b = await r.blob(); c.files.push(new File([b], a.filename, { type: a.contentType || b.type })); } catch (e) { toast(t('Could not attach {x}', { x: a.filename }), { err: true }); }
    }
  } else {
    c.subject = (re.test(m.subject) ? '' : 'Re: ') + m.subject;
    const fromMe = me.includes(m.from.email.toLowerCase());
    const base = fromMe ? m.to : (m.replyTo && m.replyTo.length ? m.replyTo : [m.from]);
    c.to = base.slice();
    if (mode === 'replyAll') {
      const seen = new Set(c.to.map(a => a.email.toLowerCase()).concat(me));
      c.cc = m.to.concat(m.cc).filter(a => !seen.has(a.email.toLowerCase()) && seen.add(a.email.toLowerCase()));
    }
    c.html = quoteHtml(m);
  }
  openComposer(c);
}
function openComposer(init) {
  const c = Object.assign({ id: 'c' + (++cseq) + Date.now(), mode: 'new', to: [], cc: [], bcc: [], subject: '', html: '', files: [], accountId: '', inReplyTo: '', refs: [], threadHint: '' }, init);
  if (!c.accountId) c.accountId = (S.account || ((S.data.accounts[0] || {}).id)) || '';
  c.to = (c.to || []).slice(); c.cc = (c.cc || []).slice(); c.bcc = (c.bcc || []).slice(); c.files = (c.files || []).slice();
  const root = document.createElement('div'); root.className = 'composer'; root.setAttribute('role', 'dialog'); root.setAttribute('aria-label', 'New message');
  const accts = S.data.accounts;
  const title = c.subject ? c.subject : 'New message';
  root.innerHTML = '<div class="chead"><span class="ttl" style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap"></span><button class="icon" data-min title="Minimize" aria-label="Minimize">&#8211;</button><button class="icon" data-full title="Full screen" aria-label="Full screen">&#9633;</button><button class="icon" data-x title="Close" aria-label="Close">&#10005;</button></div>' +
    '<div class="cbody"><div class="fld"><label>From</label><select data-from>' + accts.map(a => '<option value="' + esc(a.id) + '">' + esc((a.name ? a.name + ' ' : '') + '<' + a.email + '>') + '</option>').join('') + '</select></div>' +
    '<div class="fld" data-f="to"><label>To</label><div class="chips"></div><button class="btn" data-ccb style="padding:0 8px">Cc</button><button class="btn" data-bccb style="padding:0 8px">Bcc</button></div>' +
    '<div class="fld hide" data-f="cc"><label>Cc</label><div class="chips"></div></div><div class="fld hide" data-f="bcc"><label>Bcc</label><div class="chips"></div></div>' +
    '<div class="fld"><label>Subject</label><input data-subj></div><div class="cfiles"></div>' +
    '<div class="editor" contenteditable="true" role="textbox" aria-multiline="true" aria-label="Message body"></div>' +
    '<div class="cfoot"><button class="btn primary" data-send>Send</button><div class="fmt"><button data-cmd="bold" title="Bold"><b>B</b></button><button data-cmd="italic" title="Italic"><i>I</i></button><button data-cmd="underline" title="Underline"><u>U</u></button><button data-cmd="insertUnorderedList" title="Bullets">&#8226;</button><button data-cmd="insertOrderedList" title="Numbers">1.</button><button data-cmd="formatBlock" data-arg="blockquote" title="Quote">&#8220;</button><button data-link title="Link">&#128279;</button><button data-cmd="removeFormat" title="Clear formatting">Tx</button></div>' +
    '<button class="btn" data-attach>Attach</button><input type="file" multiple hidden data-file><span class="spacer"></span><button class="btn" data-discard>Discard</button></div></div>';
  document.body.appendChild(root);
  stackComposers();
  const ed = $('.editor', root);
  ed.innerHTML = c.html || '';
  $('[data-from]', root).value = c.accountId;
  $('[data-subj]', root).value = c.subject;
  const ttl = () => { $('.ttl', root).textContent = $('[data-subj]', root).value || 'New message'; };
  ttl(); $('[data-subj]', root).oninput = () => { c.subject = $('[data-subj]', root).value; ttl(); save(); };
  $('[data-from]', root).onchange = e => { c.accountId = e.target.value; save(); };
  // recipients
  ['to', 'cc', 'bcc'].forEach(k => chipField(root, c, k, save));
  if (c.cc.length) $('[data-f=cc]', root).classList.remove('hide');
  if (c.bcc.length) $('[data-f=bcc]', root).classList.remove('hide');
  $('[data-ccb]', root).onclick = () => { $('[data-f=cc]', root).classList.remove('hide'); $('[data-f=cc] input', root).focus(); };
  $('[data-bccb]', root).onclick = () => { $('[data-f=bcc]', root).classList.remove('hide'); $('[data-f=bcc] input', root).focus(); };
  // window controls
  $('.chead', root).onclick = e => { if (e.target.closest('button')) return; root.classList.toggle('min'); };
  $('[data-min]', root).onclick = () => root.classList.toggle('min');
  $('[data-full]', root).onclick = () => { root.classList.toggle('full'); root.classList.remove('min'); };
  $('[data-x]', root).onclick = () => close(false);
  $('[data-discard]', root).onclick = () => close(true);
  // formatting
  $$('[data-cmd]', root).forEach(b => b.onmousedown = e => { e.preventDefault(); document.execCommand(b.dataset.cmd, false, b.dataset.arg || null); save(); });
  $('[data-link]', root).onmousedown = e => { e.preventDefault(); const u = prompt(t('Link address (https://...)')); if (u) document.execCommand('createLink', false, u); };
  // attachments
  const files = $('.cfiles', root);
  const drawFiles = () => { files.innerHTML = ''; c.files.forEach((f, i) => { const s = document.createElement('span'); s.className = 'att'; s.innerHTML = esc(f.name) + ' <small>' + fmtSize(f.size) + '</small> <button data-rm aria-label="Remove">&#10005;</button>'; s.querySelector('button').onclick = () => { c.files.splice(i, 1); drawFiles(); save(); }; files.appendChild(s); }); };
  const addFiles = list => { for (const f of list) { c.files.push(f); } drawFiles(); save(); };
  $('[data-attach]', root).onclick = () => $('[data-file]', root).click();
  $('[data-file]', root).onchange = e => { addFiles(e.target.files); e.target.value = ''; };
  ['dragenter', 'dragover'].forEach(ev => root.addEventListener(ev, e => { if (e.dataTransfer && [...e.dataTransfer.types].includes('Files')) { e.preventDefault(); root.classList.add('drop'); } }));
  ['dragleave', 'drop'].forEach(ev => root.addEventListener(ev, e => { root.classList.remove('drop'); }));
  root.addEventListener('drop', e => { if (e.dataTransfer && e.dataTransfer.files.length) { e.preventDefault(); addFiles(e.dataTransfer.files); } });
  ed.addEventListener('paste', e => { const fl = [...(e.clipboardData ? e.clipboardData.files : [])]; if (fl.length) { e.preventDefault(); addFiles(fl); } });
  drawFiles();
  ed.addEventListener('input', save);
  root.addEventListener('keydown', e => { if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') { e.preventDefault(); send(); } if (e.key === 'Escape') e.stopPropagation(); });
  $('[data-send]', root).onclick = send;
  c.root = root;
  S.composers.push(c);
  if (window.H && H.composer) H.composer(root, c, { ed, save: () => save(), send: () => send() });
  // the first field takes focus once the composer is on screen, unless the person has already put the cursor somewhere in it:
  // on a slow machine this timer used to fire after they had started typing and pull the cursor away
  setTimeout(() => { if (root.contains(document.activeElement)) return; (c.to.length ? ed : $('[data-f=to] input', root)).focus(); }, 30);
  if (c.mode !== 'new' && c.to.length) { const r = document.createRange(); r.setStart(ed, 0); r.collapse(true); const sel = getSelection(); sel.removeAllRanges(); sel.addRange(r); }

  let dirty = false, savedOnce = false;
  function save() { dirty = true; }
  async function flush() {
    if (!dirty) return;
    dirty = false;
    const empty = !c.to.length && !c.subject && !ed.innerText.trim();
    if (empty) return;
    try {
      const attachments = [];
      for (const f of c.files) attachments.push({ filename: f.name, contentType: f.type || 'application/octet-stream', base64: await fileToB64(f) });
      await api('drafts', { id: c.id, accountId: c.accountId, to: c.to.map(addrFmt), cc: c.cc.map(addrFmt), bcc: c.bcc.map(addrFmt), subject: c.subject, bodyText: ed.innerText, bodyHtml: ed.innerHTML, inReplyTo: c.inReplyTo, references: c.refs || [], threadHint: c.threadHint, remoteId: c.remoteId || '', attachments, ...(window.H ? H.draftExtras(c) : {}) });
      savedOnce = true;
    } catch (e) { /* the next tick tries again */ }
  }
  const timer = setInterval(flush, 3000);
  function drop() {
    clearInterval(timer);
    if (savedOnce || dirty) api('drafts/discard', { id: c.id }).catch(() => {});
    root.remove(); S.composers = S.composers.filter(x => x !== c); stackComposers();
  }
  function close(discard) {
    const empty = !c.to.length && !c.subject && !ed.innerText.trim() && !c.files.length;
    if (!empty && !discard) { dirty = true; flush(); toast('Draft saved.', { action: { label: 'Discard', fn: drop } }); clearInterval(timer); root.remove(); S.composers = S.composers.filter(x => x !== c); stackComposers(); return; }
    drop();
  }
  async function send() {
    flushChips(root);
    const bad = [...c.to, ...c.cc, ...c.bcc].find(a => !okMail(a.email));
    if (bad) { toast(t('"{x}" is not a valid address.', { x: bad.email }), { err: true }); return; }
    if (!c.to.length && !c.cc.length && !c.bcc.length) { toast('Add at least one recipient.', { err: true }); return; }
    if (!c.subject.trim() && !confirm(t('Send this message without a subject?'))) return;
    if (window.H && H.checkSend && !H.checkSend(c)) return;
    const html = ed.innerHTML, text = ed.innerText;
    if (/attach|attached|enclosed/i.test(text.replace(/\n>.*/g, '')) && !c.files.length && !confirm(t('You mentioned an attachment but none is attached. Send anyway?'))) return;
    const fd = new FormData();
    fd.append('json', JSON.stringify({ accountId: c.accountId, to: c.to.map(addrFmt).join(', '), cc: c.cc.map(addrFmt).join(', '), bcc: c.bcc.map(addrFmt).join(', '), subject: c.subject, bodyText: text, bodyHtml: '<div>' + html + '</div>', inReplyTo: c.inReplyTo, references: (c.refs || []).join(' '), threadHint: c.threadHint, sendAt: c.sendAt || '', ...(window.H ? H.extras(c) : {}) }));
    c.files.forEach(f => fd.append('file', f, f.name));
    const btn = $('[data-send]', root); btn.disabled = true; btn.innerHTML = '<span class="spin"></span>';
    try {
      const r = await api('send', fd);
      if (S.data.settings.archiveAfterReply === 'true' && c.threadId && c.mode !== 'new' && c.mode !== 'forward') doAct('archive', [c.threadId], { quiet: true });
      const snapshot = { to: c.to, cc: c.cc, bcc: c.bcc, subject: c.subject, html, files: c.files.slice(), accountId: c.accountId, inReplyTo: c.inReplyTo, refs: c.refs, threadHint: c.threadHint, mode: c.mode };
      drop();
      if (r.scheduled) { toast('Scheduled for ' + new Date(r.scheduled).toLocaleString([], { weekday: 'short', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }) + '. It waits in the outbox until then.', { ms: 8000, action: { label: 'Cancel', fn: async () => { const u = await api('undo', { id: r.id }); toast(u.undone ? 'Cancelled.' : 'Too late, it has already been sent.'); } } }); }
      else if (r.undoMs > 0) {
        toast('Sending message...', { ms: r.undoMs + 800, action: { label: 'Undo', fn: async () => { const u = await api('undo', { id: r.id }); if (u.undone) { toast('Send undone.'); openComposer(snapshot); } else toast('Too late, it has already been sent.'); } } });
      }
    } catch (e) { btn.disabled = false; btn.textContent = 'Send'; toast(e.message, { err: true }); }
  }
}
function stackComposers() { let right = 24; S.composers.slice().reverse().forEach(c => { if (!c.root.classList.contains('full')) { c.root.style.right = right + 'px'; right += c.root.offsetWidth + 12; } }); }
function flushChips(root) { $$('.chips input', root).forEach(i => { i.dispatchEvent(new Event('flush')); }); }
function chipField(root, c, key, save) {
  const box = $('[data-f=' + key + '] .chips', root);
  const input = document.createElement('input'); input.setAttribute('aria-label', t({ to: 'To recipients', cc: 'Cc recipients', bcc: 'Bcc recipients' }[key]));
  let ac = null, acItems = [], acIdx = -1, acTimer = null;
  const draw = () => {
    $$('.chip', box).forEach(n => n.remove());
    c[key].forEach((a, i) => { const s = document.createElement('span'); s.className = 'chip' + (okMail(a.email) ? '' : ' bad'); s.title = a.email; s.innerHTML = esc(a.name || a.email) + '<button aria-label="Remove">&#10005;</button>'; s.querySelector('button').onclick = () => { c[key].splice(i, 1); draw(); save(); }; box.insertBefore(s, input); });
  };
  const commit = () => { const v = input.value.trim(); if (v) { parseAddrs(v).forEach(a => c[key].push(a)); input.value = ''; draw(); save(); } hideAc(); };
  const hideAc = () => { if (ac) { ac.remove(); ac = null; } acItems = []; acIdx = -1; };
  const showAc = list => {
    hideAc(); if (!list.length) return;
    ac = document.createElement('div'); ac.className = 'ac'; acItems = list;
    list.forEach((x, i) => { const d = document.createElement('div'); d.innerHTML = esc(x.name || x.email) + '<small>' + esc(x.name ? x.email : '') + '</small>'; d.onmousedown = e => { e.preventDefault(); pick(i); }; ac.appendChild(d); });
    box.parentElement.appendChild(ac);
  };
  const pick = i => { const x = acItems[i]; if (x) { if (x.members && x.members.length) x.members.forEach(m => c[key].push({ name: '', email: m })); else c[key].push({ name: x.name || '', email: x.email }); input.value = ''; draw(); save(); } hideAc(); input.focus(); };
  input.oninput = () => {
    if (acTimer) { clearTimeout(acTimer); acTimer = null; window.__busy--; }
    const v = input.value.trim().replace(/,$/, '');
    if (input.value.endsWith(',') || input.value.endsWith(';')) { commit(); return; }
    if (v.length < 1) { hideAc(); return; }
    window.__busy = (window.__busy || 0) + 1;
    acTimer = setTimeout(async () => { acTimer = null; window.__busy--; try { showAc(await api('contacts?q=' + encodeURIComponent(v))); } catch (e) {} }, 120);
  };
  input.onkeydown = e => {
    if (ac && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) { e.preventDefault(); acIdx = (acIdx + (e.key === 'ArrowDown' ? 1 : -1) + acItems.length) % acItems.length; $$('.ac div', ac).forEach((d, i) => d.classList.toggle('on', i === acIdx)); return; }
    if ((e.key === 'Enter' || e.key === 'Tab') && ac && acIdx >= 0) { e.preventDefault(); pick(acIdx); return; }
    if (e.key === 'Enter' || (e.key === 'Tab' && input.value.trim())) { if (input.value.trim()) { e.preventDefault(); commit(); } return; }
    if (e.key === 'Backspace' && !input.value && c[key].length) { c[key].pop(); draw(); save(); }
  };
  input.onblur = () => setTimeout(commit, 120);
  input.addEventListener('flush', commit);
  input.onpaste = e => { const t = (e.clipboardData || window.clipboardData).getData('text'); if (/[,;]/.test(t)) { e.preventDefault(); parseAddrs(t).forEach(a => c[key].push(a)); draw(); save(); } };
  box.appendChild(input); draw();
}

// ---------- events and startup ----------
async function pollEvents() {
  for (;;) {
    try {
      const r = await api('events?since=' + S.seq);
      S.seq = r.seq;
      for (const ev of r.events) handleEvent(ev);
    } catch (e) { await new Promise(r => setTimeout(r, 3000)); }
  }
}
function handleEvent(ev) {
  switch (ev.type) {
    case 'syncing': case 'syncdone': pollSync(); break;
    case 'synced': case 'accounts': case 'settings': case 'snooze': refreshSoon(); if (ev.type === 'snooze') toast(ev.text); break;
    case 'error': if (ev.subject) toast(ev.text, { err: true }); refreshSoon(); break;
    case 'sent': toast('Message sent.'); refreshSoon(); break;
    case 'new':
      if (S.data && S.data.settings.notifications === 'true' && !window.__native && window.Notification && Notification.permission === 'granted' && document.hidden) new Notification(ev.from, { body: ev.subject });
      break;
  }
}
window.addEventListener('online', () => { S.online = true; api('sync', {}).catch(() => {}); render(); });
window.addEventListener('offline', () => { S.online = false; render(); });
document.addEventListener('keydown', onKey);
(async function main() {
  try {
    await refreshState();
    await loadThreads(false);
    render();
    pollSync();
    S.drafts = await api('drafts').catch(() => []);
    if (window.H) { await Promise.all([H.loadSearches(), H.loadTemplates(), H.loadIdentities(), H.loadKeys()]); if (H.i18n) H.i18n.set(S.data.settings.language); render(); }
  } catch (e) { $('#app').innerHTML = '<div class="empty"><div><h2>Could not start</h2><p>' + esc(e.message) + '</p></div></div>'; }
  pollEvents();
  // inside the native window the Wails runtime exists: mailto links and notifications come from the shell
  if (window.runtime && window.runtime.EventsOn) {
    window.__native = true;
    window.runtime.EventsOn('mailto', url => { const u = new URL(url); openComposer({ mode: 'new', to: [{ email: decodeURIComponent(u.pathname) }], subject: u.searchParams.get('subject') || '', html: esc(u.searchParams.get('body') || '') }); });
    window.runtime.EventsEmit('ready');
  }
})();
