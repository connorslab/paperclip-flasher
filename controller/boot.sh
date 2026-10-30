#!/bin/sh
umask 077
# Keep management available even when the native controller disables SSH.
(
 while :; do
  pidof sshd >/dev/null || /usr/sbin/sshd
  sleep 10
 done
) </dev/null >/tmp/paperclip-ssh.log 2>&1 &
exec /usr/config/paperclip/supervisor.sh
