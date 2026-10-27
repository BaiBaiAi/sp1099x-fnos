"""Readable, bounded native journal/status view on tty1.

No authentication input or shell is exposed. Service logs are selected rather
than flooding the display with vendor battery debug messages. ANSI/control
bytes from log data are stripped before rendering.
"""
import argparse,datetime,fcntl,os,pathlib,re,shutil,signal,struct,subprocess,time
p=argparse.ArgumentParser()
p.add_argument('--once',action='store_true')
p.add_argument('--tty',default='/dev/tty1',choices=['/dev/tty1'])
a=p.parse_args()
units=['docker.service','trim_app_center.service','systemd-networkd.service',
       'smbd.service','sp1099x-native-gauge.service']
def command(args,timeout=4):
    try:return subprocess.run(args,text=True,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=timeout).stdout.strip()
    except (OSError,subprocess.TimeoutExpired):return ''
def clean(s):
    s=re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]','',s)
    return ''.join(c if 32<=ord(c)<127 else '?' for c in s)
def read(path,default='?'):
    try:return pathlib.Path(path).read_text().strip()
    except OSError:return default
fd=os.open(a.tty,os.O_WRONLY|os.O_NOCTTY)
previous=None
try:
    while True:
        try:rows,cols,_,_=struct.unpack('HHHH',fcntl.ioctl(fd,0x5413,b'\0'*8))
        except OSError:rows,cols=37,120
        rows=max(10,min(rows or 37,120));cols=max(40,min(cols or 120,200))
        uptime=float(read('/proc/uptime','0').split()[0])
        disk=shutil.disk_usage('/vol1')
        bat=read('/sys/class/power_supply/battery/capacity')
        state=read('/sys/class/power_supply/battery/status')
        route=command(['ip','-4','route','get','1.1.1.1'])
        match=re.search(r'\bsrc\s+(\d+\.\d+\.\d+\.\d+)',route)
        ip=match.group(1) if match else command(['hostname','-I'])
        services=command(['systemctl','is-active','docker','trim_app_center']).splitlines()
        header=['SP1099X  |  native fnOS system log console',
            datetime.datetime.now().strftime('%Y-%m-%d %H:%M:%S')+f'  uptime {int(uptime//3600)}h {int(uptime%3600//60)}m',
            f'Web http://{ip}:5666  |  SSH port 2222',
            f'Battery {bat}% ({state})  |  /vol1 free {disk.free/1024**3:.2f} GiB',
            'Docker / App center: '+' / '.join(services),
            '-'*(cols-1),'Recent service journal (read only):']
        args=['journalctl','--no-pager','--output=short-iso','-n',str(rows-len(header)-2)]
        for u in units:args+=['-u',u]
        logs=command(args).splitlines()
        # journal entries can span multiple physical lines: retain the title
        # and status even when the selected entry count expands into a stack.
        logslots=max(1,rows-len(header)-1)
        lines=[clean(s)[:cols-1] for s in header+logs[-logslots:]]
        # Do not let a log line create terminal escape sequences or autowrap.
        frame='\x1b[?25l\x1b[H'+'\r\n'.join(s.ljust(cols-1) for s in lines)+'\x1b[J'
        if frame!=previous:os.write(fd,frame.encode('ascii'));previous=frame
        if a.once:break
        time.sleep(5)
finally:
    os.close(fd)
