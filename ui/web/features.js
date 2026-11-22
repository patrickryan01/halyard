'use strict';
/* Commands, shortcuts and the command palette. Every action the window can do is a command with an id; the keyboard,
   the palette and the settings screen all read this one list, and a person can change any key. */

const H = window.H = window.H || {};

// ---------- the command list ----------
// keys: the default shortcut ("c", "Shift+I", "Ctrl+K", or a sequence "g i"); ctx says where it works.
H.commands = [];
function cmd(id, title, keys, run, ctx) { H.commands.push({ id, title, keys: keys || '', run, ctx: ctx || 'any' }); }
H.cmd = cmd;

const inList = () => !S.open;
const inConvo = () => !!S.open;
function tgts() { return targets(); }
function act(kind, quiet) { const t = tgts(); if (t.length) doAct(kind, t, quiet ? { quiet: true } : {}); }

cmd('compose', 'Compose a new message', 'c', () => openComposer({ mode: 'new' }));
cmd('search', 'Search mail', '/', () => { const q = $('#q'); if (q) { q.focus(); q.select(); } });
cmd('palette', 'Open the command palette', 'Ctrl+K', () => H.palette());
cmd('shortcuts', 'Show keyboard shortcuts', '?', () => H.shortcutsDialog());
cmd('next', 'Next conversation', 'j', () => { if (S.open) stepConvo(1); else if (S.cursor < S.threads.length - 1) { S.cursor++; render(); keepCursor(); } });
cmd('prev', 'Previous conversation', 'k', () => { if (S.open) stepConvo(-1); else if (S.cursor > 0) { S.cursor--; render(); keepCursor(); } });
cmd('open', 'Open the conversation', 'o', () => { const t = S.threads[S.cursor]; if (t && !S.open) openThread(t.id); }, 'list');
cmd('openEnter', 'Open the conversation (Enter)', 'Enter', () => { const t = S.threads[S.cursor]; if (t && !S.open) openThread(t.id); }, 'list');
cmd('back', 'Back to the list', 'u', () => { if (S.open) { S.open = null; render(); } }, 'open');
cmd('archive', 'Archive', 'e', () => act('archive'));
cmd('trash', 'Delete', '#', () => act('trash'));
cmd('trashKey', 'Delete (Delete key)', 'Delete', () => act('trash'));
cmd('spam', 'Report spam', '!', () => act('spam'));
cmd('star', 'Star or unstar', 's', () => {
  if (S.open) doAct(S.open.msgs.some(m => m.starred) ? 'unstar' : 'star', [S.open.id], { quiet: true });
  else { const t = S.threads[S.cursor]; if (t) doAct(t.starred ? 'unstar' : 'star', tgts(), { quiet: true }); }
});
cmd('select', 'Select the conversation', 'x', () => { const t = S.threads[S.cursor]; if (t && !S.open) { if (S.sel.has(t.id)) S.sel.delete(t.id); else S.sel.add(t.id); render(); } }, 'list');
cmd('markRead', 'Mark as read', 'Shift+I', () => act('read'));
cmd('markUnread', 'Mark as unread', 'Shift+U', () => act('unread'));
cmd('reply', 'Reply', 'r', () => { if (S.open) replyTo(S.open.msgs.length - 1, 'reply'); }, 'open');
cmd('replyAll', 'Reply all', 'a', () => { if (S.open) replyTo(S.open.msgs.length - 1, 'replyAll'); }, 'open');
cmd('forward', 'Forward', 'f', () => { if (S.open) replyTo(S.open.msgs.length - 1, 'forward'); }, 'open');
cmd('snooze', 'Snooze', 'b', () => { const t = tgts(); if (t.length) snoozeMenu({ currentTarget: $('.toolbar') }, t); });
cmd('undo', 'Undo the last action', 'z', () => { if (S.lastAct) { doAct(S.lastAct.kind, S.lastAct.ids); S.lastAct = null; } });
cmd('nextMsg', 'Next message in the conversation', 'n', () => H.stepMsg(1), 'open');
cmd('prevMsg', 'Previous message in the conversation', 'p', () => H.stepMsg(-1), 'open');
cmd('goInbox', 'Go to Inbox', 'g i', () => go('role:inbox'));
cmd('goStarred', 'Go to Starred', 'g s', () => go('starred'));
cmd('goSent', 'Go to Sent', 'g t', () => go('role:sent'));
cmd('goDrafts', 'Go to Drafts', 'g d', () => go('role:drafts'));
cmd('goAll', 'Go to All Mail', 'g a', () => go('role:all'));
cmd('goSnoozed', 'Go to Snoozed', 'g b', () => go('snoozed'));
cmd('goSpam', 'Go to Spam', '', () => go('role:spam'));
cmd('goTrash', 'Go to Trash', '', () => go('role:trash'));
cmd('sync', 'Check for new mail', 'Ctrl+Shift+R', () => api('sync', {}).then(() => toast('Checking for mail...')));
cmd('settings', 'Open settings', 'Ctrl+,', () => settingsDialog());
cmd('sidebar', 'Show or hide the folder list', '', () => { S.side = !S.side; render(); });
cmd('print', 'Print the message', 'Ctrl+P', () => H.printOpen(), 'open');
cmd('source', 'View the message source', '', () => H.viewSource(), 'open');
cmd('saveEml', 'Save the message as a file (.eml)', '', () => H.saveEml(), 'open');
cmd('exportThreads', 'Save the selected conversations as an mbox file', '', () => H.exportMbox());
cmd('markAllRead', 'Mark everything in this folder as read', '', () => H.markAllRead());
cmd('emptyTrash', 'Empty the trash', '', () => H.emptyFolder('trash'));
cmd('emptySpam', 'Empty the spam folder', '', () => H.emptyFolder('spam'));
cmd('saveSearch', 'Save this search', '', () => H.saveCurrentSearch());
cmd('contacts', 'Open contacts', '', () => H.contactsDialog());
cmd('calendar', 'Open the calendar', '', () => H.calendarDialog());
cmd('identities', 'Identities and signatures', '', () => H.identitiesDialog());
cmd('templates', 'Templates (canned replies)', '', () => H.templatesDialog());
cmd('rules', 'Filters and rules', '', () => H.rulesDialog());
cmd('vacation', 'Vacation reply', '', () => H.vacationDialog());
cmd('sieve', 'Server filters (Sieve)', '', () => H.sieveDialog());
cmd('security', 'Encryption keys and certificates', '', () => H.securityDialog());
cmd('dav', 'Calendar and contacts server', '', () => H.davDialog());
cmd('importExport', 'Import mail', '', () => H.importDialog());
cmd('general', 'More settings', '', () => H.generalDialog());
cmd('addAccount', 'Add an account', '', () => addAccountDialog());
cmd('themeLight', 'Use the light theme', '', () => H.setSetting({ theme: 'light' }));
cmd('themeDark', 'Use the dark theme', '', () => H.setSetting({ theme: 'dark' }));
cmd('themeAuto', 'Follow the system theme', '', () => H.setSetting({ theme: 'auto' }));
cmd('conversations', 'Show or hide conversation view', '', () => H.setSetting({ conversations: S.data.settings.conversations === 'true' ? 'false' : 'true' }, true));
cmd('language', 'Change the language', '', () => H.generalDialog());

