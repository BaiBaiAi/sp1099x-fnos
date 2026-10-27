"""Board-specific display adapter, based on stock DTBO and disassembly.

Run under WSL. Does not touch the device or change out-native configuration.
"""
from pathlib import Path
import shutil

base=Path('/opt/sp1099x-fnos')
src=base/'kernel/drivers/misc/mediatek/lcm'
backup=base/'display-original'
backup.mkdir(exist_ok=True)
for rel in ['mt65xx_lcm_list.c','lcm_gpio.c']:
    p=src/rel
    if not (backup/rel).exists(): shutil.copy2(p,backup/rel)

p=src/'mt65xx_lcm_list.c'
s=(backup/p.name).read_text()
s=s.replace('#include <lcm_drv.h>', '#include <lcm_drv.h>\n#if defined(S801_BSNFHD_70PIN_DSI_VDO)\nextern struct LCM_DRIVER lcm_common_drv;\n#endif')
s=s.replace('struct LCM_DRIVER *lcm_driver_list[] = {', '''struct LCM_DRIVER *lcm_driver_list[] = {
#if defined(S801_BSNFHD_70PIN_DSI_VDO)
    &lcm_common_drv,
#endif''')
s=s.replace('unsigned char lcm_name_list[][128] = {','''unsigned char lcm_name_list[][128] = {
#if defined(S801_BSNFHD_70PIN_DSI_VDO)
    "s801_bsnfhd_70pin_dsi_vdo",
#endif''')
p.write_text(s)

p=src/'lcm_common.c'
bak=backup/'lcm_common.c'
if not bak.exists(): shutil.copy2(p,bak)
s=bak.read_text()
anchor='\t\tmemcpy(params, &(_LCM_DTS.params), sizeof(struct LCM_PARAMS));'
assert s.count(anchor)==1
s=s.replace(anchor,anchor+'''
#if defined(S801_BSNFHD_70PIN_DSI_VDO)
        /* On this panel the typed runtime SSC-off test removed vertical
         * shaking. Retain nominal PLL480MHz and all stock porch timings. */
        params->dsi.ssc_disable = 1;
#endif
''')
p.write_text(s)

p=base/'kernel/drivers/misc/mediatek/video/mt6765/dispsys/ddp_dsi.c'
bak=backup/'ddp_dsi.c'
if not bak.exists(): shutil.copy2(p,bak)
s=bak.read_text()
assert s.count('static const struct LCM_UTIL_FUNCS lcm_utils_')==3
# These tables are populated by ddp_dsi_set_lcm_utils. They are mutable.
# Early Android probe hid this bug before mark_rodata_ro; late probe faults.
s=s.replace('static const struct LCM_UTIL_FUNCS lcm_utils_', 'static struct LCM_UTIL_FUNCS lcm_utils_')
p.write_text(s)

p=base/'kernel/drivers/misc/mediatek/video/mt6765/videox/disp_assert_layer.c'
bak=backup/'disp_assert_layer.c'
if not bak.exists(): shutil.copy2(p,bak)
p.write_text(bak.read_text().replace('NOT_REFERENCED(', '(void)('))
panel=src/'s801_bsnfhd_70pin_dsi_vdo'
panel.mkdir(exist_ok=True)
(panel/'Makefile').write_text('obj-y += board.o\n')
(panel/'board.c').write_text('/* Panel uses stock DT parameters via lcm_common_drv. */\n')

p=src/'lcm_gpio.c'
s=(backup/p.name).read_text()
anchor='\tif (_lcm_gpio_check_data(type, t1) == LCM_STATUS_OK) {'
assert s.count(anchor)==1
custom='''
#if defined(S801_BSNFHD_70PIN_DSI_VDO)
    /* Stock lcm_gpio_set_data at ffffff80086a529c:
     * types1..4 validate and return; only type5 drives a pinctrl/bias state.
     * dts_to_pin[] is {0,0,0,0,3,1,2,3}; 1=bias on,2=bias off,3=no-op.
     * Do not interpret obsolete type4 values as real GPIO pin writes.
     */
    if (type >= 1 && type <= 4)
        return LCM_STATUS_OK;
    if (type == 5 && t1->data < MAX_LCM_GPIO_MODE) {
        unsigned int mode = t1->data;
        int ret;
        if (mode == 5) ret = display_bias_enable();
        else if (mode == 6) ret = display_bias_disable();
        else if (mode == 4 || mode == 7) ret = 0;
        else if (IS_ERR_OR_NULL(_lcm_gpio) ||
                 IS_ERR_OR_NULL(_lcm_gpio_mode[mode])) ret = -ENODEV;
        else ret = pinctrl_select_state(_lcm_gpio, _lcm_gpio_mode[mode]);
        pr_info("[SP1099X LCM] stock state %u result %d\\n", mode, ret);
        return ret ? LCM_STATUS_ERROR : LCM_STATUS_OK;
    }
    return LCM_STATUS_ERROR;
#endif
'''
s=s.replace('#include "lcm_gpio.h"','#include "lcm_gpio.h"\n#if defined(S801_BSNFHD_70PIN_DSI_VDO)\n#include "lcm_pmic.h"\n#endif')
s=s.replace(anchor,custom+anchor)
s=s.replace('#include <mt-plat/mt_gpio.h>', '#if !defined(S801_BSNFHD_70PIN_DSI_VDO)\n#include <mt-plat/mt_gpio.h>\n#endif')
start=s.index('\tGPIO_LCD_PWR_EN =\n')
end=s.index('\n\tpr_debug("[LCM][GPIO] _lcm_gpio_get_info end!',start)
s=s[:start]+'#if !defined(S801_BSNFHD_70PIN_DSI_VDO)\n'+s[start:end]+'\n#endif\n'+s[end:]
p.write_text(s)

