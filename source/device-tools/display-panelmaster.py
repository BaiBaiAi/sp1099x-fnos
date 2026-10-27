"""Use the vendor's typed PanelMaster ioctls, no arbitrary register access."""
import os,fcntl,struct,json,argparse
p=argparse.ArgumentParser()
p.add_argument('--ssc',type=int,choices=[0,1])
a=p.parse_args()
fd=os.open('/sys/kernel/debug/fbconfig',os.O_RDONLY)
def cmd(direction,n):return (direction<<30)|(4<<16)|(ord('X')<<8)|n
def get_timing(t):
    b=bytearray(struct.pack('II',t,0))
    fcntl.ioctl(fd,cmd(2,73),b,True)
    return struct.unpack('II',b)[1]
before={k:get_timing(t) for k,t in [('hsync',13),('hfront',14),('hback',15),('vsync',16),('vfront',17),('vback',18),('ssc_enabled',254)]}
if a.ssc is not None:
    # MIPI_SET_TIMING uses an8-byte MIPI_TIMING payload despite ioctl size4.
    fcntl.ioctl(fd,cmd(1,53),struct.pack('II',254,a.ssc))
after=get_timing(254)
os.close(fd)
print(json.dumps({'before':before,'ssc_after':after,'changed_only':'DSI SSC enable' if a.ssc is not None else None}))
