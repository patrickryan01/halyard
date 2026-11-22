'use strict';
/* What sits around a message: calendar invitations, unsubscribe, read receipts, signature and encryption checks,
   printing, saving, the source, folder tools and saved searches. */

H.cache = {};
const cacheGet = (k, f) => (H.cache[k] = H.cache[k] || f());

// ---------- decoration of an open conversation ----------
const PROTECTED = /pgp-signature|pgp-encrypted|pkcs7-signature|pkcs7-mime|x-pkcs7/i;
const isProtected = m => (m.attachments || []).some(a => PROTECTED.test(a.contentType || '') || /\.(p7m|p7s)$/i.test(a.filename || '')) ;

function bar(cls, html) { const d = document.createElement('div'); d.className = 'mxbar ' + (cls || ''); d.innerHTML = html; return d; }

H.afterRender = function () {
  H.decorateSide();
  if (!S.open) { H.decorateToolbar(); return; }
  H.decorateToolbar();
  S.open.msgs.forEach((m, i) => {
    if (!S.expanded.has(m.id)) return;
    const box = $('.msg[data-m="' + i + '"]'); if (!box) return;
    const slot = document.createElement('div'); slot.className = 'mx';
    const head = $('.mh', box); head.after(slot);
    H.decorate(slot, m, i);
    const foot = $('.foot', box);
    if (foot) {
      foot.insertAdjacentHTML('beforeend', '<span class="spacer"></span><button class="btn" data-print="' + i + '">Print</button><button class="btn" data-src="' + i + '">View source</button><button class="btn" data-eml="' + i + '">Save as file</button>');
      $('[data-print]', foot).onclick = () => H.printMsgs([m]);
      $('[data-src]', foot).onclick = () => H.viewSource(m);
      $('[data-eml]', foot).onclick = () => H.saveEml(m);
    }
  });
};

H.decorate = function (slot, m, i) {
  if (m.listUnsubscribe) {
    const b = bar('', 'This message came from a mailing list. <button class="btn" data-unsub>Unsubscribe</button>');
    $('[data-unsub]', b).onclick = async () => {
      if (!confirm(t('Tell the sender to stop sending you these messages?'))) return;
      try {
        const r = await api('unsubscribe', { id: m.id });
        if (r.openUrl) { if (confirm(t('The sender wants you to confirm on a web page. Open it now?'))) api('open', { url: r.openUrl }).catch(H.err); }
        else toast('Done. You asked the sender to stop. It can take a few days.');
      } catch (e) { H.err(e); }
    };
    slot.appendChild(b);
  }
  if (m.receiptTo && !m.draft) H.receipt(slot, m);
  if (m.hasInvite) H.invite(slot, m);
  if (isProtected(m)) H.secure(slot, m);
};

// ---------- read receipts ----------
const RK = 'halyard.receipts';
const receiptDone = id => { try { return (JSON.parse(localStorage.getItem(RK) || '[]')).includes(id); } catch (e) { return false; } };
const receiptMark = id => { try { const l = JSON.parse(localStorage.getItem(RK) || '[]'); l.push(id); localStorage.setItem(RK, JSON.stringify(l.slice(-2000))); } catch (e) {} };
H.receipt = function (slot, m) {
  const pol = S.data.settings.receipts || 'ask';
  if (receiptDone(m.id) || H.cache['rcpt:' + m.id] || pol === 'never') return;
  const send = async quiet => {
    H.cache['rcpt:' + m.id] = true; receiptMark(m.id);
    try { await api('receipt', { id: m.id }); if (!quiet) toast('Receipt sent.'); } catch (e) { H.err(e); }
    refreshSoon();
  };
  if (pol === 'always') { send(true); return; }
  const b = bar('', 'The sender asked to know when you read this. <button class="btn" data-yes>Send a receipt</button><button class="btn" data-no>Not this time</button>');
  $('[data-yes]', b).onclick = () => { b.remove(); send(); };
  $('[data-no]', b).onclick = () => { H.cache['rcpt:' + m.id] = true; receiptMark(m.id); b.remove(); };
  slot.appendChild(b);
};

