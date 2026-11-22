'use strict';
/* The screens for everything past reading and writing mail: filters, templates, identities, vacation reply, server
   filters, keys, calendar and contacts servers, import and export, and the rest of the settings. */

const LANGS = [['auto', 'Same as the system'], ['en', 'English'], ['es', 'Español'], ['fr', 'Français'], ['it', 'Italiano'], ['de', 'Deutsch'], ['nb', 'Norsk bokmål'], ['bg', 'Български'], ['el', 'Ελληνικά'], ['ko', '한국어'], ['ja', '日本語'], ['vi', 'Tiếng Việt'], ['hi', 'हिन्दी'], ['zh-Hans', '简体中文'], ['ar', 'العربية']];
H.LANGS = LANGS;

const opt = (v, cur, t) => '<option value="' + esc(v) + '"' + (String(cur) === String(v) ? ' selected' : '') + '>' + esc(t) + '</option>';
const acctSelect = (id, cur) => '<select id="' + id + '">' + S.data.accounts.map(a => opt(a.id, cur || S.data.accounts[0].id, a.email)).join('') + '</select>';
const lbl = (t, inner) => '<label class="l">' + t + '</label>' + inner;
const errTo = e => toast(e.message || String(e), { err: true });
H.err = errTo;
function localDT(ms) { if (!ms) return ''; const d = new Date(ms); d.setMinutes(d.getMinutes() - d.getTimezoneOffset()); return d.toISOString().slice(0, 16); }
H.localDT = localDT;
async function caps(acct) { try { return await api('capabilities?account=' + encodeURIComponent(acct)); } catch (e) { return {}; } }
const needAccount = () => { if (!S.data.accounts.length) { toast('Add an account first.', { err: true }); return true; } return false; };

// ---------- more settings ----------
H.generalDialog = function () {
  const s = S.data.settings;
  dialog('<h2>More settings</h2><div class="row2">' +
    lbl('Language', '<select id="g_lang">' + LANGS.map(l => opt(l[0], s.language, l[1])).join('') + '</select>') +
    lbl('Read receipts, when a sender asks for one', '<select id="g_rcpt">' + opt('ask', s.receipts, 'Ask me each time') + opt('always', s.receipts, 'Always send') + opt('never', s.receipts, 'Never send') + '</select>') +
    '<label><input type="checkbox" id="g_conv" style="width:auto"> Group messages into conversations</label>' +
    '<label><input type="checkbox" id="g_spell" style="width:auto"> Check spelling while writing</label>' +
    '<label><input type="checkbox" id="g_arch" style="width:auto"> Archive a conversation after I reply to it</label>' +
    '<label><input type="checkbox" id="g_tray" style="width:auto"> Keep an icon in the notification area, and keep running when the window is closed</label>' +
    '<label><input type="checkbox" id="g_min" style="width:auto"> Start hidden in the notification area</label>' +
    '<label><input type="checkbox" id="g_sign" style="width:auto"> Sign every message I send when I have a key</label>' +
    '<label><input type="checkbox" id="g_enc" style="width:auto"> Encrypt when I have a key for every recipient</label>' +
    lbl('Protect messages with', '<select id="g_prot">' + opt('pgp', s.defaultProtect, 'OpenPGP') + opt('smime', s.defaultProtect, 'S/MIME') + '</select>') +
    '</div><div class="btns"><button class="btn" data-close>Cancel</button><button class="btn primary" id="g_save">Save</button></div>', el => {
    $('#g_conv', el).checked = s.conversations !== 'false'; $('#g_spell', el).checked = s.spellcheck !== 'false'; $('#g_arch', el).checked = s.archiveAfterReply === 'true';
    $('#g_tray', el).checked = s.tray !== 'false'; $('#g_min', el).checked = s.startMinimized === 'true'; $('#g_sign', el).checked = s.signAll === 'true'; $('#g_enc', el).checked = s.encryptWhenPossible === 'true';
    $('#g_save', el).onclick = async () => {
      try {
        await api('settings', { language: $('#g_lang', el).value, receipts: $('#g_rcpt', el).value, conversations: String($('#g_conv', el).checked), spellcheck: String($('#g_spell', el).checked),
          archiveAfterReply: String($('#g_arch', el).checked), tray: String($('#g_tray', el).checked), startMinimized: String($('#g_min', el).checked), signAll: String($('#g_sign', el).checked),
          encryptWhenPossible: String($('#g_enc', el).checked), defaultProtect: $('#g_prot', el).value });
        await refreshState(); el.remove(); await loadThreads(false); render();
        if (H.i18n) H.i18n.set(S.data.settings.language);
        toast('Saved. The notification area icon changes the next time the app starts.');
      } catch (e) { errTo(e); }
    };
  });
};

