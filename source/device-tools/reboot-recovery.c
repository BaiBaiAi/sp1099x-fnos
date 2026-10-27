/* Switch the MTK RTC reboot reason to recovery without Android userspace. */
void _start(void) {
    static const char mode[]="recovery";
    register unsigned long x0 __asm__("x0")=0xfee1dead;
    register unsigned long x1 __asm__("x1")=0x28121969;
    register unsigned long x2 __asm__("x2")=0xa1b2c3d4;
    register unsigned long x3 __asm__("x3")=(unsigned long)mode;
    register unsigned long x8 __asm__("x8")=142;
    __asm__ volatile("svc 0":"+r"(x0):"r"(x1),"r"(x2),"r"(x3),"r"(x8):"memory");
    for(;;) { }
}