// ---------- calendar invitations ----------
function when(ev) {
  const s = new Date(ev.start), e = new Date(ev.end);
  if (ev.allDay) return s.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' }) + ' (all day)';
  const day = s.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });
  const t = d => d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  return day + ', ' + t(s) + (s.toDateString() === e.toDateString() ? ' to ' + t(e) : ' to ' + e.toLocaleString());
}
H.invite = function (slot, m) {
  const host = bar('invite', '<span class="spin"></span>');
  slot.appendChild(host);
  cacheGet('inv:' + m.id, () => api('invite?id=' + encodeURIComponent(m.id)).catch(e => ({ error: e.message }))).then(inv => {
    if (inv.error || !inv.events) { host.remove(); return; }
    const ev = inv.events[0];
    const me = inv.me && inv.me.partStat;
    const cancel = inv.method === 'CANCEL', reply = inv.method === 'REPLY';
    const att = (ev.attendees || []).map(a => '<span class="chip" title="' + esc(a.email) + '">' + esc(a.name || a.email) + (a.partStat && a.partStat !== 'NEEDS-ACTION' ? ' &middot; ' + esc(a.partStat.toLowerCase()) : '') + '</span>').join(' ');
    host.innerHTML = '<div class="invtitle">' + esc(cancel ? 'Cancelled: ' : '') + esc(ev.summary || 'Event') + '</div><div>' + esc(when(ev)) + '</div>' + (ev.location ? '<div class="pill">' + esc(ev.location) + '</div>' : '') +
      (ev.organizer && ev.organizer.email ? '<div class="pill">' + esc(t('Organizer: {x}', { x: ev.organizer.name || ev.organizer.email })) + '</div>' : '') + (ev.recurrence ? '<div class="pill">' + esc(t('Repeats: {x}', { x: ev.recurrence })) + '</div>' : '') + (att ? '<div style="margin-top:4px">' + att + '</div>' : '') +
      (reply ? '<div class="pill">This is an answer to an invitation.</div>' : cancel ? '' :
        '<div class="invbtns">' + [['ACCEPTED', 'Yes'], ['TENTATIVE', 'Maybe'], ['DECLINED', 'No']].map(([s, l]) => '<button class="btn ' + (me === s ? 'primary' : '') + '" data-rsvp="' + s + '">' + l + '</button>').join('') +
          (davAccountsFor(m) ? '<button class="btn" data-addcal>Add to calendar</button>' : '') + '</div>' + (me && me !== 'NEEDS-ACTION' ? '<div class="pill">' + esc(t('You answered: {x}', { x: t(({ ACCEPTED: 'yes', DECLINED: 'no', TENTATIVE: 'maybe' })[me] || me.toLowerCase()) })) + '</div>' : ''));
    $$('[data-rsvp]', host).forEach(b => b.onclick = async () => {
      b.disabled = true;
      try { await api('invite/respond', { id: m.id, status: b.dataset.rsvp }); delete H.cache['inv:' + m.id]; H.cache['inv:' + m.id] = Promise.resolve(Object.assign({}, inv, { me: Object.assign({}, inv.me || {}, { partStat: b.dataset.rsvp }) })); toast(t('Your answer was sent to {x}.', { x: ev.organizer.name || ev.organizer.email })); render(); } catch (e) { b.disabled = false; H.err(e); }
    });
    const ac = $('[data-addcal]', host);
    if (ac) ac.onclick = async () => { try { await api('invite/add', { id: m.id, accountId: davAccountsFor(m) }); toast('Added to your calendar.'); } catch (e) { H.err(e); } };
  });
};
function davAccountsFor(m) {
  const own = acctOf(m.accountId);
  if (own && own.settings && own.settings.dav_url) return own.id;
  const any = S.data.accounts.find(a => a.settings && a.settings.dav_url);
  return any ? any.id : '';
}

