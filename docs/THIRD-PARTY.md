# 第三方组件、来源与版本

- fnOS ARM 1.2.0701：官方 ARM UEFI 安装介质中的用户空间。官方入口：https://fnnas.com/download-arm 。原 ISO SHA-256 为 `16775a1a5bb58ec8211665a17e48cfc9397f33af9a45d08d3364655ab8535b51`。本包使用该用户空间加板级适配，不把 UEFI 启动内核用于本平板，也不将官方闭源程序标记为本项目开源代码。相关版权/许可保留在镜像和原发行材料中。
- Linux 4.9.117：MediaTek `100227b164b750408d61e40e153ec10c49d48863` 基线及板级修改。参考 https://github.com/mediatek-android-development/android_kernel_mediatek_mt6761-62-4.9 。完整工作源码在 `source/kernel-source.tar.gz`，许可见其中 `COPYING`。
- 无线源码：Xiaomi MT6765 内核工程 `b39512fd5038ee8d6431206f34a3767f7687dd2f` 的 connectivity 代码，经 4.9 API 和本板供电适配。上游 https://github.com/xiaomi-mt6765-devs/android_kernel_xiaomi_mt6765 。相应代码包含在上述源码归档内。
- WMT Python loader：上游代码及其原许可随 `source/wmt-pyloader-source.tar.gz` / `source/WMT-LOADER-LICENSE` 提供；包内启动脚本负责本板初始化次序，不包含开发者的设备授权密钥。
- UPower 0.99.20 板载电池/UPS 适配：对应修改源码见 `source/upower-native-source.tar.bz2`，原版权/许可保留在归档中。
- Google Android SDK Platform-Tools：本机离线文件标注版本 37.0.1，ADB/Fastboot/DLL 由同一份发行目录复制；原始 NOTICE 和版本文件在 `tools/NOTICE.txt`、`tools/source.properties`。官方资料：https://developer.android.com/tools/releases/platform-tools 。工具用户目录设置依据 https://developer.android.com/tools/variables 。
- Windows 刷机前端：Go 1.24.4 编译。源码及固定依赖在 `source/flasher/`；依赖 `golang.org/x/crypto v0.36.0`、`golang.org/x/term v0.30.0`、`golang.org/x/sys v0.31.0` 的原许可证保留在 vendor 目录。它是 SSH 客户端，只连接安装记录中固定的设备主机公钥。
- BusyBox：实际二进制报告 `v1.36.1.1 topjohnwu (2024-10-06)`。提供 BusyBox 1.36.1 源码、构建配置、按二进制发布日期选取的 Android NDK kitchen 补丁源码；来源 https://busybox.net/downloads/ 和 https://github.com/topjohnwu/ndk-box-kitchen 。日期限定的源码对应关系记录在 `source/busybox-provenance.json`，未独立完成逐字节重建验证。
- Debian 工具链与系统包的版权声明保留于原镜像和组件文档。设备专用无线固件来自这一硬件批次原厂 vendor，适用其各自许可；本项目不改变它们的许可。

Windows 的驱动框架、PowerShell、NetTCPIP/NetSecurity 模块由 Windows 提供，本包没有复制一套 Windows 系统文件，也不包含第三方驱动签名绕过工具。

完整源码和构建配置用于检查适配行为及满足对应组件的源码要求。普通刷入只需要已经编译好的包，不需要构建环境。