// ---------- identities and signatures ----------
H.identitiesDialog = async function () {
  if (needAccount()) return;
  const el = dialog('<h2>Identities and signatures</h2><div id="idbody"><span class="spin"></span></div><div class="btns"><button class="btn primary" data-close>Done</button></div>');
  const draw = async () => {
    const all = await api('identities');
    const body = $('#idbody', el);
    body.innerHTML = S.data.accounts.map(a => {
      const ids = all.filter(i => i.accountId === a.id);
      return '<div class="card"><div style="display:flex;align-items:center;gap:8px"><b>' + esc(a.email) + '</b><span class="spacer"></span><button class="btn" data-refresh="' + esc(a.id) + '">Get from the server</button><button class="btn" data-add="' + esc(a.id) + '">Add an address</button></div>' +
        ids.map(i => '<div class="idrow" data-id="' + esc(i.id) + '"><div class="grid2" style="grid-template-columns:1fr 1fr">' + lbl('Name', '<input data-f="name" value="' + esc(i.name) + '">') + lbl('Address', '<input data-f="email" value="' + esc(i.email) + '">') + '</div>' +
          lbl('Reply-to address (optional)', '<input data-f="replyTo" value="' + esc(i.replyTo || '') + '">') + lbl('Signature', '<textarea rows="3" data-f="signature">' + esc(i.signature || '') + '</textarea>') +
          '<div style="display:flex;gap:10px;align-items:center;margin-top:4px"><label><input type="checkbox" data-f="default" style="width:auto" ' + (i.default ? 'checked' : '') + '> ' + esc(t('Use as the default for {x}', { x: a.email })) + '</label>' + (i.alias ? '<span class="pill">alias</span>' : '') +
          '<span class="spacer"></span><button class="btn" data-save="' + esc(i.id) + '">Save</button>' + (i.source === 'provider' && i.default ? '' : '<button class="btn danger" data-del="' + esc(i.id) + '">Remove</button>') + '</div></div>').join('') + '</div>';
    }).join('');
    const rowData = id => { const r = $('[data-id="' + id + '"]', el); const g = k => $('[data-f=' + k + ']', r); return { id, name: g('name').value, email: g('email').value.trim(), replyTo: g('replyTo').value.trim(), signature: g('signature').value, default: g('default').checked }; };
    $$('[data-save]', el).forEach(b => b.onclick = async () => {
      const cur = all.find(i => i.id === b.dataset.save);
      try { await api('identities', Object.assign({}, cur, rowData(b.dataset.save))); await refreshState(); toast('Saved'); draw(); } catch (e) { errTo(e); }
    });
    $$('[data-del]', el).forEach(b => b.onclick = async () => { try { await api('identities/delete', { id: b.dataset.del }); draw(); } catch (e) { errTo(e); } });
    $$('[data-add]', el).forEach(b => b.onclick = async () => {
      const a = acctOf(b.dataset.add);
      const email = prompt(t('The address to send from. Your server must allow it.'));
      if (!email) return;
      try { await api('identities', { accountId: a.id, name: a.name, email: email.trim(), signature: '', alias: true, source: 'user' }); draw(); } catch (e) { errTo(e); }
    });
    $$('[data-refresh]', el).forEach(b => b.onclick = async () => {
      b.innerHTML = '<span class="spin"></span>';
      try { await api('identities/refresh', { accountId: b.dataset.refresh }); toast('Addresses updated from the server.'); } catch (e) { errTo(e); }
      draw();
    });
  };
  draw().catch(errTo);
};

