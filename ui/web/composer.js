'use strict';
/* The composer's extras: who the message is from (identities and their signatures), templates, send later, read
   receipt, signing and encryption, spelling, and the size check. */

H.identities = [];
H.loadIdentities = async () => { try { H.identities = await api('identities'); } catch (e) { H.identities = []; } };
H.keyring = { pgp: [], smime: [] };
H.loadKeys = async () => { try { H.keyring = await api('keys'); } catch (e) { H.keyring = { pgp: [], smime: [] }; } };

const SIGMARK = 'data-sig';
function sigHtml(text) { return text ? '<div ' + SIGMARK + '="1"><br>-- <br>' + esc(text).replace(/\n/g, '<br>') + '</div>' : ''; }
function identityFor(c) {
  const mine = H.identities.filter(i => i.accountId === c.accountId);
  return mine.find(i => c.fromEmail && i.email.toLowerCase() === c.fromEmail.toLowerCase()) || mine.find(i => i.default) || mine[0] || null;
}

H.composer = function (root, c, ctl) {
  const ed = ctl.ed;
  ed.setAttribute('spellcheck', S.data.settings.spellcheck === 'false' ? 'false' : 'true');
  c.receipt = !!c.receipt; c.sign = c.sign || (S.data.settings.signAll === 'true'); c.encrypt = !!c.encrypt;
  c.protect = c.protect || S.data.settings.defaultProtect || 'pgp';
  // ---- from: every account and each of its addresses
  const from = $('[data-from]', root);
  const drawFrom = () => {
    const opts = [];
    for (const a of S.data.accounts) {
      const ids = H.identities.filter(i => i.accountId === a.id);
      if (!ids.length) opts.push([a.id + '|' + a.email, (a.name ? a.name + ' ' : '') + '<' + a.email + '>']);
      for (const i of ids) opts.push([a.id + '|' + i.email, (i.name ? i.name + ' ' : '') + '<' + i.email + '>' + (S.data.accounts.length > 1 ? '  (' + a.email + ')' : '')]);
    }
    from.innerHTML = opts.map(o => '<option value="' + esc(o[0]) + '">' + esc(o[1]) + '</option>').join('');
    const id = identityFor(c);
    from.value = c.accountId + '|' + (id ? id.email : (acctOf(c.accountId) || {}).email);
  };
  drawFrom();
  const putSignature = () => {
    const id = identityFor(c);
    const old = $('[' + SIGMARK + ']', ed); if (old) old.remove();
    const sig = id ? id.signature : (acctOf(c.accountId) || {}).signature;
    if (!sig) return;
    const q = $('blockquote', ed);
    const holder = document.createElement('div'); holder.innerHTML = sigHtml(sig);
    const node = holder.firstChild;
    // on a reply the signature sits above the quoted text, as people expect
    const quoteHead = q && q.previousElementSibling && /wrote:|Forwarded message/.test(q.previousElementSibling.textContent) ? q.previousElementSibling : null;
    if (quoteHead) ed.insertBefore(node, quoteHead.previousElementSibling && quoteHead.previousElementSibling.tagName === 'BR' ? quoteHead.previousElementSibling : quoteHead); else ed.appendChild(node);
  };
  if (c.mode !== 'draft' && !c.fromDraft) putSignature();
  from.onchange = () => {
    const [a, e] = from.value.split('|'); c.accountId = a; c.fromEmail = e;
    putSignature(); ctl.save();
  };
  if (c.fromEmail) from.value = c.accountId + '|' + c.fromEmail;
  // ---- the foot: templates, later, options
  const foot = $('.cfoot', root);
  const sendBtn = $('[data-send]', root);
  const later = document.createElement('button'); later.className = 'btn'; later.setAttribute('data-later', ''); later.title = 'Send later'; later.setAttribute('aria-label', 'Send later'); later.innerHTML = '&#9662;'; later.style.cssText = 'margin-left:-6px;padding:7px 8px';
  sendBtn.after(later);
  later.onclick = ev => {
    const now = new Date(); const at = (d, h) => { const x = new Date(d); x.setHours(h, 0, 0, 0); return x; };
    const tom = at(new Date(now.getTime() + 864e5), 8), mon = at(new Date(now.getTime() + ((1 - now.getDay() + 7) % 7 || 7) * 864e5), 8), hr = new Date(now.getTime() + 36e5);
    popup(ev, [['In one hour', hr], ['Tomorrow morning', tom], ['Monday morning', mon]].map(([l, d]) => '<button data-at="' + d.toISOString() + '">' + l + '<small>' + d.toLocaleString([], { weekday: 'short', hour: 'numeric', minute: '2-digit' }) + '</small></button>').join('') +
      '<div style="padding:6px"><label class="pill">Pick date and time</label><input type="datetime-local" id="laterat" style="width:100%"><button class="btn primary" id="latergo" style="margin-top:6px;justify-content:center">Schedule</button></div>', pop => {
      const go = iso => { closePop(); c.sendAt = iso; ctl.send(); };
      $$('[data-at]', pop).forEach(b => b.onclick = () => go(b.dataset.at));
      $('#latergo', pop).onclick = () => { const v = $('#laterat', pop).value; if (v) go(new Date(v).toISOString()); };
    });
  };
  const tbtn = document.createElement('button'); tbtn.className = 'btn'; tbtn.textContent = 'Templates'; tbtn.setAttribute('data-tpl', '');
  $('[data-attach]', root).after(tbtn);
  tbtn.onclick = async ev => {
    const list = await H.loadTemplates();
    if (!list.length) { toast('No templates yet. Make some under Templates.'); return; }
    popup(ev, list.map(t => '<button data-t="' + esc(t.id) + '">' + esc(t.name) + '</button>').join(''), pop => $$('[data-t]', pop).forEach(b => b.onclick = () => {
      closePop(); const t = list.find(x => x.id === b.dataset.t);
      if (t.subject && !$('[data-subj]', root).value) { $('[data-subj]', root).value = t.subject; c.subject = t.subject; $('.ttl', root).textContent = t.subject; }
      ed.focus(); document.execCommand('insertHTML', false, '<div>' + esc(t.body).replace(/\n/g, '<br>') + '</div>'); ctl.save();
    }));
  };
  const obtn = document.createElement('button'); obtn.className = 'btn'; obtn.textContent = 'Options'; obtn.setAttribute('data-opts', '');
  tbtn.after(obtn);
  const panel = document.createElement('div'); panel.className = 'copts hide';
  const hasKey = kind => kind === 'pgp' ? H.keyring.pgp.some(k => true) : H.keyring.smime.some(k => true);
  panel.innerHTML = '<label><input type="checkbox" data-rcpt style="width:auto"> Ask for a read receipt</label>' +
    '<label><input type="checkbox" data-sign style="width:auto"> Sign</label><label><input type="checkbox" data-enc style="width:auto"> Encrypt</label>' +
    '<select data-prot style="width:auto"><option value="pgp">OpenPGP</option><option value="smime">S/MIME</option></select><span class="pill" data-keyhint></span>';
  $('.cfiles', root).before(panel);
  obtn.onclick = () => panel.classList.toggle('hide');
  const rc = $('[data-rcpt]', panel), sg = $('[data-sign]', panel), en = $('[data-enc]', panel), pr = $('[data-prot]', panel);
  rc.checked = c.receipt; sg.checked = c.sign; en.checked = c.encrypt; pr.value = c.protect;
  const hint = () => {
    const need = (sg.checked || en.checked);
    $('[data-keyhint]', panel).textContent = need && !(pr.value === 'pgp' ? H.keyring.pgp.length : H.keyring.smime.length) ? 'You have no ' + (pr.value === 'pgp' ? 'OpenPGP key' : 'S/MIME certificate') + ' yet: make or import one under Encryption keys.' : '';
  };
  const sync = () => { c.receipt = rc.checked; c.sign = sg.checked; c.encrypt = en.checked; c.protect = pr.value; hint(); ctl.save(); obtn.classList.toggle('primary', c.receipt || c.sign || c.encrypt); };
  [rc, sg, en, pr].forEach(x => x.onchange = sync);
  if (c.receipt || c.sign || c.encrypt) { panel.classList.remove('hide'); obtn.classList.add('primary'); }
  hint();
  // when everyone written to has a key, offer encryption by default if the person asked for it
  c.maybeEncrypt = () => {
    if (S.data.settings.encryptWhenPossible !== 'true' || en.checked) return;
    const keys = pr.value === 'pgp' ? H.keyring.pgp : H.keyring.smime;
    const all = [...c.to, ...c.cc, ...c.bcc];
    if (all.length && keys.length && all.every(a => keys.some(k => (k.email || '').toLowerCase() === a.email.toLowerCase()))) { en.checked = true; sync(); }
  };
};

// the extra fields the core needs, for sending and for saving a draft
H.extras = function (c) {
  return { from: c.fromEmail ? c.fromEmail : '', receipt: !!c.receipt, protect: (c.sign || c.encrypt) ? c.protect : '', sign: !!c.sign, encrypt: !!c.encrypt };
};
H.draftExtras = function (c) {
  return { fromEmail: c.fromEmail || '', receiptRequested: !!c.receipt, protect: c.protect || '', sign: !!c.sign, encrypt: !!c.encrypt };
};
// a message that is too big for most servers, or too many recipients without a subject, is questioned before it leaves
H.checkSend = function (c) {
  const total = c.files.reduce((n, f) => n + f.size, 0);
  if (total > 25 * 1048576 && !confirm('The attachments are ' + fmtSize(total) + '. Most mail servers refuse messages over 25 MB. Send anyway?')) return false;
  c.maybeEncrypt && c.maybeEncrypt();
  return true;
};
