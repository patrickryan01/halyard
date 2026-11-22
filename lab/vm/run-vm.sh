#!/bin/bash
# Boots a cloud image in QEMU/KVM, installs the built package in it, starts the app in a kiosk compositor,
# adds an account from the Podman lab through the app's own MCP endpoint, and takes screenshots.
#   run-vm.sh arch|suse PKGFILE OUTDIR      (run on the Beast, one VM at a time; the lab must be up: lab/stalwart.sh up)
set -euo pipefail
KIND=${1:?arch|suse}; PKG=${2:?package file}; OUT=${3:?outdir}
W=${VMDIR:-$HOME/halyard-build/vm}; mkdir -p "$W" "$OUT"
case $KIND in
arch) BASE_URL=https://geo.mirror.pkgbuild.com/images/latest/Arch-Linux-x86_64-cloudimg.qcow2 ;;
suse) BASE_URL=https://download.opensuse.org/tumbleweed/appliances/openSUSE-Tumbleweed-Minimal-VM.x86_64-Cloud.qcow2 ;;
esac
BASE=$W/base-$KIND.qcow2
[ -f "$BASE" ] || curl -fL --retry 3 -o "$BASE" "$BASE_URL"
DISK=$W/$KIND.qcow2; rm -f "$DISK"; qemu-img create -q -f qcow2 -F qcow2 -b "$BASE" "$DISK" 12G
[ -f "$W/key" ] || ssh-keygen -q -t ed25519 -N '' -f "$W/key"
PUB=$(cat "$W/key.pub")
# serve the package to the guest at 10.0.2.2:8099
SERVE=$W/serve; rm -rf "$SERVE"; mkdir -p "$SERVE"; cp "$PKG" "$SERVE/"
(cd "$SERVE" && exec python3 -m http.server 8099 --bind 127.0.0.1 >/dev/null 2>&1) & HTTP=$!
trap 'kill $HTTP $QEMU 2>/dev/null || true' EXIT
PKGNAME=$(basename "$PKG")
if [ $KIND = arch ]; then
  INSTALL="pacman -Syu --noconfirm --needed cage seatd mesa ttf-dejavu libinput xorg-xwayland python && pacman -U --noconfirm /tmp/$PKGNAME"
else
  INSTALL="zypper -n install --no-recommends cage seatd Mesa-dri dejavu-fonts libinput10 python3 libwebkit2gtk-4_1-0 libgtk-3-0 && rpm -i --nodeps /tmp/$PKGNAME"
fi
SEED=$W/seed-$KIND; rm -rf "$SEED"; mkdir -p "$SEED"
cat > "$SEED/meta-data" <<M
instance-id: halyard-$KIND-$RANDOM
local-hostname: halyard-$KIND
M
cat > "$SEED/user-data" <<U
#cloud-config
users:
  - name: tester
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    lock_passwd: false
    plain_text_passwd: tester
    groups: [wheel]
    ssh_authorized_keys: ["$PUB"]
