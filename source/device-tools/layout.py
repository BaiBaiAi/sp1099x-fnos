"""Strict 32GB SP1099X GPT plan; preserve all per-device identities."""
import json,re,uuid
SECTORS=60620800
ROOT_START=10862592
OLD_SIZE=49637343
ROOT_SIZE=16777216
DATA_START=ROOT_START+ROOT_SIZE
DATA_SIZE=OLD_SIZE-ROOT_SIZE

def validate(table,reference,installed=False):
    t=table['partitiontable']; parts=t['partitions'];ref=reference['partitions']
    assert t['label']=='gpt' and t['sectorsize']==512
    assert len(parts)==len(ref)+(1 if installed else 0),'Unexpected partition count'
    actual={int(re.search(r'(\d+)$',p['node']).group(1)):p for p in parts}
    for expected in ref:
        n=expected['number']; p=actual[n]
        for key in ('name','start','size','type'):
            wanted=ROOT_SIZE if installed and n==35 and key=='size' else expected[key]
            assert str(p[key]).lower()==str(wanted).lower(),f'Partition {n} {key} mismatch'
    if installed:
        assert actual[38]['start']==DATA_START and actual[38]['size']==DATA_SIZE
        assert actual[38]['name']=='fnos-data'
        assert actual[38]['type'].lower()=='0fc63daf-8483-4772-8e79-3d69d8477de4'
    return actual

def plan_dump(dump):
    lines=dump.splitlines(); found=0
    for i,line in enumerate(lines):
        if re.match(r'^/dev/mmcblk0p35\s*:',line):
            assert re.search(r'size=\s*'+str(OLD_SIZE)+r'\b',line)
            lines[i]=re.sub(r'size=\s*'+str(OLD_SIZE)+r'\b','size='+str(ROOT_SIZE),line);found+=1
    assert found==1 and not any(re.match(r'^/dev/mmcblk0p38\s*:',line) for line in lines)
    lines.append(f'/dev/mmcblk0p38 : start={DATA_START}, size={DATA_SIZE}, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, uuid={uuid.uuid4()}, name="fnos-data"')
    return '\n'.join(lines)+'\n'

if __name__=='__main__':
    import sys,subprocess,pathlib
    reference=json.loads(pathlib.Path('/usr/share/sp1099x-release/hardware.json').read_text())
    table=json.loads(subprocess.check_output(['sfdisk','--json','/dev/mmcblk0']))
    validate(table,reference)
    if '--plan' in sys.argv:
        print(plan_dump(subprocess.check_output(['sfdisk','--dump','/dev/mmcblk0'],text=True)),end='')
    else:print('All 37 stock partition entries match supported geometry')