H.setSetting = async function (o, reload) {
  await api('settings', o); await refreshState(); applyTheme();
  if (reload) await loadThreads(false);
  render();
};

// ---------- keys ----------
function keyName(e) {
  let k = e.key;
  if (k === ' ') k = 'Space';
  const mods = [];
  if (e.ctrlKey) mods.push('Ctrl');
  if (e.metaKey) mods.push('Ctrl'); // Cmd counts as Ctrl so one list serves every system
  if (e.altKey) mods.push('Alt');
  // a letter with Shift is written as the capital; the Shift prefix is only for keys that do not change with it
  if (e.shiftKey && (k.length > 1 || mods.length) && k !== 'Shift') mods.push('Shift');
  if (k === 'Shift' || k === 'Control' || k === 'Alt' || k === 'Meta') return '';
  if (mods.length && k.length === 1) k = k.toUpperCase();
  return mods.concat([k]).join('+');
}
// "Shift+I" in the list is the same key as the capital "I" the keyboard reports
function normKeys(s) {
  return s.split(' ').map(p => p === 'Shift+I' ? 'I' : p === 'Shift+U' ? 'U' : p).join(' ');
}
H.keymap = function () {
  let over = {};
  try { over = JSON.parse((S.data && S.data.settings.shortcuts) || '{}') || {}; } catch (e) {}
  const m = {};
  for (const c of H.commands) {
    const k = c.id in over ? over[c.id] : c.keys;
    if (k) (m[normKeys(k)] = m[normKeys(k)] || []).push(c);
  }
  return m;
};
H.keyOf = id => {
  let over = {};
  try { over = JSON.parse((S.data && S.data.settings.shortcuts) || '{}') || {}; } catch (e) {}
  const c = H.commands.find(c => c.id === id);
  return id in over ? over[id] : (c ? c.keys : '');
};
let pending = '', pendTimer = null;
H.key = function (e) {
  if (!S.data) return false;
  const name = keyName(e);
  if (!name) return false;
  if (name === 'Escape') {
    pending = '';
    closePop();
    const d = $('.scrim'); if (d && !d.dataset.sticky) { d.remove(); return true; }
    if (S.open && !typing(e)) { S.open = null; render(); return true; }
    return false;
  }
  // while typing, only combinations with Ctrl or Alt are shortcuts
  if (typing(e) && !/^(Ctrl|Alt)\+/.test(name)) return false;
  if ($('.scrim') && !/^Ctrl\+/.test(name)) return false;
  const km = H.keymap();
  const seq = pending ? pending + ' ' + name : name;
  const hit = km[seq];
  const isPrefix = Object.keys(km).some(k => k.startsWith(seq + ' '));
  if (hit) {
    pending = ''; clearTimeout(pendTimer);
    const c = hit.find(c => c.ctx === 'any' || (c.ctx === 'list' && inList()) || (c.ctx === 'open' && inConvo())) || null;
    if (!c) return false;
    e.preventDefault();
    c.run();
    return true;
  }
  if (isPrefix) { pending = seq; clearTimeout(pendTimer); pendTimer = setTimeout(() => { pending = ''; }, 1500); e.preventDefault(); return true; }
  pending = '';
  return false;
};

