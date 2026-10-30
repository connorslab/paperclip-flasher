#!/bin/sh
# Restore disk parameters before the native daemon and its API handshake.
until /usr/config/paperclip/controller -preflight >>/tmp/paperclip-preflight.log 2>&1; do sleep 5; done
while test -f /usr/config/paperclip/halted.json; do sleep 2; done
exec xminerd
