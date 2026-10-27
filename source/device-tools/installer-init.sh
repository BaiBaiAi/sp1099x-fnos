#!/bin/busybox sh
# SP1099X native Linux bring-up. Only cache diagnostic file and exact BCB
# command handling may write storage; no formatting or OS/data installation.
export PATH=/bin:/sbin
[ -r /proc/self/status ] || /bin/busybox mount -t proc proc /proc
if [ -e /proc/sp1099x_diag ]; then exec 2>/proc/sp1099x_diag; fi
set -x
/bin/busybox --install -s /bin
[ -d /sys/class ] || mount -t sysfs sysfs /sys
if [ ! -c /dev/null ]; then mount -t devtmpfs devtmpfs /dev || {
    mount -t tmpfs tmpfs /dev
    mknod /dev/console c 5 1
    mknod /dev/null c 1 3
    mknod /dev/zero c 1 5
    mknod /dev/kmsg c 1 11
}; fi
mount -t tmpfs tmpfs /run
exec >/run/stage0.log 2>&1
if [ -e /proc/sp1099x_diag ]; then exec 2>/proc/sp1099x_diag; fi
set -x
[ -c /dev/kmsg ] || { rm -f /dev/kmsg; mknod /dev/kmsg c 1 11; }
if [ ! -c /dev/pmsg0 ] && [ -r /sys/class/misc/pmsg0/dev ]; then
    NUM=$(cat /sys/class/misc/pmsg0/dev)
    mknod /dev/pmsg0 c "${NUM%:*}" "${NUM#*:}"
