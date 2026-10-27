#!/bin/bash
# Runs only in the RAM overlay of the verified release squashfs, never Android.
set -euo pipefail
exec >>/run/install.log 2>&1
trap 'echo "FAILED line=$LINENO"; echo failed >/run/install.state; sync' ERR
echo verifying >/run/install.state
test "$(findmnt -n -o FSTYPE /)" = overlay
test "$(cat /sys/class/block/mmcblk0/size)" = 60620800
! findmnt -rn -S /dev/mmcblk0p35 >/dev/null
python3 - <<'PY'
import sys,pathlib,json,hashlib,subprocess
sys.path.insert(0,'/usr/local/libexec/sp1099x-release')
import layout
data=json.loads(pathlib.Path('/run/session.json').read_text())
assert data['format']==1 and data['confirmed_erase'] is True
assert data['cid'].lower()==pathlib.Path('/sys/class/block/mmcblk0/device/cid').read_text().strip().lower(),'Wrong target device'
for name,expected in data['files'].items():
    assert '/' not in name and name not in ('.','..')
    p=pathlib.Path('/run')/name
    h=hashlib.sha256()
    with p.open('rb') as f:
        for block in iter(lambda:f.read(1024*1024),b''):h.update(block)
    assert h.hexdigest()==expected, name+' checksum mismatch'
ref=json.loads(pathlib.Path('/usr/share/sp1099x-release/hardware.json').read_text())
assert data['mode'] in ('install','restore')
if data['mode']=='restore':
    # Validate both backed-up GPT headers and partition-array CRCs before
    # allowing the restore branch. Raw writes cover GPT sectors only.
    import struct,zlib
    primary=pathlib.Path('/run/gpt-primary.bin').read_bytes();tail=pathlib.Path('/run/gpt-secondary.bin').read_bytes()
    assert len(primary)==len(tail)==1048576
    for raw,offset in [(primary,512),(tail,len(tail)-512)]:
        header=bytearray(raw[offset:offset+512]);assert header[:8]==b'EFI PART'
        size,crc=struct.unpack_from('<II',header,12);assert 92<=size<=512
        struct.pack_into('<I',header,16,0);assert zlib.crc32(header[:size])==crc
    assert struct.unpack_from('<II',primary,512+80)==(128,128)
    assert zlib.crc32(primary[1024:1024+16384])==struct.unpack_from('<I',primary,512+88)[0]
    assert zlib.crc32(tail[-512-16384:-512])==struct.unpack_from('<I',tail,len(tail)-512+88)[0]
    sys.exit(0)
t=json.loads(subprocess.check_output(['sfdisk','--json','/dev/mmcblk0']))
layout.validate(t,ref)
for number,key in [(25,'original_boot_sha256'),(27,'dtbo_sha256')]:
    actual=subprocess.check_output(['sha256sum',f'/dev/mmcblk0p{number}'],text=True).split()[0]
    assert actual==data[key],f'Partition {number} changed since Android backup'
assert data['dtbo_sha256']==ref['dtbo_sha256']
# Recovery currently contains our temporary installer. The original is a
# per-device verified backup, never a recovery cloned from another tablet.
assert data['files']['recovery.img']==ref['recovery_sha256']
plan=layout.plan_dump(subprocess.check_output(['sfdisk','--dump','/dev/mmcblk0'],text=True))
pathlib.Path('/run/install-layout.sfdisk').write_text(plan)
PY
MODE=$(python3 -c 'import json;print(json.load(open("/run/session.json"))["mode"])')
if [ "$MODE" = restore ]; then
    echo restoring-gpt >/run/install.state
    blockdev --setrw /dev/mmcblk0
    dd if=/run/gpt-primary.bin of=/dev/mmcblk0 bs=512 count=34 conv=fsync
    dd if=/run/gpt-secondary.bin of=/dev/mmcblk0 bs=512 skip=2015 seek=60620767 count=33 conv=fsync
    blockdev --rereadpt /dev/mmcblk0
    for n in 2 3 25; do blockdev --setrw /dev/mmcblk0p$n; done
    dd if=/run/recovery.img of=/dev/mmcblk0p2 bs=1M conv=fsync
    dd if=/run/para.img of=/dev/mmcblk0p3 bs=512 conv=fsync
    dd if=/run/original-boot.img of=/dev/mmcblk0p25 bs=1M conv=fsync
    for pair in '2 recovery.img' '3 para.img' '25 original-boot.img'; do
        read -r n filename <<<"$pair"
        test "$(sha256sum /dev/mmcblk0p$n | cut -d' ' -f1)" = "$(sha256sum /run/$filename | cut -d' ' -f1)"
    done
    sync
    echo restored >/run/install.state
    sleep 15
    /usr/local/libexec/sp1099x/native-reboot
    exit 0
