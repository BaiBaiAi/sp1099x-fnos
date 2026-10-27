#!/usr/bin/python3
"""Temporary protection while the Android fuel-gauge SOC daemon is unavailable.

Never synthesizes a percentage. Retains kernel/PMIC thermal and undervoltage
protection and requests an orderly shutdown at sustained low measured voltage.
"""
import pathlib, subprocess, time, json, sys
base=pathlib.Path('/sys/class/power_supply/battery')
def sample():
    result={}
    for key in ('voltage_now','status','capacity','temp'):
        result[key]=(base/key).read_text().strip()
    result['voltage_uv']=int(result['voltage_now'])
    if not 2000000 <= result['voltage_uv'] <= 5000000:
        raise ValueError('Battery voltage reading outside valid measurement range')
    return result
if '--check' in sys.argv:
    print(json.dumps(sample())); sys.exit(0)
low=bad=0
while True:
    try:
        state=sample(); bad=0
        # Conservative threshold above the existing PMIC shutdown thresholds.
        low=low+1 if state['voltage_uv'] < 3500000 else 0
        if low or int(time.monotonic())%60 < 10:
            print(json.dumps({'sample':state,'low_samples':low}),flush=True)
    except (OSError,ValueError) as e:
        bad+=1; print('Battery measurement failed: '+str(e),flush=True)
    if low >= 6 or bad >= 6:
        print('Requesting orderly poweroff: sustained low voltage or unavailable measurement',flush=True)
        subprocess.run(['/usr/bin/systemctl','poweroff'],check=True)
        time.sleep(60)
    time.sleep(10)
