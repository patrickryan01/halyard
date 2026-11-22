#!/usr/bin/env python3
"""Lists what the tour saw on screen that no catalog phrase covers. Names, addresses, dates and key names are not phrases."""
import json, re, sys
shown = json.load(open(sys.argv[1])); keys = json.load(open('ui/web/i18n/keys.json'))
pats = [re.compile('^' + re.sub(r'\\\{\w+\\\}', '.+?', re.escape(k)) + '$') for k in keys if '{' in k]
ks = set(keys)
def ignorable(s):
    return (re.match(r'^[\w.+-]+@[\w.-]+$', s) or re.match(r'^[\d:]+ ?[AP]?M?$', s) or re.match(r'^(Mon|Tue|Wed|Thu|Fri|Sat|Sun)', s) or re.match(r'^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d', s)
            or re.match(r'^(Ctrl|Shift|Alt)\+', s) or len(s) <= 1 or re.match(r'^g then \w$', s) or s in ('Halyard', 'English', 'Deutsch', 'Español', 'Français', 'Italiano', 'Norsk bokmål', 'Tiếng Việt', 'OpenPGP', 'S/MIME', 'SSL/TLS', 'common', 'demo', 'GitHub', 'Tx', 'U')
            or re.match(r'^[A-Z][a-z]+ [A-Z][a-z]+( <.*)?$', s) or re.match(r'^/', s) or '@' in s)
miss = [s for s in shown if s not in ks and not any(p.match(s) for p in pats) and not ignorable(s)]
print('\n'.join(miss)); print(len(miss), 'phrases missing', file=sys.stderr)
