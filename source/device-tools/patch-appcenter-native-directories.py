"""Pinned fnOS ARM 1.2.0701 application-cache directory traversal correction.

The vendor pre-install walk flattens regular files AND directories to 0644.
Keep regular files at 0644; give only the directory owner its traversal bit.
Work on a separate executable and retain the vendor file unchanged. Reuse
duplicate return tails inside the same Go function, and update its pcsp table
so stack unwinding remains accurate at every trampoline instruction.
"""
from pathlib import Path
import struct,hashlib,json
root=Path(__file__).resolve().parent
original=(root/'trim-app-center-original').read_bytes()
assert hashlib.sha256(original).hexdigest()=='9794a320644a47e68f6811a7be0bb8075084ede2c073d1cbb5439935dc87b2f3'
d=bytearray(original);shoff=struct.unpack_from('<Q',d,40)[0];es,n,si=struct.unpack_from('<HHH',d,58)
ss=[struct.unpack_from('<IIQQQQIIQQ',d,shoff+i*es) for i in range(n)];st=ss[si];names=d[st[4]:st[4]+st[5]]
sections={bytes(names[x[0]:names.index(0,x[0])]).decode():x for x in ss}
def offset(address):
    for s in ss:
        if s[1]!=8 and s[3]<=address<s[3]+s[5]:return s[4]+address-s[3]
    raise ValueError(hex(address))
def word(address):return struct.unpack_from('<I',d,offset(address))[0]
changes=[]
def patch(address,expected,new):
    assert word(address)==expected,(hex(address),hex(word(address)),hex(expected))
    struct.pack_into('<I',d,offset(address),new);changes.append({'address':hex(address),'before':hex(expected),'after':hex(new)})
def branch(address,target):
    delta=target-address;assert delta%4==0 and -(1<<27)<=delta<(1<<27)
    return 0x14000000|((delta//4)&0x3ffffff)
def cbz(address,target,reg):return 0xb4000000|(((target-address)//4&0x7ffff)<<5)|reg
def tbz(address,target,reg,bit):return 0x36000000|((bit>>5)<<31)|((bit&31)<<19)|(((target-address)//4&0x3fff)<<5)|reg
# Prove the concrete os.fileStat Mode accessor and directory-bit accessor.
assert word(0x50d110)==0xb9401800
assert word(0x50d0e4)==0xb9401801 and word(0x50d0e8)==0x7201003f
patch(0xd397ec,0xb4000080,cbz(0xd397ec,0xd39600,0))
patch(0xd397f0,0xf85f83fd,branch(0xd397f0,0xd395f4))
patch(0xd396f0,0xd2803482,branch(0xd396f0,0xd397f4))
patch(0xd397f4,0xf84707fe,0xf9404be9) # ldr x9,[sp,#144]: FileInfo data
patch(0xd397f8,0xd65f03c0,0xb9401929) # ldr w9,[x9,#24]: FileMode
patch(0xd397fc,0xaa1f03e0,0xd2803482) # mov x2,#0644
patch(0xd39800,0xaa1f03e1,tbz(0xd39800,0xd396f4,9,31))
patch(0xd39804,0xf85f83fd,0xb27a0042) # orr x2,x2,#0100: owner traversal
patch(0xd39808,0xf84707fe,branch(0xd39808,0xd396f4))
s=sections['.gopclntab'];base=s[4]
nfunc,nfiles,text,fnames,cu,files,pctab,pcln=struct.unpack_from('<8Q',d,base+8)
selected=None;all_pcsp=[]
for i in range(nfunc):
    entry,fo=struct.unpack_from('<II',d,base+pcln+i*8)
    pcsp=struct.unpack_from('<I',d,base+pcln+fo+16)[0];all_pcsp.append(pcsp)
    if text+entry==0xd39540:selected=(base+pcln+fo,pcsp)
assert selected
func,pcsp=selected;assert all_pcsp.count(pcsp)==1,'Shared pcsp table must not be overwritten'
nameoff=struct.unpack_from('<i',d,func+4)[0];start=base+fnames+nameoff
assert d[start:d.index(0,start)].decode()=='appstore/core/app/appoperation.(*InstallOperation).SetPreInstallPermissionsAndUser.func1'
pos=base+pctab+pcsp;table_start=pos
def uvar():
    global pos
    result=shift=0
    while True:
        b=d[pos];pos+=1;result|=(b&127)<<shift
        if b<128:return result
        shift+=7
value=-1;pc=0xd39540;segments=[];first=True
while True:
    uv=uvar()
    if uv==0 and not first:break
    value+=-(uv//2)-1 if uv&1 else uv//2
    end=pc+uvar()*4;segments.append((pc,end,value));pc=end;first=False
old_length=pos-table_start
new_segments=[]
for start,end,value in segments:
    points=sorted({start,end}|{x for x in (0xd397f4,0xd3980c) if start<x<end})
    for a,b in zip(points,points[1:]):
        v=112 if 0xd397f4<=a<0xd3980c else value
        if new_segments and new_segments[-1][1]==a and new_segments[-1][2]==v:
            new_segments[-1]=(new_segments[-1][0],b,v)
        else:new_segments.append((a,b,v))
def uv_bytes(n):
    output=bytearray()
    while n>=128:output.append((n&127)|128);n>>=7
    output.append(n);return output
encoded=bytearray();value=-1
for start,end,new_value in new_segments:
    delta=new_value-value;value=new_value
    encoded+=uv_bytes(2*delta if delta>=0 else -2*delta-1)
    encoded+=uv_bytes((end-start)//4)
encoded.append(0)
assert len(encoded)<=old_length
d[table_start:table_start+old_length]=encoded+b'\0'*(old_length-len(encoded))
target=root/'trim-app-center-native';target.write_bytes(d)
manifest={'original_sha256':hashlib.sha256(original).hexdigest(),'patched_sha256':hashlib.sha256(d).hexdigest(),'instructions':changes,'pcsp_old_length':old_length,'pcsp_new_length':len(encoded),'pcsp_file_offset':table_start,'pcsp_before':segments,'pcsp_after':new_segments,'purpose':'Owner-only traversal for application-cache directories; regular files retain 0644. Vendor file unchanged.'}
(root/'appcenter-native-directory-patch.json').write_text(json.dumps(manifest,indent=2))
print(json.dumps({k:v for k,v in manifest.items() if k not in ('instructions','pcsp_before','pcsp_after')}))
