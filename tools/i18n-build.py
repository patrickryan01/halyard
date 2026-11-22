#!/usr/bin/env python3
"""Builds ui/web/i18n/<lang>.json from the numbered translation files in ui/web/i18n/src/. A line is "N|text", where N is
the place of the English phrase in tools/i18n-order.json. Checks that every phrase is there and that the values in
braces survive. Usage: tools/i18n-build.py [lang ...]"""
import json, re, sys, os
ORDER = json.load(open('tools/i18n-order.json', encoding='utf-8'))
KEYS = json.load(open('ui/web/i18n/keys.json', encoding='utf-8'))
LANGS = ['es', 'fr', 'it', 'de', 'nb', 'bg', 'el', 'ko', 'ja', 'vi', 'hi', 'zh-Hans', 'ar']
ph = lambda s: sorted(re.findall(r'\{\w+\}', s))
bad = 0
for lang in (sys.argv[1:] or LANGS):
    path = 'ui/web/i18n/src/%s.txt' % lang
    if not os.path.exists(path): print(lang, 'missing'); bad += 1; continue
    by = {}
    for ln in open(path, encoding='utf-8').read().split('\n'):
        if not ln.strip(): continue
        n, _, t = ln.partition('|')
        by[int(n)] = t
    cat = {}; errs = []
    for k in KEYS:
        i = ORDER.index(k) + 1
        t = by.get(i)
        if not t: errs.append('missing %d %s' % (i, k)); continue
        if ph(t) != ph(k): errs.append('placeholders %d %s -> %s' % (i, k, t))
        cat[k] = t
    if errs:
        print(lang, len(errs), 'problems'); print('\n'.join(errs[:20])); bad += 1; continue
    json.dump(cat, open('ui/web/i18n/%s.json' % lang, 'w', encoding='utf-8'), ensure_ascii=False, indent=0, sort_keys=True)
    print(lang, 'ok', len(cat))
sys.exit(1 if bad else 0)
