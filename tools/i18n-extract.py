#!/usr/bin/env python3
"""List the English phrases the window can show, from the source of the UI files. The result is ui/web/i18n/keys.json:
every language catalog must have each of them (tools/i18n-check.mjs checks it)."""
import json, re, sys, html
from html.parser import HTMLParser

FILES = ['app.js', 'features.js', 'panels.js', 'message.js', 'composer.js', 'index.html']
ROOT = 'ui/web/'

class P(HTMLParser):
    def __init__(self):
        super().__init__(); self.out = set(); self.skip = 0
    def handle_starttag(self, tag, attrs):
        if tag in ('script', 'style', 'textarea', 'pre'): self.skip += 1
        for k, v in attrs:
            if k in ('title', 'placeholder', 'aria-label', 'alt') and v: add(self.out, v)
    def handle_endtag(self, tag):
        if tag in ('script', 'style', 'textarea', 'pre') and self.skip: self.skip -= 1
    def handle_data(self, d):
        if not self.skip: add(self.out, d)

def add(out, s):
    s = html.unescape(s)
    s = re.sub(r'\s+', ' ', s).strip()
    s = re.sub(r'^[">\s]+', '', s)
    if s.endswith('(') or s.startswith((',', ')', '.')) or s.endswith(' ('): return
    if len(s) < 2 or not re.search(r'[A-Za-z]{2}', s): return
    if re.search(r'[{}\[\]=]|=>|\\|\bfunction\b|\.js\b|^#|^\.', s) and not re.search(r'\{\w+\}', s): return
    out.add(s)

def strings(src):
    # single and double quoted literals and the text of template literals, with simple escapes
    for m in re.finditer(r"'((?:[^'\\\n]|\\.)*)'|\"((?:[^\"\\\n]|\\.)*)\"", src):
        v = m.group(1) if m.group(1) is not None else m.group(2)
        v = v.replace("\\'", "'").replace('\\"', '"').replace('\\u00f1', 'ñ')
        yield v

keys = set()
for f in FILES:
    src = open(ROOT + f, encoding='utf-8').read()
    if f.endswith('.html'):
        p = P(); p.feed(src); keys |= p.out; continue
    for m in re.finditer(r"\bt\(\s*(?:'((?:[^'\\\n]|\\.)*)'|\"((?:[^\"\\\n]|\\.)*)\")", src):
        v = m.group(1) if m.group(1) is not None else m.group(2)
        v = v.replace("\\'", "'").replace('\\"', '"')
        keys.add(re.sub(r'\s+', ' ', v).strip())
    for v in strings(src):
        if '<' in v and '>' in v:
            p = P(); p.feed(v); keys |= p.out
        else:
            # plain strings: kept when they read like text (a space, or a capital first letter), not code
            if re.match(r'^[A-Z][A-Za-z ,.\'!?()-]+[.!?]?$', v) or (' ' in v and re.match(r'^[A-Za-z][A-Za-z ,.\'!?()-]+[.!?]?$', v)):
                add(keys, v)

# phrases that appear on screen only after the program joins words and numbers together
EXTRA = ['{n} selected', '{n} conversation', '{n} conversations', '{n} message(s) did not send: {x}', 'Draft', '1 unread message', '{n} unread messages', 'No unread mail',
         'Archive (e)', 'Report spam (!)', 'Delete (#)', 'Mark as read (Shift+I)', 'Mark as unread (Shift+U)', 'Snooze (b)', 'Back (u)', 'Newer (k)', 'Older (j)',
         'yes', 'no', 'maybe', 'Retry', 'Folders', 'Menu', 'Sync now', 'Settings', 'Message', 'Later today', 'Tomorrow', 'This weekend', 'Next week', 'Snooze until', 'Snooze', 'To recipients', 'Cc recipients', 'Bcc recipients', 'Could not find this server automatically. Enter its address.', 'IMAP server', 'POP3 (downloads mail)', 'Mark read or unread', 'contains', 'is', 'all', 'any', 'Use as the default for {x}', 'Cc', 'Bcc', 'S/MIME (a self signed certificate)', 'Search mail (from: to: subject: is:unread has:attachment before: larger:)', 'any free port']
JUNK = set(['ArrowDown', 'ArrowUp', 'Alt', 'Backspace', 'BR', 'Bearer', 'CANCEL', 'Content-Type', 'Control', 'Ctrl', 'Enter', 'Escape', 'GET', 'INPUT', 'Meta', 'POST', 'REPLY', 'SELECT', 'Shift', 'Space', 'TENTATIVE', 'TEXTAREA',
            'Tab', 'X-Token', 'ACCEPTED', 'DECLINED', 'NEEDS-ACTION', 'Tx', 'allow-same-origin allow-modals', 'copts hide', 'mxbar warn', 'scrim palette', 'use strict', 'myaccount.google.com/apppasswords', 'myaccount.google.com/security',
            '/home/me/.thunderbird/xxxx.default-release', 'you@example.com', 'Deutsch', 'English', 'Italiano', 'Added', 'Could not attach', 'Show only', 'Sign in with', 'Signed by', 'Welcome to', 'Snoozed until', 'Go to',
            'Delete everything in', 'Turn on 2-Step Verification at', 'redirect of', 'You have no', 'Organizer:', 'Repeats:', 'You answered:', 'g then a', 'g then d', 'g then i', 'g then s', 'g then t', 'o or Enter', 'Mobile and desktop',
            'it does not match', 'could not read the source', 'no key', ', name it', 'Desktop app', 'Account id to run on ('])
def drop(k):
    if k in JUNK or k.startswith(';'): return True
    if k in ('Halyard', 'MCP', 'IMAP', 'POP3', 'JMAP', 'SMTP', 'CalDAV', 'CardDAV', 'OpenPGP', 'S/MIME', 'Sieve', 'Cc', 'Bcc'): return True
    if re.match(r'^[a-z][a-z.:_-]*$', k): return True
    if re.match(r'^(data|aria|role|text|image|application|message|multipart)[/-]', k): return True
    return False
keys = sorted(set(k for k in keys if not drop(k)) | set(EXTRA))
json.dump(keys, open(ROOT + 'i18n/keys.json', 'w', encoding='utf-8'), ensure_ascii=False, indent=0)
print(len(keys), 'phrases')