// ---------- signatures and encryption ----------
H.secure = function (slot, m) {
  const host = bar('', '<span class="spin"></span> Checking...');
  slot.appendChild(host);
  cacheGet('sec:' + m.id, () => api('security?id=' + encodeURIComponent(m.id)).catch(e => ({ problem: e.message, failed: true }))).then(v => {
    if (v.failed) { host.className = 'mxbar warn'; host.textContent = t('Could not check this message: {x}', { x: v.problem }); return; }
    const parts = [];
    let cls = 'ok';
    if (v.encrypted) parts.push(v.decrypted ? t('Encrypted: only you and the people it was sent to can read it.') : t('Encrypted, and this app cannot open it: {x}', { x: v.problem || t('no key') }));
    if (!v.encrypted || v.decrypted) {
      if (v.signed) {
        if (v.valid && v.signerMatchesFrom) parts.push(esc(t(v.trusted ? 'Signed by {x}. The message has not been changed.' : 'Signed by {x}. The message has not been changed. You have not saved this key yet.', { x: (v.signer.name ? v.signer.name + ' ' : '') + '<' + v.signer.email + '>' })));
        else if (v.valid) { parts.push(esc(t('Signed by {x}, who is not the sender of this message.', { x: v.signer.email }))); cls = 'warn'; }
        else { parts.push(esc(t('The signature could not be trusted: {x}', { x: v.problem || t('it does not match') }))); cls = 'warn'; }
        if (v.valid && !v.trusted) parts.push('<button class="btn" data-keep>Keep this key</button>');
      }
    }
    if (v.encrypted && !v.decrypted) cls = 'warn';
    host.className = 'mxbar ' + cls;
    host.innerHTML = '<b>' + (cls === 'ok' ? '&#10003;' : '!') + '</b> ' + parts.join(' ');
    const keep = $('[data-keep]', host);
    if (keep) keep.onclick = async () => { try { await api('security/keep', { id: m.id }); toast('Key saved.'); delete H.cache['sec:' + m.id]; render(); } catch (e) { H.err(e); } };
    // the readable text of a decrypted message replaces the unreadable one
    if (v.decrypted && !H.cache['dec:' + m.id]) {
      H.cache['dec:' + m.id] = true;
      m.bodyText = v.text || ''; m.bodyHtml = v.html || ''; m.attachments = (v.attachments || []).map(a => Object.assign({}, a, { secure: true }));
      render();
    }
  });
};

// ---------- print, source, save ----------
H.printMsgs = function (msgs) {
  const f = document.createElement('iframe'); f.setAttribute('sandbox', 'allow-same-origin allow-modals'); f.style.cssText = 'position:fixed;left:-9999px;width:800px;height:600px;border:0';
  const body = msgs.map(m => {
    const b = bodyDoc(m).doc; const inner = b.substring(b.indexOf('<body'), b.lastIndexOf('</body>')).replace(/^<body[^>]*>/, '');
    return '<div class="pm"><table class="ph"><tr><td>From</td><td>' + esc(addrStr(m.from)) + '</td></tr><tr><td>To</td><td>' + esc((m.to || []).map(addrStr).join(', ')) + '</td></tr>' + ((m.cc || []).length ? '<tr><td>Cc</td><td>' + esc(m.cc.map(addrStr).join(', ')) + '</td></tr>' : '') + '<tr><td>Date</td><td>' + esc(fmtFull(m.date)) + '</td></tr><tr><td>Subject</td><td><b>' + esc(m.subject) + '</b></td></tr></table><div>' + inner + '</div></div>';
  }).join('<hr>');
  const csp = "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'";
  f.srcdoc = '<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="' + csp + '"><title>' + esc((msgs[0] || {}).subject || 'Message') + '</title><style>body{font:13px/1.45 -apple-system,Segoe UI,Roboto,Arial,sans-serif;color:#000;margin:24px}.ph{border-collapse:collapse;margin-bottom:12px}.ph td{padding:1px 10px 1px 0;vertical-align:top}.ph td:first-child{color:#555}hr{border:0;border-top:1px solid #999;margin:18px 0}img{max-width:100%}pre{white-space:pre-wrap}</style></head><body>' + body + '</body></html>';
  f.onload = () => { try { f.contentWindow.focus(); f.contentWindow.print(); } catch (e) { H.err(e); } setTimeout(() => f.remove(), 60000); };
  document.body.appendChild(f);
};
H.printOpen = function () { if (S.open) H.printMsgs(S.open.msgs.filter(m => S.expanded.has(m.id))); };
H.lastMsg = m => m || (S.open && (S.open.msgs.filter(x => S.expanded.has(x.id)).pop() || S.open.msgs[S.open.msgs.length - 1]));
H.viewSource = async function (m) {
  m = H.lastMsg(m); if (!m) return;
  try {
    const r = await fetch('/api/source?id=' + encodeURIComponent(m.id) + tokQ()); const t = await r.text();
    if (!r.ok) throw new Error(JSON.parse(t).error || 'could not read the source');
    dialog('<h2>Message source</h2><textarea readonly rows="22" style="width:100%;font-family:var(--font-mono);font-size:12px" spellcheck="false">' + esc(t) + '</textarea><div class="btns"><button class="btn primary" data-close>Close</button></div>');
  } catch (e) { H.err(e); }
};
function download(url, name) { const a = document.createElement('a'); a.href = url; a.download = name || ''; document.body.appendChild(a); a.click(); a.remove(); }
H.saveEml = function (m) { m = H.lastMsg(m); if (m) download('/api/export/eml?id=' + encodeURIComponent(m.id) + tokQ(), (m.subject || 'message') + '.eml'); };
H.exportMbox = function () {
  const ids = S.open ? [S.open.id] : [...S.sel];
  if (!ids.length) { toast('Select some conversations first.'); return; }
  download('/api/export/mbox?threads=' + encodeURIComponent(ids.join(',')) + tokQ(), 'mail.mbox');
};

