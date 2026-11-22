#!/usr/bin/env python3
"""Makes the phone crew's winvm.py (copied to ~/work/MAIL/win) use its own server port, so two crews can run VMs side by side.
The guest agent in the disk always calls 10.0.2.2:8800; with WINVM_PORT set, QEMU forwards that address to our own port instead.
Usage: patch-winvm.py PATH_TO_winvm.py"""
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace('MON = f"{W}/mon.sock"; PID = f"{W}/qemu.pid"', 'MON = f"{W}/mon.sock"; PID = f"{W}/qemu.pid"\nPORT = int(os.environ.get("WINVM_PORT", "8800"))')
s = s.replace('ThreadingHTTPServer(("127.0.0.1", 8800), H)', 'ThreadingHTTPServer(("127.0.0.1", PORT), H)')
s = s.replace('"http://127.0.0.1:8800" + path', 'f"http://127.0.0.1:{PORT}" + path')
s = s.replace('"-netdev", "user,id=n0"', '"-netdev", "user,id=n0" + (f",guestfwd=tcp:10.0.2.2:8800-tcp:127.0.0.1:{PORT}" if PORT != 8800 else "")')
open(p, 'w').write(s)
print("patched", p)
