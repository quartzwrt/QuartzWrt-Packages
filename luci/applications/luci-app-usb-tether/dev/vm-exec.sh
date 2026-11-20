#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# luci-app-usb-tether dev 工具：把一段 shell 脚本投进测试 VM 执行并回显输出
# （复用 vm-e2e.sh 的 pty 驱动逻辑：空密码/密码两段认证 + 提示符/空转兜底发车，
#  heredoc 逐行节流防 canonical 队列丢字节）。
#
# 用法：dev/vm-exec.sh <脚本文件>      # 或 stdin
# 环境变量：SSH_PORT(2222) VM_PASS(quartz)

set -euo pipefail
SSH_PORT="${SSH_PORT:-2222}"
VM_PASS="${VM_PASS:-quartz}"

if [ -n "${1:-}" ] && [ -f "$1" ]; then
	INNER="$(cat "$1")"
else
	INNER="$(cat)"
fi
[ -n "$INNER" ] || { echo "usage: $0 <script>" >&2; exit 1; }

export SSH_PORT VM_PASS INNER
python3 - <<'PYEOF'
import os, pty, select, sys, time

port = os.environ["SSH_PORT"]
passwords = [os.environ.get("VM_PASS", "quartz"), ""]
inner = os.environ["INNER"]

payload = "\n".join([
	"cat > /tmp/vmexec.sh <<'XEOF'",
	inner, "XEOF",
	"sh /tmp/vmexec.sh",
	"echo VMEXEC-DONE",
	"exit",
]) + "\n"

cmd = ["ssh", "-p", port,
       "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
       "-o", "PreferredAuthentications=password,none", "-o", "PubkeyAuthentication=no",
       "-o", "LogLevel=ERROR", "root@127.0.0.1"]

pid, fd = pty.fork()
if pid == 0:
	os.execvp(cmd[0], cmd)

buf = b""
sent_pass = 0
sent_payload = False
idle = 0
deadline = time.time() + 120

def send_payload():
	for line in payload.splitlines(keepends=True):
		try:
			os.write(fd, line.encode())
		except OSError:
			break
		time.sleep(0.03)
	time.sleep(1)

while time.time() < deadline:
	r, _, _ = select.select([fd], [], [], 1)
	if not r:
		idle += 1
		if not sent_payload and idle >= 6:
			send_payload()
			sent_payload = True
		continue
	try:
		data = os.read(fd, 4096)
	except OSError:
		break
	if not data:
		break
	buf += data
	idle = 0
	sys.stdout.write(data.decode(errors="replace"))
	sys.stdout.flush()
	low = buf.lower()
	if b"password:" in low and sent_pass < len(passwords):
		time.sleep(0.3)
		os.write(fd, (passwords[sent_pass] + "\n").encode())
		sent_pass += 1
		buf = b""
	elif not sent_payload and (b":~# " in low or b":~$" in low):
		time.sleep(0.3)
		send_payload()
		sent_payload = True
PYEOF