p=src/'inc/lcm_common.h'
bak=backup/'lcm_common.h'
if not bak.exists(): shutil.copy2(p,bak)
s=bak.read_text().replace('lcm_common_get_params(LCM_PARAMS *params)', 'lcm_common_get_params(struct LCM_PARAMS *params)')
p.write_text(s)
print('Stock DT panel name and scoped GPIO operation compatibility installed in source')

# First test must let networking start before probing the display pipeline.
# A root-only parameter and explicit platform bind trigger the probe later.
p=base/'kernel/drivers/misc/mediatek/video/mt6765/videox/mtkfb.c'
bak=backup/'mtkfb.c'
if not bak.exists(): shutil.copy2(p,bak)
s=bak.read_text()
anchor='static int mtkfb_probe(struct platform_device *pdev)'
assert s.count(anchor)==1
s=s.replace(anchor, '''#if defined(CONFIG_MTK_LCM_DEVICE_TREE_SUPPORT)
static bool native_probe_enable;
module_param(native_probe_enable, bool, 0600);
extern void __iomem *smi_mmsys_base_addr_get(void);
#endif
''' + anchor)
anchor='\tlong dts_gpio_state = 0;'
assert s.count(anchor)==1
s=s.replace(anchor,anchor+'''
#if defined(CONFIG_MTK_LCM_DEVICE_TREE_SUPPORT)
    if (!native_probe_enable) {
        pr_info("[SP1099X LCM] probe held for explicit native display test\\n");
        return -ENODEV;
    }
#endif
''')
old='''\tif (!smi_mm_clk_first_get()) {
\t\tDISPMSG("SMI not start probe\\n");
\t\treturn -EPROBE_DEFER;
\t}'''
assert s.count(old)==1
s=s.replace(old,'''\tif (!smi_mm_clk_first_get()) {
        /* The boot-only SMI first-clock flag is cleared at late_initcall.
         * Explicit native probe happens later: acquire real LARB0/common
         * clocks through SMI rather than pretending the boot flag is true.
         * This trial retains one reference until reboot.
         */
        if (!smi_mmsys_base_addr_get() || !smi_base_addr_get(0))
            return -EPROBE_DEFER;
        r = smi_bus_prepare_enable(0, "DISP_NATIVE", true);
        if (r) return r;
        pr_info("[SP1099X LCM] acquired SMI clocks for late probe\\n");
    }''')
s=s.replace('primary_display_init(mtkfb_find_lcm_driver(), lcd_fps, is_lcm_inited);',
'''/* The native late probe must reinitialize the real panel after clocks
     * were released at boot, rather than relying on LK handover state. */
    primary_display_init(mtkfb_find_lcm_driver(), lcd_fps, 0);''')
# FB_VISUAL_TRUECOLOR uses a16-entry pseudo_palette, independent of bpp.
# Original 1 << bpp shifts a32-bit value by32, rejecting console colors
# and also failing to bound16-bit writes to the actual palette allocation.
assert s.count('m = 1 << bpp;')==1
s=s.replace('m = 1 << bpp;', 'm = 16; /* truecolor pseudo_palette entries */')
old='''(0xff000000) |
\t\t    ((red & 0xFF00) << 8) | ((green & 0xFF00)) |
\t\t    ((blue & 0xFF00) >> 8);'''
assert s.count(old)==1
s=s.replace(old,'''(0xff000000) |
            ((red >> 8) << info->var.red.offset) |
            ((green >> 8) << info->var.green.offset) |
            ((blue >> 8) << info->var.blue.offset);''')
p.write_text(s)

p=base/'kernel/drivers/misc/mediatek/video/mt6765/videox/disp_lowpower.c'
bak=backup/'disp_lowpower.c'
if not bak.exists(): shutil.copy2(p,bak)
s=bak.read_text()
assert s.count('static atomic_t idlemgr_task_wakeup = ATOMIC_INIT(1);')==1
s=s.replace('static atomic_t idlemgr_task_wakeup = ATOMIC_INIT(1);',
            'static atomic_t idlemgr_task_wakeup = ATOMIC_INIT(0);')
p.write_text(s)
