/* RAM-only test reset. No filesystem access or userspace mounts. */
void _start(void) {
    register unsigned long x0 __asm__("x0")=0xfee1dead;
    register unsigned long x1 __asm__("x1")=0x28121969;
    register unsigned long x2 __asm__("x2")=0x01234567;
    register unsigned long x3 __asm__("x3")=0;
    register unsigned long x8 __asm__("x8")=142;
    __asm__ volatile("svc 0":"+r"(x0):"r"(x1),"r"(x2),"r"(x3),"r"(x8):"memory");
    for(;;) { }
}