fi
test ! -e /sys/class/block/mmcblk0p38
test "$(cat /sys/class/block/mmcblk0p35/start)" = 10862592
test "$(cat /sys/class/block/mmcblk0p35/size)" = 49637343
echo restoring-original-recovery >/run/install.state
blockdev --setrw /dev/mmcblk0
blockdev --setrw /dev/mmcblk0p2
dd if=/run/recovery.img of=/dev/mmcblk0p2 bs=1M conv=fsync
test "$(sha256sum /dev/mmcblk0p2 | cut -d' ' -f1)" = "$(sha256sum /run/recovery.img | cut -d' ' -f1)"
echo checking-own-calibration >/run/install.state
mkdir -p /mnt/original-nvdata /run/own-nvdata
chmod 0700 /run/own-nvdata
mount -t ext4 -o ro,noload /dev/mmcblk0p7 /mnt/original-nvdata
test -d /mnt/original-nvdata/APCFG
cp -a /mnt/original-nvdata/APCFG /run/own-nvdata/
umount /mnt/original-nvdata
echo partitioning >/run/install.state
sfdisk --wipe never --wipe-partitions never /dev/mmcblk0 </run/install-layout.sfdisk
blockdev --rereadpt /dev/mmcblk0 || true
for n in $(seq 1 20); do test ! -b /dev/mmcblk0p38 || break; sleep 1; done
python3 - <<'PY'
import sys,json,pathlib,subprocess
sys.path.insert(0,'/usr/local/libexec/sp1099x-release');import layout
layout.validate(json.loads(subprocess.check_output(['sfdisk','--json','/dev/mmcblk0'])),json.loads(pathlib.Path('/usr/share/sp1099x-release/hardware.json').read_text()),installed=True)
PY
echo formatting-root >/run/install.state
mkfs.ext4 -F -b 4096 -m 1 -L fnos-root -O ^64bit,^metadata_csum_seed,^orphan_file -E lazy_itable_init=0,lazy_journal_init=0 /dev/mmcblk0p35
mkdir -p /mnt/target
mount -t ext4 -o noatime /dev/mmcblk0p35 /mnt/target
echo copying-root >/run/install.state
rsync -aHAXSx --numeric-ids --exclude='/dev/***' --exclude='/proc/***' --exclude='/sys/***' --exclude='/run/***' --exclude='/tmp/***' --exclude='/mnt/***' --exclude='/media/***' --exclude='/vol1/***' / /mnt/target/
DEST=/mnt/target
mkdir -p "$DEST"/{dev,proc,sys,run,tmp,mnt,media,vol1,root/.ssh}
chmod 1777 "$DEST/tmp"
chmod 0700 "$DEST/root/.ssh"
install -m 0600 /run/authorized_keys "$DEST/root/.ssh/authorized_keys"
# Retain this tablet's own private calibration, never release-builder NVRAM.
mkdir -p "$DEST/mnt/vendor/nvdata"
cp -a /run/own-nvdata/APCFG "$DEST/mnt/vendor/nvdata/"
chown -R 0:0 "$DEST/mnt/vendor/nvdata"
chmod 0700 "$DEST/mnt/vendor/nvdata"
echo creating-data >/run/install.state
VG="trim_$(cat /proc/sys/kernel/random/uuid | tr '-' '_')"
LVM_CONFIG='activation { udev_sync=0 udev_rules=0 }'
pvcreate --config "$LVM_CONFIG" -y /dev/mmcblk0p38
vgcreate --config "$LVM_CONFIG" "$VG" /dev/mmcblk0p38
lvcreate --config "$LVM_CONFIG" -l 100%FREE -n 0 "$VG"
dmsetup mknodes
DM="/dev/mapper/${VG}-0"
mkfs.btrfs -L fnos-data -O extref,skinny-metadata,no-holes "$DM"
ROOT_UUID=$(blkid -s UUID -o value /dev/mmcblk0p35)
DATA_UUID=$(blkid -s UUID -o value "$DM")
printf 'UUID=%s / ext4 defaults,noatime 0 1\nUUID=%s /vol1 btrfs defaults,noatime,space_cache=v2,nofail 0 0\n' "$ROOT_UUID" "$DATA_UUID" >"$DEST/etc/fstab"
printf '{"vg":"%s","filesystem_uuid":"%s"}\n' "$VG" "$DATA_UUID" >"$DEST/etc/sp1099x-release-volume.json"
printf 'VG=%s\nDM=%s\nFILESYSTEM_UUID=%s\nMOUNT=/vol1\n' "$VG" "$DM" "$DATA_UUID" >"$DEST/etc/sp1099x-data-volume"
printf 'sp1099x-%s\n' "$(cat /sys/class/block/mmcblk0/device/cid | tail -c 7)" >"$DEST/etc/hostname"
# A new installation gets its own software identifier, not the template's
# build-machine identifier. Official triminit regenerates its own RSA/trim key.
{ cat /sys/class/block/mmcblk0/device/cid; cat /proc/sys/kernel/random/uuid; } | sha1sum | cut -d' ' -f1 | tr -d '\n' >"$DEST/usr/trim/etc/machine_id"
NET_UUID=$(cat /proc/sys/kernel/random/uuid)
cat >"$DEST/etc/NetworkManager/system-connections/sp1099x-wired-dhcp.nmconnection" <<EOF
[connection]
id=sp1099x-wired-dhcp
uuid=$NET_UUID
type=ethernet
autoconnect=true
[ethernet]
[ipv4]
method=auto
route-metric=100
[ipv6]
method=disabled
EOF
chmod 0600 "$DEST/etc/NetworkManager/system-connections/sp1099x-wired-dhcp.nmconnection"
mkdir -p "$DEST/etc/docker"
cat >"$DEST/etc/docker/daemon.json" <<'EOF'
{"data-root":"/vol1/docker","log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"3"}}
EOF
# Unique keys are generated on the destination, not on the packaging machine.
ssh-keygen -A -f "$DEST"
ssh-keygen -q -t ed25519 -N '' -f "$DEST/etc/ssh/sp1099x_host_ed25519_key"
mkdir -p "$DEST/var/lib/sp1099x"
printf 'SSH_HOST_KEY ' >>/run/install.log
cat "$DEST/etc/ssh/sp1099x_host_ed25519_key.pub" >>/run/install.log
sync
echo verifying-installed-root >/run/install.state
test "$(sha256sum /usr/trim/bin/trim | cut -d' ' -f1)" = "$(sha256sum "$DEST/usr/trim/bin/trim" | cut -d' ' -f1)"
test -f "$DEST/etc/sp1099x-native-install"
umount "$DEST"
echo writing-final-boot >/run/install.state
blockdev --setrw /dev/mmcblk0p25
dd if=/run/boot.img of=/dev/mmcblk0p25 bs=1M conv=fsync
test "$(sha256sum /dev/mmcblk0p25 | cut -d' ' -f1)" = "$(sha256sum /run/boot.img | cut -d' ' -f1)"
sync
echo complete >/run/install.state
# Give the PC time to retain logs before hardware reboot.
sleep 15
/usr/local/libexec/sp1099x/native-reboot
