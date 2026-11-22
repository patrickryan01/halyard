#!/usr/bin/env python3
# Delivers one message from bob to alice on the lab (host side), so the running app has something new to announce.
import smtplib, ssl, sys, email.message, time
m = email.message.EmailMessage()
m['From'] = 'Bob Lab <bob@lab.test>'; m['To'] = 'alice@lab.test'; m['Subject'] = 'Notification check ' + str(int(time.time()))
m.set_content('A new message while the app is open.')
ctx = ssl.create_default_context(); ctx.check_hostname = False; ctx.verify_mode = ssl.CERT_NONE
with smtplib.SMTP_SSL('127.0.0.1', 18465, context=ctx) as s:
    s.login('bob@lab.test', 'labpass-bob'); s.send_message(m)
print('sent')
