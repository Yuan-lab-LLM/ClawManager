#!/bin/sh
set -eu
mkdir -p /export/test /run/ganesha
chown 200522:1000 /export/test
chmod 0755 /export/test
exec ganesha.nfsd -F -L /dev/stdout -f /etc/ganesha/ganesha.conf
