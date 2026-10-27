/* RAM-only native init probe; kernel also enforces an independent 60s reset. */
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
static char message[512]; static unsigned int used;
static long writer=-1;
static void text(const char *s) { while(*s&&used<500) message[used++]=*s++; }
static void number(long n) {
    char d[24]; unsigned int i=0;
    if(n<0){text("-");n=-n;}
    do {d[i++]='0'+n%10;n/=10;}while(n&&i<24);
    while(i&&used<500) message[used++]=d[--i];
}
static void step(const char *name,long value) {
    used=0; text(name); text("="); number(value); text("\n"); message[used]=0;
    call(167,15,(long)message,0,0,0); /* PR_SET_NAME: visible to kernel task trace */
    if(writer>=0) call(64,writer,(long)message,used,0,0);
}
void _start(void) {
    long ret; long delay[2]={45,0};
    step("before-proc",0);
    ret=call(40,(long)"proc",(long)"/proc",(long)"proc",0,0);
    step("proc",ret);
    writer=call(56,-100,(long)"/proc/sp1099x_diag",1,0,0);
    step("writer",writer);
    ret=call(40,(long)"sysfs",(long)"/sys",(long)"sysfs",0,0);
    step("sysfs",ret);
    ret=call(40,(long)"devtmpfs",(long)"/dev",(long)"devtmpfs",0,0);
    step("devtmpfs",ret);
    ret=call(56,-100,(long)"/dev/kmsg",1,0,0);
    step("kmsg",ret);
    {
        char *argv[]={(char *)"/bin/busybox",(char *)"sh",(char *)"/stage0-shell",0};
        char *env[]={(char *)"PATH=/bin:/sbin",(char *)"HOME=/",(char *)"ASH_STANDALONE=1",0};
        step("exec-shell",0);
        ret=call(221,(long)"/bin/busybox",(long)argv,(long)env,0,0);
        step("exec-failed",ret);
    }
    step("sleep45",0);
    call(101,(long)delay,0,0,0,0);
    step("reboot",0);
    call(142,0xfee1dead,0x28121969,0x01234567,0,0);
    for(;;) { }
}
