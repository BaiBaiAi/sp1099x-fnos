#!/usr/bin/python3
"""Board-scoped WMT startup: one core power-on, then one WLAN probe."""
import sys,pathlib,subprocess,threading,hashlib,time

module=pathlib.Path('/lib/modules/4.9.117-sp1099x-bringup/extra/sp1099x/wmt_drv.ko')
assert subprocess.check_output(['uname','-r'],text=True).strip()=='4.9.117-sp1099x-bringup'
assert hashlib.sha256(module.read_bytes()).hexdigest()=='551b13f1eba7d43261353462d9baee0e71c72f5141d6e94aa2e0ccb78d6f8302'
assert not pathlib.Path('/sys/module/wmt_drv').exists(),'Refuse to reuse unknown WMT state; reboot first'
pending=pathlib.Path('/var/lib/sp1099x/native-wifi-boot-pending')
pending.parent.mkdir(parents=True,exist_ok=True)
assert not pending.exists(),'Previous Wi-Fi startup did not settle; keep wired recovery and inspect before retry'
pending.write_text(pathlib.Path('/proc/sys/kernel/random/boot_id').read_text())
sys.path.insert(0,'/usr/local/libexec/sp1099x/wifi-loader')
import loader,launcher

ready=threading.Event()
subprocess.run(['modprobe','wmt_drv','native_keep_efuse_vcn28=1'],check=True)
assert loader.do_loader()==0
# Existing kernel APIs; do not change unrelated clock buffers or regulators.
pathlib.Path('/sys/power/clk_buf/clk_buf_debug').write_text('11\n')
pathlib.Path('/proc/driver/wmt_dbg').write_text('0 0 0\n')
subprocess.run(['modprobe','wmt_chrdev_wifi'],check=True)
subprocess.run(['modprobe','wlan_drv_gen4m'],check=True)

def core_power(self):
    # arg1 silently does nothing without always_pwr_on_flag; arg2 is explicit.
    result=launcher.do_ioctl(self.fd,launcher.WMT_IOCTL_LPBK_POWER_CTRL,2)
    if result==0:
        print('Native Wi-Fi: WMT core initialized; enabling WLAN once',flush=True)
        ready.set()
    else:
        print(f'Native Wi-Fi: core initialization failed: {result}; no retry',flush=True)

def wifi_enable(self):
    if not ready.wait(60):
        print('Native Wi-Fi: core readiness timeout; no WLAN probe',flush=True)
        return
    try:
        with open(launcher.WMT_WIFI,'wt') as f:f.write('1')
        print('Native Wi-Fi: WLAN enable completed',flush=True)
        # The stock firmware emits a detailed stream with no Android log
        # consumer on fnOS. Disable that stream via its normal driver API;
        # retain kernel error reporting and normal service journal output.
        driver=pathlib.Path('/proc/net/wlan/driver')
        if driver.exists():
            driver.write_text('SET_FWLOG 0 0\n')
            print('Native Wi-Fi: detailed firmware host log disabled',flush=True)
        # A hard reset during early initialization leaves this marker behind;
        # the next boot then keeps Ethernet available without another probe.
        time.sleep(60)
        if pathlib.Path('/sys/class/net/wlan0').exists():
            pending.unlink()
            print('Native Wi-Fi: interface survived early settle; boot guard cleared',flush=True)
    except OSError as ex:
        print(f'Native Wi-Fi: WLAN enable failed: {ex!r}; no retry',flush=True)

launcher.Launcher._launcher_pwr_on_conn_thread=core_power
launcher.Launcher._launcher_wifi_enable_thread=wifi_enable
launcher.do_launcher()