H.stepMsg = function (d) {
  if (!S.open) return;
  const last = S.open.msgs.length - 1;
  const cur = S.open.msgs.findIndex(m => S.expanded.has(m.id));
  const n = Math.max(0, Math.min(last, cur + d));
  S.expanded.add(S.open.msgs[n].id); render();
};

// ---------- the command palette ----------
function subseq(q, s) {
  q = q.toLowerCase(); s = s.toLowerCase();
  let i = 0, score = 0, last = -2;
  for (let j = 0; j < s.length && i < q.length; j++) { if (s[j] === q[i]) { score += (j === last + 1) ? 3 : 1; last = j; i++; } }
  return i === q.length ? score + (s.startsWith(q) ? 20 : 0) + (s.includes(q) ? 10 : 0) : -1;
}
H.paletteItems = function () {
  const items = H.commands.filter(c => c.ctx === 'any' || (c.ctx === 'list' && inList()) || (c.ctx === 'open' && inConvo()))
    .map(c => ({ title: t(c.title), keys: H.keyOf(c.id), run: c.run, group: t('Command') }));
  for (const f of S.data.folders.filter(f => f.role === 'custom')) {
    const a = acctOf(f.accountId);
    items.push({ title: t('Go to {x}', { x: f.name + (S.data.accounts.length > 1 && a ? ' (' + a.email + ')' : '') }), keys: '', run: () => go(f.id), group: t('Folder') });
  }
  for (const a of S.data.accounts) items.push({ title: t('Show only {x}', { x: a.email }), keys: '', run: () => go('role:inbox', a.id), group: t('Account') });
  for (const s of (H.searches || [])) items.push({ title: t('Search: {x}', { x: s.name }), keys: '', run: () => { S.q = s.query; search(s.query); }, group: t('Saved search') });
  for (const tp of (H.templatesList || [])) items.push({ title: t('New message from template: {x}', { x: tp.name }), keys: '', run: () => H.composeFromTemplate(tp), group: t('Template') });
  return items;
};
H.palette = function () {
  if ($('.palette')) return;
  const el = document.createElement('div'); el.className = 'scrim palette'; el.dataset.sticky = '1';
  el.innerHTML = '<div class="dialog" role="dialog" aria-modal="true" aria-label="Command palette" style="padding:10px;width:min(620px,94vw);align-self:start;margin-top:12vh"><input id="pq" placeholder="Type a command, a folder, or words to search for" autocomplete="off" style="width:100%"><div class="plist" role="listbox"></div></div>';
  document.body.appendChild(el);
  const inp = $('#pq', el), list = $('.plist', el);
  let items = [], idx = 0;
  const draw = () => {
    const q = inp.value.trim();
    let all = H.paletteItems();
    if (q) {
      all = all.map(i => ({ i, s: subseq(q, i.title) })).filter(x => x.s >= 0).sort((a, b) => b.s - a.s).map(x => x.i);
      all.push({ title: t('Search mail for "{x}"', { x: q }), keys: '', run: () => search(q), group: t('Search') });
    }
    items = all.slice(0, 40); idx = Math.min(idx, Math.max(0, items.length - 1));
    list.innerHTML = items.map((it, n) => '<div class="pi' + (n === idx ? ' on' : '') + '" role="option" data-n="' + n + '"><span>' + esc(it.title) + '</span><small>' + esc(it.keys ? it.keys.replace(/ /g, ' then ') : it.group) + '</small></div>').join('');
    $$('.pi', list).forEach(d => { d.onmousedown = ev => { ev.preventDefault(); run(+d.dataset.n); }; });
    const on = $('.pi.on', list); if (on) on.scrollIntoView({ block: 'nearest' });
  };
  const run = n => { const it = items[n]; el.remove(); if (it) setTimeout(() => it.run(), 0); };
  inp.oninput = () => { idx = 0; draw(); };
  inp.onkeydown = e => {
    if (e.key === 'ArrowDown') { e.preventDefault(); idx = Math.min(items.length - 1, idx + 1); draw(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); idx = Math.max(0, idx - 1); draw(); }
    else if (e.key === 'Enter') { e.preventDefault(); run(idx); }
    else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); el.remove(); }
  };
  el.addEventListener('mousedown', e => { if (e.target === el) el.remove(); });
  draw(); inp.focus();
};