// ---------- templates ----------
H.templatesList = [];
H.loadTemplates = async () => { try { H.templatesList = await api('templates'); } catch (e) { H.templatesList = []; } return H.templatesList; };
H.templatesDialog = async function () {
  const el = dialog('<h2>Templates</h2><p class="pill">A template is a reply you use again and again. Choose it in the composer, or from the command palette.</p><div id="tpbody"></div><div class="btns"><button class="btn" id="tpnew">New template</button><button class="btn primary" data-close>Done</button></div>');
  const draw = async () => {
    const list = await H.loadTemplates();
    $('#tpbody', el).innerHTML = list.length ? list.map(t => '<div class="card"><div style="display:flex;gap:8px;align-items:center"><b>' + esc(t.name) + '</b><span class="pill" style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + esc(t.subject) + '</span><button class="btn" data-edit="' + esc(t.id) + '">Edit</button><button class="btn danger" data-del="' + esc(t.id) + '">Delete</button></div></div>').join('') : '<p>No templates yet.</p>';
    $$('[data-edit]', el).forEach(b => b.onclick = () => edit(list.find(t => t.id === b.dataset.edit)));
    $$('[data-del]', el).forEach(b => b.onclick = async () => { try { await api('templates/delete', { id: b.dataset.del }); draw(); } catch (e) { errTo(e); } });
  };
  const edit = t => {
    t = t || {};
    const ed = dialog('<h2>' + (t.id ? 'Edit the template' : 'New template') + '</h2><div class="row2">' + lbl('Name', '<input id="tn" value="' + esc(t.name || '') + '">') + lbl('Subject (optional)', '<input id="ts" value="' + esc(t.subject || '') + '">') + lbl('Text', '<textarea id="tb" rows="8">' + esc(t.body || '') + '</textarea>') +
      '</div><div class="btns"><button class="btn" data-close>Cancel</button><button class="btn primary" id="tsave">Save</button></div>', e2 => {
      $('#tsave', e2).onclick = async () => { try { await api('templates', { id: t.id || '', name: $('#tn', e2).value.trim(), subject: $('#ts', e2).value, body: $('#tb', e2).value }); e2.remove(); draw(); } catch (e) { errTo(e); } };
    });
    ed.style.zIndex = 70;
  };
  $('#tpnew', el).onclick = () => edit();
  draw().catch(errTo);
};
H.composeFromTemplate = function (t) { openComposer({ mode: 'new', subject: t.subject || '', html: '<div>' + esc(t.body || '').replace(/\n/g, '<br>') + '</div>' }); };

// ---------- filters (rules) ----------
const FIELDS = [['from', 'From'], ['to', 'To'], ['cc', 'Cc'], ['subject', 'Subject'], ['body', 'Text of the message'], ['has_attachment', 'Has an attachment'], ['has_unsubscribe', 'Is a mailing list'], ['size_over', 'Larger than (bytes)'], ['size_under', 'Smaller than (bytes)']];
const OPS = [['contains', 'contains'], ['not_contains', 'does not contain'], ['equals', 'is'], ['starts', 'starts with'], ['ends', 'ends with'], ['matches', 'matches the pattern']];
const KINDS = [['move', 'Move to a folder'], ['label', 'Add a label or copy to a folder'], ['archive', 'Archive'], ['read', 'Mark as read'], ['star', 'Star'], ['trash', 'Delete'], ['spam', 'Mark as spam'], ['forward', 'Forward to']];
H.rulesDialog = async function () {
  if (needAccount()) return;
  const el = dialog('<h2>Filters</h2><p class="pill">Filters run on new mail as it arrives, in this order. A filter works on every account unless you choose one.</p><div id="rlbody"></div><div class="btns"><button class="btn" id="rlnew">New filter</button><button class="btn" id="rlrun">Run on the inbox now</button><button class="btn primary" data-close>Done</button></div>');
  const draw = async () => {
    const rs = await api('rules');
    $('#rlbody', el).innerHTML = rs.length ? rs.map((r, i) => '<div class="card"><div style="display:flex;gap:8px;align-items:center"><label style="flex:1"><input type="checkbox" data-on="' + esc(r.id) + '" style="width:auto" ' + (r.enabled ? 'checked' : '') + '> <b>' + esc(r.name) + '</b></label>' +
      '<button class="icon" data-up="' + esc(r.id) + '" data-i="' + i + '" title="Run earlier" aria-label="Run earlier">&#8593;</button><button class="icon" data-down="' + esc(r.id) + '" data-i="' + i + '" title="Run later" aria-label="Run later">&#8595;</button>' +
      '<button class="btn" data-edit="' + esc(r.id) + '">Edit</button><button class="btn danger" data-del="' + esc(r.id) + '">Delete</button></div><div class="pill">' + esc((r.conditions || []).map(c => (FIELDS.find(f => f[0] === c.field) || [0, c.field])[1] + ' ' + (['has_attachment', 'has_unsubscribe', 'size_over', 'size_under'].includes(c.field) ? '' : (OPS.find(o => o[0] === c.op) || [0, c.op])[1] + ' ') + c.value).join(r.match === 'any' ? '  or  ' : '  and  ')) + '</div></div>').join('') : '<p>No filters yet.</p>';
    $$('[data-on]', el).forEach(c => c.onchange = async () => { const r = rs.find(r => r.id === c.dataset.on); try { await api('rules', Object.assign({}, r, { enabled: c.checked })); } catch (e) { errTo(e); } });
    $$('[data-up]', el).forEach(b => b.onclick = async () => { await api('rules/move', { id: b.dataset.up, index: Math.max(0, +b.dataset.i - 1) }); draw(); });
    $$('[data-down]', el).forEach(b => b.onclick = async () => { await api('rules/move', { id: b.dataset.down, index: +b.dataset.i + 1 }); draw(); });
    $$('[data-del]', el).forEach(b => b.onclick = async () => { await api('rules/delete', { id: b.dataset.del }); draw(); });
    $$('[data-edit]', el).forEach(b => b.onclick = () => edit(rs.find(r => r.id === b.dataset.edit)));
  };
  const edit = r => {
    r = r || { name: '', enabled: true, match: 'all', conditions: [{ field: 'from', op: 'contains', value: '' }], actions: [{ kind: 'archive' }] };
    const ed = dialog('<h2>' + (r.id ? 'Edit the filter' : 'New filter') + '</h2><div class="row2">' + lbl('Name', '<input id="rn" value="' + esc(r.name) + '">') +
      lbl('Account', '<select id="ra"><option value="">All accounts</option>' + S.data.accounts.map(a => opt(a.id, r.accountId || '', a.email)).join('') + '</select>') +
      '<div>When <select id="rm" style="width:auto">' + opt('all', r.match, 'all') + opt('any', r.match, 'any') + '</select> of these are true</div><div id="rc"></div><button class="btn" id="rcadd" style="justify-self:start">Add a condition</button>' +
      '<div>Then do</div><div id="rx"></div><button class="btn" id="rxadd" style="justify-self:start">Add an action</button><label><input type="checkbox" id="rs" style="width:auto"> Stop here: later filters do not run on a message this one caught</label></div>' +
      '<div class="btns"><button class="btn" data-close>Cancel</button><button class="btn primary" id="rsave">Save</button></div>', e2 => {
      const conds = JSON.parse(JSON.stringify(r.conditions || [])), acts = JSON.parse(JSON.stringify(r.actions || []));
      $('#rs', e2).checked = !!r.stop;
      const folders = () => S.data.folders.filter(f => !['starred', 'all', 'drafts', 'sent'].includes(f.role));
      const folderOpts = (cur, acct) => folders().filter(f => !$('#ra', e2).value || f.accountId === $('#ra', e2).value).map(f => '<option value="' + esc(f.remoteId) + '"' + (f.remoteId === cur ? ' selected' : '') + '>' + esc(f.name + (S.data.accounts.length > 1 ? ' (' + ((acctOf(f.accountId) || {}).email || '') + ')' : '')) + '</option>').join('');
      const drawC = () => {
        $('#rc', e2).innerHTML = conds.map((c, i) => '<div class="grid3"><select data-cf="' + i + '">' + FIELDS.map(f => opt(f[0], c.field, f[1])).join('') + '</select>' +
          (['has_attachment', 'has_unsubscribe'].includes(c.field) ? '<span class="pill">yes</span><input data-cv="' + i + '" type="hidden" value="true">' : ['size_over', 'size_under'].includes(c.field) ? '<span></span><input data-cv="' + i + '" value="' + esc(c.value) + '">' : '<select data-co="' + i + '">' + OPS.map(o => opt(o[0], c.op || 'contains', o[1])).join('') + '</select><input data-cv="' + i + '" value="' + esc(c.value) + '">') +
          '<button class="icon" data-cx="' + i + '" aria-label="Remove">&#10005;</button></div>').join('');
        $$('[data-cf]', e2).forEach(s => s.onchange = () => { conds[+s.dataset.cf].field = s.value; conds[+s.dataset.cf].value = ''; drawC(); });
        $$('[data-co]', e2).forEach(s => s.onchange = () => conds[+s.dataset.co].op = s.value);
        $$('[data-cv]', e2).forEach(s => s.oninput = () => conds[+s.dataset.cv].value = s.value);
        $$('[data-cx]', e2).forEach(b => b.onclick = () => { conds.splice(+b.dataset.cx, 1); drawC(); });
        $$('[data-cv]', e2).forEach(s => { const c = conds[+s.dataset.cv]; if (s.type === 'hidden') c.value = 'true'; });
      };
      const drawA = () => {
        $('#rx', e2).innerHTML = acts.map((a, i) => '<div class="grid3"><select data-ak="' + i + '">' + KINDS.map(k => opt(k[0], a.kind, k[1])).join('') + '</select>' +
          (['move', 'label'].includes(a.kind) ? '<select data-af="' + i + '">' + folderOpts(a.folderId) + '</select>' : a.kind === 'forward' ? '<input data-at="' + i + '" placeholder="name@example.com" value="' + esc(a.to || '') + '">' : '<span></span>') +
          '<button class="icon" data-ax="' + i + '" aria-label="Remove">&#10005;</button></div>').join('');
        $$('[data-ak]', e2).forEach(s => s.onchange = () => { acts[+s.dataset.ak].kind = s.value; drawA(); });
        $$('[data-af]', e2).forEach(s => { acts[+s.dataset.af].folderId = acts[+s.dataset.af].folderId || s.value; s.onchange = () => acts[+s.dataset.af].folderId = s.value; });
        $$('[data-at]', e2).forEach(s => s.oninput = () => acts[+s.dataset.at].to = s.value);
        $$('[data-ax]', e2).forEach(b => b.onclick = () => { acts.splice(+b.dataset.ax, 1); drawA(); });
      };
      $('#ra', e2).onchange = drawA;
      $('#rcadd', e2).onclick = () => { conds.push({ field: 'from', op: 'contains', value: '' }); drawC(); };
      $('#rxadd', e2).onclick = () => { acts.push({ kind: 'archive' }); drawA(); };
      drawC(); drawA();
      $('#rsave', e2).onclick = async () => {
        try {
          await api('rules', { id: r.id || '', name: $('#rn', e2).value.trim(), accountId: $('#ra', e2).value, enabled: r.enabled !== false, match: $('#rm', e2).value, conditions: conds, actions: acts, stop: $('#rs', e2).checked });
          e2.remove(); draw();
        } catch (e) { errTo(e); }
      };
    });
    ed.style.zIndex = 70;
  };
  $('#rlnew', el).onclick = () => edit();
  $('#rlrun', el).onclick = async () => {
    const a = S.data.accounts.length === 1 ? S.data.accounts[0].id : (S.account || prompt(t('Account id to run on ({x})', { x: S.data.accounts.map(a => a.id + ' = ' + a.email).join(', ') })));
    if (!a) return;
    try { const r = await api('rules/run', { accountId: a }); toast(t('{n} message(s) matched a filter.', { n: r.matched })); refreshSoon(); } catch (e) { errTo(e); }
  };
  draw().catch(errTo);
};

// ---------- vacation reply ----------
H.vacationDialog = async function () {
  if (needAccount()) return;
  const el = dialog('<h2>Vacation reply</h2><div class="row2">' + lbl('Account', acctSelect('vc_acct', S.account)) + '<div id="vcbody"><span class="spin"></span></div></div><div class="btns"><button class="btn" data-close>Close</button><button class="btn primary" id="vcsave">Save</button></div>');
  const load = async () => {
    const id = $('#vc_acct', el).value, body = $('#vcbody', el);
    const c = await caps(id);
    if (!c.vacation) { body.innerHTML = '<p>This kind of account keeps no automatic reply on its server. Gmail, Microsoft, JMAP and IMAP accounts with a filter service do.</p>'; $('#vcsave', el).disabled = true; return; }
    $('#vcsave', el).disabled = false;
    try {
      const v = await api('vacation?account=' + encodeURIComponent(id));
      body.innerHTML = '<label><input type="checkbox" id="vc_on" style="width:auto"> Send an automatic reply</label>' + lbl('Subject', '<input id="vc_s" value="' + esc(v.subject || '') + '">') + lbl('Message', '<textarea id="vc_b" rows="6">' + esc(v.body || '') + '</textarea>') +
        '<div class="grid2" style="grid-template-columns:1fr 1fr">' + lbl('First day (optional)', '<input type="datetime-local" id="vc_f" value="' + localDT(v.start && !v.start.startsWith('0001') ? Date.parse(v.start) : 0) + '">') + lbl('Last day (optional)', '<input type="datetime-local" id="vc_t" value="' + localDT(v.end && !v.end.startsWith('0001') ? Date.parse(v.end) : 0) + '">') + '</div>' +
        '<label><input type="checkbox" id="vc_c" style="width:auto"> Only reply to people in my contacts</label><p class="pill">The reply is kept on your server, so it answers while this app is closed.</p>';
      $('#vc_on', el).checked = !!v.enabled; $('#vc_c', el).checked = !!v.contactsOnly;
    } catch (e) { body.innerHTML = '<p>' + esc(e.message) + '</p>'; }
  };
  $('#vc_acct', el).onchange = load;
  $('#vcsave', el).onclick = async () => {
    const f = $('#vc_f', el).value, t = $('#vc_t', el).value;
    try {
      await api('vacation', { accountId: $('#vc_acct', el).value, enabled: $('#vc_on', el).checked, subject: $('#vc_s', el).value, body: $('#vc_b', el).value, contactsOnly: $('#vc_c', el).checked, start: f ? new Date(f).toISOString() : '0001-01-01T00:00:00Z', end: t ? new Date(t).toISOString() : '0001-01-01T00:00:00Z' });
      toast($('#vc_on', el).checked ? 'The automatic reply is on.' : 'The automatic reply is off.'); el.remove();
    } catch (e) { errTo(e); }
  };
  load();
};

// ---------- server side filters (Sieve) ----------
H.sieveDialog = async function () {
  const accts = S.data.accounts.filter(a => a.kind === 'imap');
  if (!accts.length) { toast('Server filters need an IMAP account.', { err: true }); return; }
  const el = dialog('<h2>Server filters (Sieve)</h2><p class="pill">These scripts run on your mail server, so they work while this app is closed. The server checks each script before it keeps it.</p><div class="row2">' + lbl('Account', '<select id="sv_acct">' + accts.map(a => opt(a.id, accts[0].id, a.email)).join('') + '</select>') +
    '<div id="svlist"></div>' + lbl('Script', '<textarea id="sv_text" rows="12" style="font-family:var(--font-mono);font-size:12px" spellcheck="false"></textarea>') + '<div style="display:flex;gap:8px;align-items:center">' + lbl('Name', '<input id="sv_name" style="width:200px">') + '<div id="sv_msg" class="pill" style="flex:1"></div></div></div>' +
    '<div class="btns"><button class="btn" id="sv_new">New script</button><button class="btn" id="sv_check">Check</button><button class="btn" id="sv_save">Save</button><button class="btn primary" id="sv_act">Save and run</button><button class="btn" data-close>Close</button></div>');
  const acct = () => $('#sv_acct', el).value, msg = (t, bad) => { const m = $('#sv_msg', el); m.textContent = t; m.style.color = bad ? 'var(--danger)' : ''; };
  const list = async () => {
    try {
      const ss = await api('sieve/scripts?account=' + encodeURIComponent(acct()));
      $('#svlist', el).innerHTML = ss.length ? ss.map(s => '<div style="display:flex;gap:8px;align-items:center"><button class="btn" data-open="' + esc(s.name) + '">' + esc(s.name) + '</button>' + (s.active ? '<span class="pill">running</span>' : '<button class="btn" data-activate="' + esc(s.name) + '">Run this one</button>') + '<button class="btn danger" data-del="' + esc(s.name) + '">Delete</button></div>').join('') : '<p>No scripts on the server yet.</p>';
      $$('[data-open]', el).forEach(b => b.onclick = async () => { try { const r = await api('sieve/script?account=' + encodeURIComponent(acct()) + '&name=' + encodeURIComponent(b.dataset.open)); $('#sv_text', el).value = r.script; $('#sv_name', el).value = r.name; msg(''); } catch (e) { errTo(e); } });
      $$('[data-activate]', el).forEach(b => b.onclick = async () => { try { await api('sieve/activate', { accountId: acct(), name: b.dataset.activate }); list(); } catch (e) { errTo(e); } });
      $$('[data-del]', el).forEach(b => b.onclick = async () => { if (confirm(t('Delete the script "{x}" from the server?', { x: b.dataset.del }))) { try { await api('sieve/delete', { accountId: acct(), name: b.dataset.del }); list(); } catch (e) { errTo(e); } } });
    } catch (e) { $('#svlist', el).innerHTML = '<p>' + esc(e.message) + '</p>'; }
  };
  $('#sv_acct', el).onchange = list;
  $('#sv_new', el).onclick = () => { $('#sv_text', el).value = 'require ["fileinto"];\n\n# if header :contains "subject" "invoice" { fileinto "Bills"; }\n'; $('#sv_name', el).value = 'my-filters'; msg(''); };
  $('#sv_check', el).onclick = async () => { try { await api('sieve/check', { accountId: acct(), script: $('#sv_text', el).value }); msg('The server accepts this script.'); } catch (e) { msg(e.message, true); } };
  const put = async run => { try { await api('sieve/put', { accountId: acct(), name: $('#sv_name', el).value.trim(), script: $('#sv_text', el).value, activate: run }); msg(run ? 'Saved and running.' : 'Saved.'); list(); } catch (e) { msg(e.message, true); } };
  $('#sv_save', el).onclick = () => put(false); $('#sv_act', el).onclick = () => put(true);
  list();
};

// ---------- keys and certificates ----------
H.securityDialog = async function () {
  const el = dialog('<h2>Encryption keys and certificates</h2><p class="pill">A key lets you sign mail, so people know it is from you, and read mail written to you in private. Give your public key to the people who write to you. Private keys never leave this computer.</p><div id="kybody"><span class="spin"></span></div>' +
    '<div class="btns"><button class="btn" id="ky_make">Make a key</button><button class="btn" id="ky_imp">Import</button><button class="btn primary" data-close>Done</button></div>');
  const draw = async () => {
    const k = await api('keys');
    const row = (kind, x) => '<div class="card" style="display:flex;gap:8px;align-items:center"><span class="tag">' + (kind === 'pgp' ? 'OpenPGP' : 'S/MIME') + '</span><div style="flex:1;min-width:0"><b>' + esc(x.name || x.email) + '</b> <span class="pill">' + esc(x.email) + '</span><div class="pill" style="overflow:hidden;text-overflow:ellipsis">' + esc((x.fingerprint || x.id)) + (x.expires ? ' &middot; expires ' + esc(new Date(x.expires).toLocaleDateString()) : '') + (x.selfSigned ? ' &middot; self signed' : '') + '</div></div>' +
      '<button class="btn" data-exp="' + esc(x.id) + '">Share the public part</button><button class="btn danger" data-rm="' + esc(x.id) + '">Remove</button></div>';
    $('#kybody', el).innerHTML = (k.pgp.length + k.smime.length) ? k.pgp.map(x => row('pgp', x)).join('') + k.smime.map(x => row('smime', x)).join('') : '<p>No keys yet.</p>';
    $$('[data-exp]', el).forEach(b => b.onclick = () => { location.href = '/api/keys/export?id=' + encodeURIComponent(b.dataset.exp) + tokQ(); });
    $$('[data-rm]', el).forEach(b => b.onclick = async () => { if (confirm(t('Remove this key? Mail encrypted to it can no longer be read here.'))) { try { await api('keys/remove', { id: b.dataset.rm }); draw(); } catch (e) { errTo(e); } } });
  };
  $('#ky_make', el).onclick = () => {
    const d2 = dialog('<h2>Make a key</h2><div class="row2">' + lbl('Type', '<select id="mk_kind">' + opt('pgp', 'pgp', 'OpenPGP') + opt('smime', 'pgp', 'S/MIME (a self signed certificate)') + '</select>') + lbl('Your name', '<input id="mk_name">') + lbl('Email address', '<select id="mk_email">' + S.data.accounts.map(a => opt(a.email, '', a.email)).join('') + '</select>') +
      '</div><div class="btns"><button class="btn" data-close>Cancel</button><button class="btn primary" id="mk_go">Make it</button></div>', e2 => {
      $('#mk_name', e2).value = (S.data.accounts[0] || {}).name || '';
      $('#mk_go', e2).onclick = async () => { const b = $('#mk_go', e2); b.innerHTML = '<span class="spin"></span>'; try { await api('keys/make', { kind: $('#mk_kind', e2).value, name: $('#mk_name', e2).value, email: $('#mk_email', e2).value }); e2.remove(); draw(); } catch (e) { b.textContent = 'Make it'; errTo(e); } };
    });
    d2.style.zIndex = 70;
  };
  $('#ky_imp', el).onclick = () => {
    const d2 = dialog('<h2>Import a key or certificate</h2><div class="row2">' + lbl('Type', '<select id="im_kind">' + opt('pgp', 'pgp', 'OpenPGP key') + opt('smime', 'pgp', 'S/MIME certificate (.pem, .cer, .p12, .pfx)') + '</select>') + lbl('File', '<input type="file" id="im_file">') + lbl('Or paste the text', '<textarea id="im_text" rows="5" style="font-family:var(--font-mono);font-size:12px"></textarea>') + lbl('Password (for a protected key or a .p12 file)', '<input type="password" id="im_pw">') +
      '</div><div class="btns"><button class="btn" data-close>Cancel</button><button class="btn primary" id="im_go">Import</button></div>', e2 => {
      $('#im_go', e2).onclick = async () => {
        const fd = new FormData(); fd.append('kind', $('#im_kind', e2).value); fd.append('password', $('#im_pw', e2).value); fd.append('text', $('#im_text', e2).value);
        if ($('#im_file', e2).files[0]) fd.append('file', $('#im_file', e2).files[0]);
        try { const r = await api('keys/import', fd); toast(t('{n} imported.', { n: r.imported })); e2.remove(); draw(); } catch (e) { errTo(e); }
      };
    });
    d2.style.zIndex = 70;
  };
  draw().catch(errTo);
};

// ---------- calendar and contacts server ----------
H.davDialog = function () {
  const accts = S.data.accounts.filter(a => !['google', 'microsoft', 'local'].includes(a.kind));
  if (!accts.length) { toast('A calendar and contacts server can be set for IMAP, POP3 and JMAP accounts.', { err: true }); return; }
  const el = dialog('<h2>Calendar and contacts server</h2><p class="pill">Most servers speak CalDAV and CardDAV on their web address. Your events and your address book then show up here.</p><div class="row2">' + lbl('Account', '<select id="dv_acct">' + accts.map(a => opt(a.id, accts[0].id, a.email)).join('') + '</select>') +
    lbl('Server address', '<input id="dv_url" placeholder="https://mail.example.com">') + lbl('User name (empty: the account\'s)', '<input id="dv_user">') + lbl('Password (empty: the account\'s)', '<input id="dv_pw" type="password" autocomplete="new-password">') + '</div>' +
    '<div class="btns"><button class="btn danger" id="dv_off">Turn off</button><button class="btn" data-close>Cancel</button><button class="btn primary" id="dv_save">Connect</button></div>', e => {
    const fill = () => { const a = acctOf($('#dv_acct', e).value); $('#dv_url', e).value = (a.settings && a.settings.dav_url) || ''; $('#dv_user', e).value = (a.settings && a.settings.dav_user) || ''; };
    $('#dv_acct', e).onchange = fill; fill();
    $('#dv_save', e).onclick = async () => { const b = $('#dv_save', e); b.innerHTML = '<span class="spin"></span>'; try { await api('dav/set', { accountId: $('#dv_acct', e).value, url: $('#dv_url', e).value.trim(), user: $('#dv_user', e).value.trim(), password: $('#dv_pw', e).value }); await refreshState(); toast('Connected.'); e.remove(); } catch (x) { b.textContent = 'Connect'; errTo(x); } };
    $('#dv_off', e).onclick = async () => { try { await api('dav/set', { accountId: $('#dv_acct', e).value, url: '' }); await refreshState(); toast('Turned off.'); e.remove(); } catch (x) { errTo(x); } };
  });
  return el;
};
const davAccounts = () => S.data.accounts.filter(a => a.settings && a.settings.dav_url);
H.calendarDialog = async function () {
  const accts = davAccounts();
  if (!accts.length) { toast('Set up a calendar server first.'); H.davDialog(); return; }
  const el = dialog('<h2>Calendar</h2><div class="row2">' + lbl('Account', '<select id="ca_acct">' + accts.map(a => opt(a.id, accts[0].id, a.email)).join('') + '</select>') + '<div id="cabody"><span class="spin"></span></div></div><div class="btns"><button class="btn" id="ca_new">New event</button><button class="btn primary" data-close>Done</button></div>');
  const draw = async () => {
    const from = new Date(); from.setHours(0, 0, 0, 0);
    try {
      const evs = await api('calendar/events?account=' + encodeURIComponent($('#ca_acct', el).value) + '&from=' + encodeURIComponent(from.toISOString()) + '&to=' + encodeURIComponent(new Date(from.getTime() + 45 * 864e5).toISOString()));
      let day = '';
      $('#cabody', el).innerHTML = evs.length ? evs.map(e => {
        const d = new Date(e.start), dd = d.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' });
        const h = (dd !== day ? '<div class="sect" style="margin:12px 0 4px">' + esc(dd) + '</div>' : ''); day = dd;
        return h + '<div class="card" style="display:flex;gap:10px;align-items:center"><span class="pill" style="width:110px">' + (e.allDay ? 'All day' : esc(d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }) + ' to ' + new Date(e.end).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }))) + '</span><div style="flex:1"><b>' + esc(e.summary) + '</b>' + (e.location ? '<div class="pill">' + esc(e.location) + '</div>' : '') + '</div><button class="btn" data-edit="' + esc(e.url) + '">Edit</button><button class="btn danger" data-del="' + esc(e.url) + '" data-tag="' + esc(e.etag) + '">Delete</button></div>';
      }).join('') : '<p>Nothing in the next 45 days.</p>';
      $$('[data-del]', el).forEach(b => b.onclick = async () => { try { await api('calendar/delete', { accountId: $('#ca_acct', el).value, url: b.dataset.del, etag: b.dataset.tag }); draw(); } catch (e) { errTo(e); } });
      $$('[data-edit]', el).forEach(b => b.onclick = () => edit(evs.find(x => x.url === b.dataset.edit)));
    } catch (e) { $('#cabody', el).innerHTML = '<p>' + esc(e.message) + '</p>'; }
  };
  const edit = ev => {
    const n = new Date(); n.setMinutes(0, 0, 0); n.setHours(n.getHours() + 1);
    ev = ev || { summary: '', start: n.toISOString(), end: new Date(n.getTime() + 36e5).toISOString() };
    const d2 = dialog('<h2>' + (ev.url ? 'Edit the event' : 'New event') + '</h2><div class="row2">' + lbl('Title', '<input id="ev_t" value="' + esc(ev.summary) + '">') + '<div class="grid2" style="grid-template-columns:1fr 1fr">' + lbl('Starts', '<input type="datetime-local" id="ev_s" value="' + localDT(Date.parse(ev.start)) + '">') + lbl('Ends', '<input type="datetime-local" id="ev_e" value="' + localDT(Date.parse(ev.end)) + '">') + '</div>' + lbl('Place', '<input id="ev_l" value="' + esc(ev.location || '') + '">') + lbl('Notes', '<textarea id="ev_d" rows="3">' + esc(ev.description || '') + '</textarea>') +
      '</div><div class="btns"><button class="btn" data-close>Cancel</button><button class="btn primary" id="ev_go">Save</button></div>', e2 => {
      $('#ev_go', e2).onclick = async () => {
        try {
          await api('calendar/save', { accountId: $('#ca_acct', el).value, event: Object.assign({}, ev, { summary: $('#ev_t', e2).value.trim(), start: new Date($('#ev_s', e2).value).toISOString(), end: new Date($('#ev_e', e2).value).toISOString(), location: $('#ev_l', e2).value, description: $('#ev_d', e2).value }) });
          e2.remove(); draw();
        } catch (e) { errTo(e); }
      };
    });
    d2.style.zIndex = 70;
  };
  $('#ca_acct', el).onchange = draw; $('#ca_new', el).onclick = () => edit(); draw();
};

