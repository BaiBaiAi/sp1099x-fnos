"""Opt-in runtime diagnostic; default firmware sequence is preserved."""
from pathlib import Path
p=Path('/opt/sp1099x-fnos/kernel/drivers/misc/mediatek/connectivity/common/common_main/core/wmt_ic_soc.c')
s=p.read_text()
tag='static bool native_keep_efuse_vcn28;'
if tag not in s:
    marker='#include "wmt_step.h"'
    assert s.count(marker)==1
    s=s.replace(marker,marker+'\n#include <linux/moduleparam.h>\n'+tag+'\nmodule_param(native_keep_efuse_vcn28, bool, 0400);\nMODULE_PARM_DESC(native_keep_efuse_vcn28, "Diagnostic: hold VCN28 after RF calibration on MT6765");')
    marker='iRet = wmt_core_ctrl(WMT_CTRL_SOC_PALDO_CTRL, &ctrlPa1, &ctrlPa2);'
    start=s.index('/* turn off VCN28 after reading efuse */')
    end=s.index(marker,start)
    s=s[:end]+s[end:].replace(marker,'if (native_keep_efuse_vcn28 && wmt_ic_ops_soc.icId == 0x6765) {\n\t\tWMT_INFO_FUNC("NATIVE DIAGNOSTIC: hold efuse VCN28 after calibration\\n");\n\t\tiRet = 0;\n\t} else {\n\t\t'+marker+'\n\t}',1)
    p.with_suffix('.c.before-vcn28-probe').write_text(p.read_text())
    p.write_text(s)
print('Installed opt-in VCN28 diagnostic; default false')