// ---------- keyboard shortcuts screen: see, search and change ----------
H.shortcutsDialog = function () {
  const d = dialog('<h2>Keyboard shortcuts</h2><input id="kf" placeholder="Find a shortcut" style="width:100%;margin-bottom:8px"><div class="kmap"></div><div class="btns"><button class="btn" id="kreset">Reset all</button><button class="btn primary" data-close>Done</button></div>', el => {
    const body = $('.kmap', el);
    const draw = () => {
      const f = $('#kf', el).value.toLowerCase();
      body.innerHTML = H.commands.filter(c => !f || t(c.title).toLowerCase().includes(f)).map(c => {
        const k = H.keyOf(c.id);
        return '<div class="krow"><span>' + esc(t(c.title)) + '</span><button class="kbd kchg" data-id="' + c.id + '" title="Change">' + esc(k ? k.replace(/ /g, ' then ') : 'none') + '</button></div>';
      }).join('');
      $$('.kchg', body).forEach(b => b.onclick = () => capture(b.dataset.id));
    };
    const save = async over => { await api('settings', { shortcuts: JSON.stringify(over) }); await refreshState(); draw(); };
    const current = () => { try { return JSON.parse(S.data.settings.shortcuts || '{}') || {}; } catch (e) { return {}; } };
    function capture(id) {
      const c = H.commands.find(c => c.id === id);
      const cap = dialog('<h2>' + esc(c.title) + '</h2><p>Press the new keys. For two keys in a row, press them one after the other. Escape cancels, Delete clears.</p><div id="got" class="kbd" style="font-size:16px;padding:6px 12px;display:inline-block;min-width:60px"></div><div id="warn" class="pill" style="margin-top:8px"></div><div class="btns"><button class="btn" data-close>Cancel</button><button class="btn primary" id="ok" disabled>Use this</button></div>', ce => {
        let seq = [];
        const show = () => {
          const v = seq.join(' ');
          $('#got', ce).textContent = v || '...';
          const clash = H.commands.find(x => x.id !== id && normKeys(H.keyOf(x.id)) === normKeys(v));
          $('#warn', ce).textContent = clash ? 'Already used by "' + clash.title + '". Using it here frees it there.' : '';
          $('#ok', ce).disabled = !v;
        };
        ce.tabIndex = 0; ce.focus();
        ce.addEventListener('keydown', e => {
          e.preventDefault(); e.stopPropagation();
          if (e.key === 'Escape') { ce.remove(); return; }
          if (e.key === 'Delete' || e.key === 'Backspace') { const o = current(); o[id] = ''; ce.remove(); save(o); return; }
          const n = keyName(e); if (!n) return;
          if (seq.length >= 2) seq = [];
          seq.push(n); show();
        });
        $('#ok', ce).onclick = () => {
          const v = seq.join(' '); const o = current();
          for (const x of H.commands) if (x.id !== id && normKeys(H.keyOf(x.id)) === normKeys(v)) o[x.id] = '';
          o[id] = v; ce.remove(); save(o);
        };
      }, true);
      cap.style.zIndex = 80;
    }
    $('#kf', el).oninput = draw;
    $('#kreset', el).onclick = () => save({});
    draw();
  });
  return d;
};
// the old name: the help screen and the shortcut list are the same screen now
window.helpDialog = H.shortcutsDialog;
