#!/bin/bash
# Explicit private POSIX share, without advertising Windows/TRIM ACL support.
set -euo pipefail
test "$#" = 2 || { echo 'usage: native-share.sh ACCOUNT SHARE_NAME'; exit 2; }
ACCOUNT=$1
SHARE=$2
[[ "$ACCOUNT" =~ ^[a-zA-Z_][a-zA-Z0-9_.-]*$ ]]
[[ "$SHARE" =~ ^[a-zA-Z][a-zA-Z0-9_-]{0,39}$ ]]
USER_ID=$(id -u "$ACCOUNT")
test "$USER_ID" -ge 1000
ROOT="/vol1/$USER_ID"
test -d "$ROOT" && test "$(realpath "$ROOT")" = "$ROOT"
test "$(stat -c %u "$ROOT")" = "$USER_ID"
pdbedit -L 2>/dev/null | cut -d: -f1 | grep -Fx "$ACCOUNT" >/dev/null
CONF=/etc/samba/sp1099x-native-shares.conf
touch "$CONF"
if testparm -s 2>/dev/null | grep -Fx "[$SHARE]" >/dev/null; then echo 'Share name already exists; choose another name'; exit 1; fi
runuser -u "$ACCOUNT" -- mkdir -m 0700 -- "$ROOT/$SHARE"
cat >>"$CONF" <<EOF
[$SHARE]
    path = $ROOT/$SHARE
    valid users = $ACCOUNT
    read only = no
    guest ok = no
    browseable = yes
    vfs objects =
    nt acl support = no
    create mask = 0600
    directory mask = 0700
    follow symlinks = no
    aio read size = 0
    aio write size = 0
EOF
grep -Fxq 'include = /etc/samba/sp1099x-native-shares.conf' /etc/samba/smb.conf || printf '\ninclude = /etc/samba/sp1099x-native-shares.conf\n' >>/etc/samba/smb.conf
testparm -s >/dev/null
smbcontrol all reload-config
echo "Private POSIX share created: $SHARE for $ACCOUNT"
