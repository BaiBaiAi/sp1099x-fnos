#!/usr/bin/python3
"""Estimate runtime from valid MTK coulomb-counter data and measured discharge.

No voltage-to-percentage curve or charging-current runtime is invented. A 5%
capacity reserve is an estimate margin, not a replacement shutdown policy.
"""
import json,math,os,pathlib,statistics,time
base=pathlib.Path('/sys/class/power_supply/battery')
directory=pathlib.Path('/var/lib/sp1099x');directory.mkdir(mode=0o755,exist_ok=True)
target=directory/'battery-runtime.json'
history=[];previous=None
try:
    old=json.loads(target.read_text())
    if old.get('last_discharge_average_uA',0)>10000 and 0<=time.time()-old.get('last_discharge_timestamp',0)<=7*86400:
        previous=old
except (OSError,ValueError):pass
while True:
    now=time.time();out={'timestamp':int(now),'reserve_percent':5,'estimated_remaining_seconds':None,'estimate_mode':'unavailable'}
    try:
        read=lambda name:int((base/name).read_text().strip())
        status=(base/'status').read_text().strip();soc=read('capacity');current=read('current_now');full=read('charge_full');counter=read('charge_counter');voltage=read('voltage_now')
        valid=0<=soc<=100 and full>0 and counter>=0 and counter<=full*1.1 and (pathlib.Path('/sys/module/mtk_battery/parameters/native_fg').read_text().strip() in ('Y','1'))
        out.update(status=status,capacity_valid=valid)
        if status=='Discharging' and valid and current < -10000:
            history.append((now,-current));history=[x for x in history if now-x[0]<=120]
            out['discharge_samples']=len(history)
            if len(history)>=6 and now-history[0][0]>=45:
                mean=statistics.mean(x[1] for x in history)
                previous={'last_discharge_average_uA':round(mean),'last_discharge_timestamp':int(now),'last_discharge_power_W':round(mean*voltage/1e12,3),'sample_window_seconds':int(now-history[0][0])}
                out['estimate_mode']='live_discharge'
            else:out['estimate_mode']='collecting_discharge'
        else:history=[]
        if previous and 0<=now-previous['last_discharge_timestamp']<=7*86400:
            out.update({k:previous[k] for k in ('last_discharge_average_uA','last_discharge_timestamp','last_discharge_power_W','sample_window_seconds')})
            if valid and out['estimate_mode']!='collecting_discharge':
                usable=max(0,min(counter,full*soc/100)-full*0.05)
                estimate=3600*usable/previous['last_discharge_average_uA']
                if math.isfinite(estimate):out['estimated_remaining_seconds']=int(min(estimate,30*86400))
                if status!='Discharging':out['estimate_mode']='recent_discharge_reference'
        out['error']=None
    except (OSError,ValueError,KeyError) as error:
        history=[];out['error']=type(error).__name__
    temporary=target.with_suffix('.tmp')
    temporary.write_text(json.dumps(out));temporary.chmod(0o644);os.replace(temporary,target)
    time.sleep(10)