ssh_pwauth: true
write_files:
  - path: /etc/systemd/system/halyard-kiosk.service
    content: |
      [Unit]
      Description=Halyard in a kiosk compositor
      After=systemd-user-sessions.service seatd.service getty@tty1.service
      Wants=seatd.service
      Conflicts=getty@tty1.service
      [Service]
      User=tester
      SupplementaryGroups=seat
      TTYPath=/dev/tty1
      StandardInput=tty
      StandardOutput=journal
      StandardError=journal
      Environment=HOME=/home/tester XDG_RUNTIME_DIR=/run/user/1000 WLR_RENDERER=pixman WLR_NO_HARDWARE_CURSORS=1 LIBSEAT_BACKEND=seatd WEBKIT_DISABLE_COMPOSITING_MODE=1 WEBKIT_DISABLE_DMABUF_RENDERER=1
      ExecStartPre=+/usr/bin/mkdir -p /run/user/1000
      ExecStartPre=+/usr/bin/chown tester /run/user/1000
      ExecStart=/usr/bin/cage -s -- /usr/bin/halyard
      Restart=on-failure
      [Install]
      WantedBy=multi-user.target
  - path: /usr/local/bin/add-lab-account
    permissions: '0755'
    content: |
      #!/usr/bin/env python3
      # add the lab IMAP account through the app's own MCP endpoint, as another program would
      import json, urllib.request, os, sys
      cfg = json.load(open(os.path.expanduser('/home/tester/.config/halyard/mcp.json')))
      def rpc(body, sid=None):
          h = {'Content-Type': 'application/json', 'Accept': 'application/json, text/event-stream', 'Authorization': 'Bearer ' + cfg['token']}
          if sid: h['Mcp-Session-Id'] = sid
          r = urllib.request.urlopen(urllib.request.Request(cfg['url'], json.dumps(body).encode(), h), timeout=60)
          return r.headers.get('Mcp-Session-Id'), r.read().decode()
      sid, _ = rpc({'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {'protocolVersion': '2025-06-18', 'capabilities': {}, 'clientInfo': {'name': 'vm-proof', 'version': '1'}}})
      rpc({'jsonrpc': '2.0', 'method': 'notifications/initialized'}, sid)
      for u in sys.argv[1:]:
          _, out = rpc({'jsonrpc': '2.0', 'id': 2, 'method': 'tools/call', 'params': {'name': 'add_account_password', 'arguments': {'kind': 'imap', 'email': u + '@lab.test', 'password': 'labpass-' + u, 'imapHost': '10.0.2.100', 'imapPort': 993, 'imapSecurity': 'ssl', 'smtpHost': '10.0.2.100', 'smtpPort': 465, 'smtpSecurity': 'ssl', 'skipVerify': True}}}, sid)
          print(out[:300])
runcmd:
  - [sh, -c, "curl -fsS -o /tmp/$PKGNAME http://10.0.2.2:8099/$PKGNAME"]
  - [sh, -c, "$INSTALL > /var/log/halyard-install.log 2>&1; echo INSTALL-EXIT=\$? >> /var/log/halyard-install.log"]
  - [sh, -c, "usermod -aG seat tester; systemctl daemon-reload; systemctl enable --now seatd; systemctl mask getty@tty1.service; systemctl enable --now halyard-kiosk.service"]
  - [sh, -c, "touch /var/lib/halyard-ready"]
U
xorriso -as mkisofs -quiet -V cidata -J -r -o "$W/seed-$KIND.iso" "$SEED" >/dev/null 2>&1
MON=$W/mon-$KIND.sock; rm -f "$MON"
qemu-system-x86_64 -enable-kvm -cpu host -smp 2 -m 3072 -machine q35 \
  -drive file="$DISK",if=virtio -drive file="$W/seed-$KIND.iso",if=virtio,media=cdrom,readonly=on \
  -netdev user,id=n0,hostfwd=tcp:127.0.0.1:2222-:22,guestfwd=tcp:10.0.2.100:993-tcp:127.0.0.1:18993,guestfwd=tcp:10.0.2.100:465-tcp:127.0.0.1:18465 \
  -device virtio-net-pci,netdev=n0 -device virtio-vga -display none -monitor unix:"$MON",server,nowait -daemonize -pidfile "$W/qemu-$KIND.pid"
QEMU=$(cat "$W/qemu-$KIND.pid")
shot() { python3 - "$MON" "$1" <<'PY'
import socket, sys, time
s = socket.socket(socket.AF_UNIX); s.connect(sys.argv[1]); s.settimeout(5)
time.sleep(0.3); s.recv(4096); s.send(('screendump ' + sys.argv[2] + '\n').encode()); time.sleep(1.5)
PY
}
SSH="ssh -i $W/key -p 2222 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 -o LogLevel=ERROR tester@127.0.0.1"
echo "waiting for the guest to install and start the app"
for i in $(seq 1 200); do $SSH 'test -f /var/lib/halyard-ready' 2>/dev/null && break; sleep 5; done
$SSH 'cat /var/log/halyard-install.log | tail -3; systemctl is-active halyard-kiosk; pacman -Qi halyard 2>/dev/null | head -3; rpm -qi halyard 2>/dev/null | head -3' || true
for i in $(seq 1 60); do $SSH 'test -f ~/.config/halyard/mcp.json' 2>/dev/null && break; sleep 3; done
sleep 8; shot "$OUT/$KIND-1-first-run.ppm"
$SSH '/usr/local/bin/add-lab-account alice' | tee "$OUT/$KIND-add-account.txt"
sleep 25; shot "$OUT/$KIND-2-inbox.ppm"
$SSH 'journalctl -u halyard-kiosk --no-pager | tail -15' > "$OUT/$KIND-kiosk.log" || true
echo "done: $OUT"
