#!/usr/bin/env python3
"""Reads the feature list and the logs of the test runs, and fails when a row that is not dropped lacks a passing
check. Usage: completeness.py --go go.log --lab lab.log --ui ui.log --live live.log --smoke smoke.log --i18n i18n.log
[--pkgdir DIR]"""
import argparse, os, re, subprocess, sys

ap = argparse.ArgumentParser()
for k in ('go', 'lab', 'ui', 'live', 'smoke', 'i18n', 'pkgdir'):
    ap.add_argument('--' + k)
ap.add_argument("--doc", required=True, help="the completeness list (kept with the release records, not in this repository)")
a = ap.parse_args()

def read(p): return open(p, encoding='utf-8', errors='replace').read() if p and os.path.exists(p) else ''
logs = {k: read(getattr(a, k)) for k in ('go', 'lab', 'ui', 'live', 'smoke', 'i18n')}

def go_status(log):
    st = {}
    for m in re.finditer(r'^--- (PASS|FAIL|SKIP): (\w+) ', log, re.M):
        name, v = m.group(2), m.group(1)
        if st.get(name) == 'FAIL': continue
        st[name] = v
    return st
gost, labst = go_status(logs['go']), go_status(logs['lab'])

def check(tok):
    kind, _, arg = tok.partition(':')
    arg = arg.strip()
    if kind == 'go': return gost.get(arg) == 'PASS', 'go test %s: %s' % (arg, gost.get(arg, 'not run'))
    if kind == 'lab': return labst.get(arg) == 'PASS', 'lab test %s: %s' % (arg, labst.get(arg, 'not run'))
    if kind in ('ui', 'live'):
        log = logs[kind]
        ok = ('PASS ' + arg) in log and ('FAIL ' + arg) not in log
        return ok, '%s "%s": %s' % (kind, arg, 'PASS' if ok else 'missing or failed')
    if kind == 'smoke': ok = 'ui smoke ok' in logs['smoke']; return ok, 'ui smoke: %s' % ('ok' if ok else 'not ok')
    if kind == 'i18n': ok = 'i18n ok' in logs['i18n'] and 'I18N FAILED' not in logs['i18n']; return ok, 'i18n check: %s' % ('ok' if ok else 'not ok')
    if kind == 'build':
        if arg == 'windows tray':
            r = subprocess.run(['go', 'vet', './internal/tray', './cmd/halyard'], env=dict(os.environ, GOOS='windows', GOARCH='amd64', CGO_ENABLED='0'), capture_output=True, text=True)
            return r.returncode == 0, 'windows build of the tray: %s' % ('ok' if r.returncode == 0 else r.stderr[:200])
        if arg == 'packages':
            d = a.pkgdir or ''
            names = os.listdir(d) if d and os.path.isdir(d) else []
            have = [any(n.endswith(s) for n in names) for s in ('.pkg.tar.zst', '.rpm', '-setup.exe')]
            return all(have), 'package files (arch, rpm, windows): %s' % have
    return False, 'unknown check ' + tok

rows = []
for line in open(a.doc, encoding='utf-8'):
    if not line.startswith('| ') or line.startswith('| ID') or line.startswith('|---'): continue
    cells = [c.strip() for c in line.strip().strip('|').split(' | ')]
    if len(cells) < 5: cells += [''] * (5 - len(cells))
    rows.append(cells[:5])

bad, counts = [], {'shipped': 0, 'hardware': 0, 'dropped': 0}
for rid, feat, state, checks, note in rows:
    if state not in counts: bad.append('%s: unknown state %r' % (rid, state)); continue
    counts[state] += 1
    if state == 'dropped':
        if not note or checks != '-': bad.append('%s: a dropped row needs its reason and no check' % rid)
        continue
    if state == 'hardware' and not note: bad.append('%s: a hardware row must say what is left out' % rid)
    toks = [t.strip() for t in checks.split(';') if t.strip() and t.strip() != '-']
    if not toks: bad.append('%s: no automated check' % rid); continue
    fails = []
    for t in toks:
        ok, why = check(t)
        if not ok: fails.append(why)
    print('%-4s %-6s %s' % (rid, 'PASS' if not fails else 'FAIL', feat[:90]))
    for f in fails: print('       ' + f); bad.append('%s: %s' % (rid, f))
print('\n%d shipped, %d hardware (checked as far as software can), %d dropped with reasons' % (counts['shipped'], counts['hardware'], counts['dropped']))
if bad:
    print('\nNOT COMPLETE:'); print('\n'.join(' - ' + b for b in bad)); sys.exit(1)
print('COMPLETE: every row that is not dropped passes')