// ---------- contacts ----------
H.contactsDialog = async function () {
  if (needAccount()) return;
  const el = dialog('<h2>Contacts</h2><input id="ct_q" placeholder="Find a contact" style="width:100%"><div id="ctbody" style="margin-top:8px"></div><div class="btns"><button class="btn" id="ct_new">New contact</button><button class="btn primary" data-close>Done</button></div>');
  const draw = async () => {
    try {
      const cs = await api('contacts/all?q=' + encodeURIComponent($('#ct_q', el).value));
      $('#ctbody', el).innerHTML = cs.length ? cs.map(c => '<div class="card" style="display:flex;gap:10px;align-items:center"><div class="avatar" style="background:' + colorFor(c.email) + ';width:32px;height:32px">' + esc(initials(c.name || c.email)) + '</div><div style="flex:1;min-width:0"><b>' + esc(c.name || c.email) + '</b><div class="pill">' + esc(c.email) + (c.phone ? ' &middot; ' + esc(c.phone) : '') + (c.org ? ' &middot; ' + esc(c.org) : '') + '</div></div>' +
        '<button class="btn" data-mail="' + esc(c.email) + '">Write</button>' + (c.id ? '<button class="btn" data-edit="' + esc(c.id) + '">Edit</button><button class="btn danger" data-del="' + esc(c.id) + '">Delete</button>' : '') + '</div>').join('') : '<p>No contacts found. People you write to are added here.</p>';
      $$('[data-mail]', el).forEach(b => b.onclick = () => { el.remove(); openComposer({ mode: 'new', to: [{ email: b.dataset.mail }] }); });
      $$('[data-del]', el).forEach(b => b.onclick = async () => { try { await api('contacts/delete', { accountId: S.data.accounts[0].id, id: b.dataset.del }); draw(); } catch (e) { errTo(e); } });
      $$('[data-edit]', el).forEach(b => b.onclick = () => edit(cs.find(c => c.id === b.dataset.edit)));
    } catch (e) { errTo(e); }
  };
  const edit = c => {
    c = c || {};
    const d2 = dialog('<h2>' + (c.id ? 'Edit the contact' : 'New contact') + '</h2><div class="row2">' + lbl('Account', acctSelect('cc_acct', S.account)) + lbl('Name', '<input id="cc_n" value="' + esc(c.name || '') + '">') + lbl('Email address', '<input id="cc_e" value="' + esc(c.email || '') + '">') + lbl('Phone', '<input id="cc_p" value="' + esc(c.phone || '') + '">') + lbl('Organization', '<input id="cc_o" value="' + esc(c.org || '') + '">') +
      '</div><div class="btns"><button class="btn" data-close>Cancel</button><button class="btn primary" id="cc_go">Save</button></div>', e2 => {
      $('#cc_go', e2).onclick = async () => { try { await api('contacts/save', { accountId: $('#cc_acct', e2).value, id: c.id || '', name: $('#cc_n', e2).value, email: $('#cc_e', e2).value.trim(), phone: $('#cc_p', e2).value, org: $('#cc_o', e2).value }); e2.remove(); draw(); } catch (e) { errTo(e); } };
    });
    d2.style.zIndex = 70;
  };
  $('#ct_q', el).oninput = draw; $('#ct_new', el).onclick = () => edit(); draw();
};