fi
mkdir -p /dev/pts /sys/kernel/config
mount -t devpts devpts /dev/pts
mount -t configfs configfs /sys/kernel/config
log() { echo "SP1099X-STAGE0: $*"; echo "<3>SP1099X-STAGE0: $*" >/dev/kmsg; }
log "native-init pid=$$ kernel=$(uname -r)"
# The original Android kernel lacks devtmpfs. Make only the block nodes needed
# for read-only diagnostics when testing this ramdisk with that known kernel.
for DEV in /sys/class/block/*; do
    [ -r "$DEV/dev" ] || continue
    NUM=$(cat "$DEV/dev")
    [ -e "/dev/${DEV##*/}" ] || mknod "/dev/${DEV##*/}" b "${NUM%:*}" "${NUM#*:}"
done
if [ -c /dev/watchdog ]; then watchdog -t 2 -T 20 /dev/watchdog & fi

G=/sys/kernel/config/usb_gadget/sp1099x
mkdir -p "$G"
echo 0x1d6b >"$G/idVendor"
echo 0x0104 >"$G/idProduct"
echo 0x0200 >"$G/bcdUSB"
echo 0x0100 >"$G/bcdDevice"
echo 0xef >"$G/bDeviceClass"
echo 0x02 >"$G/bDeviceSubClass"
echo 0x01 >"$G/bDeviceProtocol"
mkdir -p "$G/strings/0x409" "$G/configs/c.1/strings/0x409"
echo SP1099X-STAGE0 >"$G/strings/0x409/serialnumber"
echo 'Local Linux bring-up' >"$G/strings/0x409/manufacturer"
echo 'SP1099X native Linux diagnostic' >"$G/strings/0x409/product"
echo 'RNDIS and serial diagnostics' >"$G/configs/c.1/strings/0x409/configuration"
echo 250 >"$G/configs/c.1/MaxPower"
mkdir -p "$G/functions/rndis.usb0" "$G/functions/acm.GS0"
echo 02:53:50:10:99:01 >"$G/functions/rndis.usb0/dev_addr"
echo 02:53:50:10:99:02 >"$G/functions/rndis.usb0/host_addr"
if [ -d "$G/os_desc" ]; then
    echo 1 >"$G/os_desc/use"
    echo 0xcd >"$G/os_desc/b_vendor_code"
    echo MSFT100 >"$G/os_desc/qw_sign"
    if [ -d "$G/functions/rndis.usb0/os_desc/interface.rndis" ]; then
        echo RNDIS >"$G/functions/rndis.usb0/os_desc/interface.rndis/compatible_id"
        echo 5162001 >"$G/functions/rndis.usb0/os_desc/interface.rndis/sub_compatible_id"
    fi
    ln -s "$G/configs/c.1" "$G/os_desc/c.1"
fi
ln -s "$G/functions/rndis.usb0" "$G/configs/c.1/rndis.usb0"
ln -s "$G/functions/acm.GS0" "$G/configs/c.1/acm.GS0"
for WAIT_UDC in 1 2 3 4 5 6 7 8 9 10; do
    UDC=$(ls /sys/class/udc | head -1)
    [ -n "$UDC" ] && break
    sleep 1
done
if [ -n "$UDC" ]; then echo "$UDC" >"$G/UDC"; log "usb-bound=$UDC"; else log 'no-udc'; fi
if [ ! -c /dev/ttyGS0 ] && [ -r /sys/class/tty/ttyGS0/dev ]; then
    NUM=$(cat /sys/class/tty/ttyGS0/dev)
    mknod /dev/ttyGS0 c "${NUM%:*}" "${NUM#*:}"
fi

for NET in rndis0 usb0; do
    if [ -d "/sys/class/net/$NET" ]; then
        ifconfig "$NET" 192.168.77.1 netmask 255.255.255.0 up
        echo "start 192.168.77.2
end 192.168.77.2
interface $NET
option subnet 255.255.255.0
option lease 120
lease_file /run/udhcpd.leases
pidfile /run/udhcpd.pid" >/run/udhcpd.conf
        udhcpd /run/udhcpd.conf
        log "usb-network=$NET address=192.168.77.1"
        break
    fi
done

set +x
mkdir -p /www/cgi-bin
cat >/www/cgi-bin/status <<'EOF'
#!/bin/busybox sh
printf 'Content-Type: text/plain\r\nCache-Control: no-store\r\n\r\n'
cat /run/install.state 2>/dev/null || echo starting
EOF
cat >/www/cgi-bin/log <<'EOF'
#!/bin/busybox sh
printf 'Content-Type: text/plain\r\nCache-Control: no-store\r\n\r\n'
cat /run/install.log 2>/dev/null
EOF
chmod 755 /www/cgi-bin/*
echo waiting-for-pc >/run/install.state
httpd -p 192.168.77.1:8080 -h /www
# The installer is RAM-only until the verified engine explicitly starts.
BCB=$(dd if=/dev/mmcblk0p3 bs=32 count=1 2>/dev/null | tr -d '\000')
if [ "$BCB" = boot-recovery ]; then
    dd if=/dev/zero of=/dev/mmcblk0p3 bs=32 count=1 conv=notrunc
    sync
elif [ "$BCB" = bootonce-bootloader ]; then
    echo 'Preserving known stock bootonce-bootloader BCB value' >>/run/install.log
elif [ -n "$BCB" ]; then
    echo "Unexpected BCB command: refusing installation" >/run/install.log
    echo failed >/run/install.state
    while :; do sleep 5; done
fi
mount -t tmpfs -o remount,size=2400m tmpfs /run
SERVER=http://192.168.77.2:18777
READY=0
for ATTEMPT in $(seq 1 450); do
    if wget -T 3 -O /run/session.json "$SERVER/session.json"; then READY=1; break; fi
    sleep 2
done
if [ "$READY" = 1 ]; then
    echo downloading-root >/run/install.state
    OK=1
    for FILE in rootfs.sha256 rootfs.squashfs boot.img recovery.img original-boot.img para.img gpt-primary.bin gpt-secondary.bin authorized_keys; do
        if ! wget -T 60 -O "/run/$FILE" "$SERVER/$FILE"; then OK=0; break; fi
    done
    if [ "$OK" = 1 ] && sha256sum -c /run/rootfs.sha256; then
        echo preparing-ram-tools >/run/install.state
        [ -b /dev/loop0 ] || mknod /dev/loop0 b 7 0
        if losetup -r /dev/loop0 /run/rootfs.squashfs && mount -t squashfs -o ro /dev/loop0 /lower; then
            mkdir -p /run/tool-overlay/upper /run/tool-overlay/work
            if mount -t overlay -o lowerdir=/lower,upperdir=/run/tool-overlay/upper,workdir=/run/tool-overlay/work overlay /tools; then
                for D in dev proc sys run; do mount --bind /$D /tools/$D; done
                chroot /tools /bin/bash /usr/local/libexec/sp1099x-release/install-engine.sh
            fi
        fi
    fi
fi
echo failed >/run/install.state
echo 'Installer stopped. Keep PC backup; read install log. Do not repeatedly flash.' >>/run/install.log
while :; do sleep 5; done
