#!/bin/sh
# Hold ASIC startup until this boot's controller has recovered pending settings.
while test ! -f /tmp/paperclip-ready || test -f /usr/config/paperclip/halted.json; do sleep 2; done
exec xminerd
