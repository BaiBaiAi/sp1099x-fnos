# 对应源码与重建说明

本目录供开发者审阅和重建。普通刷入使用已编译的 EXE 和 payload，不需要这些构建工具，也不依赖制作电脑的路径。

- `kernel-source.tar.gz`：板级内核及移入 connectivity 驱动的完整工作源码；不含编译输出目录或任何一台平板的 NVRAM。
- `kernel-display.config`：运行中 #19 内核对应配置；`wireless-modules.config` 是单独无线模块构建配置。
- `device-tools/`：实际分发的安装/首次启动/权限兼容/屏幕/电池等脚本或源文件；其中 `/dev/mmcblk0p35` 等是严格限定硬件的目标设备路径，不是电脑路径。
- `wmt-pyloader-source.tar.gz`：实际打入系统的 WMT Python loader 源码；许可在相邻 LICENSE 文件。
- `upower-native-source.tar.bz2`：板载电池/UPS 识别适配的 UPower 源码。
- `flasher/`：Windows 前端源码、Go 模块锁定文件、vendor 依赖和合成 GPT 测试夹具；测试夹具没有使用开发机的分区 UUID。

## Windows 工具

开发者安装 Go 1.24.4 或经兼容性复核的版本后，在 Windows PowerShell 中进入 `source\flasher`：

```powershell
go test -mod=vendor ./...
go build -mod=vendor -trimpath -ldflags "-s -w" -o ..\..\SP1099X-Flasher.exe .
```

更改 EXE、脚本或固件后必须重新生成 `SHA256.json`，不能继续使用旧的校验文件。前端与镜像通过版本化的 session.json 协作，不能只替换一边而不复核协议。

## 内核、根文件系统与启动镜像

内核构建需要 Linux、支持 ARM64 的 GCC/binutils 和本项目使用的 Android Clang 4691093 工具链。将源码和配置放在自己选择的英文构建目录，使用独立 O= 输出目录，避免污染另一个已验证配置。`kernel-display.config` 与 `wireless-modules.config` 不能混作一个输出目录。

启动镜像采用原硬件的 Android boot 格式、原 DTB 和自编译内核。可以用上游 Magisk 的 magiskboot 解包本包 `payload/boot.img` 检查头、内核、DTB 和 ramdisk，再替换自己的构建产物；不能改用 UEFI 镜像、其他板卡 DTB，或照搬不同 MTK 平台驱动。

完整板级源码中的修改、配置和实际分发的脚本是复核依据。此目录不声称“一条命令逐字节重建全部官方闭源用户空间”，也没有附带庞大的交叉编译工具链。

`payload/rootfs.squashfs` 本身是未创建管理员的完整根文件系统，可在 Linux 用 unsquashfs 解包后审阅。修改其 Linux 设备脚本或已编译的兼容库后，要保留原文件的 UID/GID、ACL、xattr、符号链接和可执行权限；重新用 mksquashfs 制包并更新全包哈希。不要以运行中的 NAS 根目录覆盖它，否则会带入账号、密钥和用户数据。

r1 的安装器和回退器尚无第二台设备的完整实刷记录，源码审阅、单元测试和隔离测试不能替代该验收。相关限制见 `docs/VALIDATION-ZH.md`。