// ---------- folder tools ----------
function currentFolders() {
  if (S.q) return [];
  if (S.folder.startsWith('role:')) return S.data.folders.filter(f => 'role:' + f.role === S.folder && (!S.account || f.accountId === S.account));
  const f = S.data.folders.find(f => f.id === S.folder); return f ? [f] : [];
}
H.markAllRead = async function () {
  const fs = currentFolders();
  if (!fs.length) { toast('Open a folder first.'); return; }
  let n = 0;
  try { for (const f of fs) { const r = await api('folders/markread', { accountId: f.accountId, remoteId: f.remoteId }); n += r.count; } toast(n ? t('{n} message(s) marked as read.', { n }) : t('Nothing was unread.')); refreshSoon(); } catch (e) { H.err(e); }
};
H.emptyFolder = async function (role) {
  const fs = S.data.folders.filter(f => f.role === role);
  if (!fs.length) { toast(t(role === 'trash' ? 'There is no trash folder.' : 'There is no spam folder.')); return; }
  if (!confirm(t(role === 'trash' ? 'Delete everything in the trash for good? This cannot be undone.' : 'Delete everything in spam for good? This cannot be undone.'))) return;
  let n = 0;
  try { for (const f of fs) { const r = await api('folders/empty', { accountId: f.accountId, role }); n += r.count; } toast(t('{n} message(s) deleted for good.', { n })); refreshSoon(); } catch (e) { H.err(e); }
};
H.renameFolder = async function (f) { const n = prompt(t('New name for "{x}"', { x: f.name }), f.name); if (!n || n === f.name) return; try { await api('folders/rename', { accountId: f.accountId, remoteId: f.remoteId, name: n }); toast('Renamed.'); setTimeout(refreshSoon, 1200); } catch (e) { H.err(e); } };
H.deleteFolder = async function (f) { if (!confirm(t('Delete the folder "{x}"? Mail only in this folder is deleted with it.', { x: f.name }))) return; try { await api('folders/delete', { accountId: f.accountId, remoteId: f.remoteId }); if (S.folder === f.id) S.folder = 'role:inbox'; toast('Deleted.'); setTimeout(refreshSoon, 1200); } catch (e) { H.err(e); } };

