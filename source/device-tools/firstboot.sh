#!/bin/bash
set -euo pipefail
test -f /etc/sp1099x-native-install
test -f /etc/sp1099x-release-volume.json
test ! -e /var/lib/sp1099x/release-firstboot.done || exit 0
findmnt --mountpoint /vol1 >/dev/null
install -d -m 0711 /vol1/appcenter-downloads
python3 - <<'PY'
import json,pathlib,subprocess,re,time
data=json.loads(pathlib.Path('/etc/sp1099x-release-volume.json').read_text())
vg=data['vg'];assert re.fullmatch(r'trim_[0-9a-f_]{36}',vg)
def sql(q):
    return subprocess.check_output(['runuser','-u','postgres','--','psql','-d','trim','-At','-v','ON_ERROR_STOP=1','-c',q],cwd='/tmp',text=True).strip()
for attempt in range(90):
    if sql("select to_regclass('public.mount') is not null")=='t':break
    time.sleep(1)
else:raise RuntimeError('Vendor storage schema was not created; no foreign data copied')
existing=sql('select uuid from mount order by id')
if not existing:
    sql("INSERT INTO mount (id,uuid,comment,sync_speed_min,sync_speed_max,stripe_cache_size,warn_frsize,grow,state,pool_type) VALUES (1,'"+vg+"-0','SP1099X internal eMMC',-1,-1,4096,10,'',0,0)")
else:assert existing==vg+'-0','Fresh installation must not contain another data volume'
PY
install -d -m 0755 /var/lib/sp1099x
touch /var/lib/sp1099x/release-firstboot.done
sync
systemctl --no-block try-restart trim_main.service