// ---------- import and export ----------
H.importDialog = function () {
  const el = dialog('<h2>Import mail</h2><p class="pill">Mail from other programs is kept in the account "On this computer", in folders, and is searched with everything else.</p><div class="card"><b>An mbox file</b><div class="row2">' + lbl('File', '<input type="file" id="mb_file">') + lbl('Folder name', '<input id="mb_folder" placeholder="Imported">') + '<button class="btn primary" id="mb_go" style="justify-self:start">Import the file</button></div></div>' +
    '<div class="card"><b>A Thunderbird profile</b><div class="row2">' + lbl('Profile folder', '<input id="tb_path" placeholder="/home/me/.thunderbird/xxxx.default-release">') + '<div id="tb_hint" class="pill">Thunderbird: Help, More Troubleshooting Information, Profile folder.</div><button class="btn primary" id="tb_go" style="justify-self:start">Import every folder</button></div></div><div id="im_msg" class="pill"></div><div class="btns"><button class="btn primary" data-close>Done</button></div>', e => {
    const msg = (t, bad) => { const m = $('#im_msg', e); m.textContent = t; m.style.color = bad ? 'var(--danger)' : ''; };
    $('#mb_go', e).onclick = async () => {
      const f = $('#mb_file', e).files[0]; if (!f) { msg('Choose a file first.', true); return; }
      const fd = new FormData(); fd.append('file', f); fd.append('folder', $('#mb_folder', e).value.trim() || f.name.replace(/\.[^.]*$/, ''));
      msg('Importing...');
      try { const r = await api('import/mbox', fd); msg(r.skipped ? t('{n} message(s) imported into "{x}", {s} were already there.', { n: r.imported, x: r.folder, s: r.skipped }) : t('{n} message(s) imported into "{x}".', { n: r.imported, x: r.folder })); await refreshState(); refreshSoon(); } catch (x) { msg(x.message, true); }
    };
    $('#tb_go', e).onclick = async () => {
      const p = $('#tb_path', e).value.trim(); if (!p) { msg('Enter the profile folder.', true); return; }
      msg('Importing...');
      try { const r = await api('import/thunderbird', { path: p }); msg(t('{n} message(s) imported from {f} folder(s).', { n: r.reduce((n, x) => n + x.imported, 0), f: r.length })); await refreshState(); refreshSoon(); } catch (x) { msg(x.message, true); }
    };
  });
  return el;
};