// ---------- the sidebar and the toolbar ----------
H.searches = [];
H.loadSearches = async () => { try { H.searches = await api('searches'); } catch (e) { H.searches = []; } };
H.sideExtras = function () {
  let h = '';
  if ((H.searches || []).length) h += '<div class="sect">Saved searches</div>' + H.searches.map(s => '<button class="nav" data-ss="' + esc(s.id) + '">' + IC.search + '<span>' + esc(s.name) + '</span></button>').join('');
  h += '<div class="sect">Tools</div><button class="nav" data-tool="contacts">' + IC.mail + '<span>Contacts</span></button><button class="nav" data-tool="calendar">' + IC.clock + '<span>Calendar</span></button>';
  return h;
};
H.decorateSide = function () {
  const side = $('.side'); if (!side) return;
  side.insertAdjacentHTML('beforeend', H.sideExtras());
  $$('[data-ss]', side).forEach(b => {
    const s = H.searches.find(x => x.id === b.dataset.ss);
    b.onclick = () => search(s.query);
    b.oncontextmenu = e => { e.preventDefault(); if (confirm(t('Remove the saved search "{x}"?', { x: s.name }))) api('searches/delete', { id: s.id }).then(() => H.loadSearches()).then(render); };
  });
  $$('[data-tool]', side).forEach(b => b.onclick = () => (b.dataset.tool === 'contacts' ? H.contactsDialog() : H.calendarDialog()));
  $$('.nav[data-nav]', side).forEach(b => {
    const f = S.data.folders.find(f => f.id === b.dataset.nav && f.role === 'custom'); if (!f) return;
    b.oncontextmenu = ev => {
      ev.preventDefault();
      popup({ currentTarget: b }, '<button data-a="read">Mark all as read</button><button data-a="rename">Rename</button><button data-a="del">Delete</button>', pop => {
        $$('[data-a]', pop).forEach(x => x.onclick = () => { closePop(); const a = x.dataset.a; if (a === 'read') { S.folder = f.id; H.markAllRead(); } else if (a === 'rename') H.renameFolder(f); else H.deleteFolder(f); });
      });
    };
  });
};
H.decorateToolbar = function () {
  const tb = $('.toolbar'); if (!tb || S.sel.size) return;
  const b = document.createElement('button'); b.className = 'icon'; b.title = 'More'; b.setAttribute('aria-label', 'More'); b.innerHTML = '&#8943;'; b.style.fontSize = '20px';
  const sp = $('.spacer', tb); if (sp) sp.after(b); else { const s2 = document.createElement('span'); s2.className = 'spacer'; tb.appendChild(s2); tb.appendChild(b); }
  b.onclick = ev => {
    const items = [];
    if (S.open) items.push(['print', 'Print'], ['source', 'View source'], ['eml', 'Save as a file'], ['mbox', 'Save the conversation (mbox)']);
    else {
      items.push(['markall', 'Mark all as read']);
      if (S.folder === 'role:trash') items.push(['empty-trash', 'Empty the trash now']);
      if (S.folder === 'role:spam') items.push(['empty-spam', 'Empty spam now']);
      if (S.q) items.push(['savesearch', 'Save this search']);
      const f = S.data.folders.find(f => f.id === S.folder && f.role === 'custom'); if (f) items.push(['rename', 'Rename this folder'], ['delfolder', 'Delete this folder']);
      items.push(['conv', S.data.settings.conversations === 'false' ? 'Group into conversations' : 'List each message on its own']);
    }
    popup(ev, items.map(i => '<button data-a="' + i[0] + '">' + esc(i[1]) + '</button>').join(''), pop => $$('[data-a]', pop).forEach(x => x.onclick = () => {
      closePop(); const a = x.dataset.a;
      const f = S.data.folders.find(f => f.id === S.folder);
      ({ print: H.printOpen, source: () => H.viewSource(), eml: () => H.saveEml(), mbox: H.exportMbox, markall: H.markAllRead, 'empty-trash': () => H.emptyFolder('trash'), 'empty-spam': () => H.emptyFolder('spam'),
        savesearch: H.saveCurrentSearch, rename: () => H.renameFolder(f), delfolder: () => H.deleteFolder(f), conv: () => H.setSetting({ conversations: S.data.settings.conversations === 'false' ? 'true' : 'false' }, true) })[a]();
    }));
  };
};
H.saveCurrentSearch = async function () {
  if (!S.q) { toast('Search for something first.'); return; }
  const name = prompt(t('Name for this search'), S.q); if (!name) return;
  try { await api('searches', { name, query: S.q }); await H.loadSearches(); render(); toast('Search saved.'); } catch (e) { H.err(e); }
};
