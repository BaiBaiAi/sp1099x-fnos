/* Freestanding diagnostic reboot helper. No libc, storage writes or formatting.
 * Raw mount/open return values survive via the kernel's RESTART2 reason log. */
static long call(long nr,long a,long b,long c,long d,long e) {
    register long x0 __asm__("x0")=a;
    register long x1 __asm__("x1")=b;
    register long x2 __asm__("x2")=c;
    register long x3 __asm__("x3")=d;
    register long x4 __asm__("x4")=e;
    register long x8 __asm__("x8")=nr;
    __asm__ volatile("svc 0":"+r"(x0):"r"(x1),"r"(x2),"r"(x3),"r"(x4),"r"(x8):"memory");
    return x0;
}
static char reason[240]; static unsigned int used;
static void text(const char *s) { while(*s && used<230) reason[used++]=*s++; }
static void number(long n) {
    char digits[24]; unsigned int i=0;
    if(n<0) { text("-"); n=-n; }
    do { digits[i++]='0'+n%10; n/=10; } while(n && i<24);
    while(i && used<230) reason[used++]=digits[--i];
}
void _start(void) {
    char data[8192]; long fd,n,writer,mproc,msys;
    mproc=call(40,(long)"proc",(long)"/proc",(long)"proc",0,0);
    msys=call(40,(long)"sysfs",(long)"/sys",(long)"sysfs",0,0);
    fd=call(56,-100,(long)"/run/stage0.log",0,0,0);
    writer=call(56,-100,(long)"/proc/sp1099x_diag",1,0,0);
    text("SP1099X probe mountproc="); number(mproc);
    text(" mountsys="); number(msys); text(" logfd="); number(fd);
    text(" writerfd="); number(writer);
    if(writer>=0) call(64,writer,(long)reason,used,0,0);
    if(fd>=0) {
        n=call(63,fd,(long)data,sizeof(data),0,0);
        if(n>0 && writer>=0) call(64,writer,(long)data,n,0,0);
        if(n>0) {
            long i; text(" log=");
            for(i=0;i<n && used<230;i++) reason[used++]=(data[i]=='\n'||data[i]=='\r')?' ':data[i];
        }
    }
    reason[used]=0;
    call(142,0xfee1dead,0x28121969,0xa1b2c3d4,(long)reason,0);
    call(142,0xfee1dead,0x28121969,0x01234567,0,0);
    call(93,1,0,0,0,0);
    for(;;) { }
}
