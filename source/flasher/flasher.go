package main

// Standalone Windows frontend. All firmware tools are resolved relative to
// this executable. No Python, WSL, global ADB, shell interpolation or online
// downloads are used by the delivered program.
import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

type Part struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	Start  uint64 `json:"start"`
	Size   uint64 `json:"size"`
	Type   string `json:"type"`
}
type Hardware struct {
	Sectors  uint64 `json:"sectors"`
	Parts    []Part `json:"partitions"`
	DTBO     string `json:"dtbo_sha256"`
	Recovery string `json:"recovery_sha256"`
}
type Backup struct {
	Format       int               `json:"format"`
	Serial       string            `json:"serial"`
	CID          string            `json:"cid"`
	Files        map[string]string `json:"files"`
	OriginalBoot string            `json:"original_boot_sha256"`
	DTBO         string            `json:"dtbo_sha256"`
}
type Session struct {
	Backup
	Mode      string `json:"mode"`
	Confirmed bool   `json:"confirmed_erase"`
}

var root, serial string
var hw Hardware
var manifest map[string]string

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func asciiPath(path string) error {
	for _, r := range path {
		if r < 32 || r > 126 {
			return fmt.Errorf("不允许中文或非 ASCII 路径 / Non-ASCII path rejected: %s", path)
		}
	}
	return nil
}
func digest(p string) string {
	f, e := os.Open(p)
	must(e)
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	must(e)
	return hex.EncodeToString(h.Sum(nil))
}
func writeJSON(p string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	must(e)
	must(os.WriteFile(p, b, 0600))
}
func readJSON(p string, v any) { b, e := os.ReadFile(p); must(e); must(json.Unmarshal(b, v)) }
func tool(name string, args ...string) *exec.Cmd {
	if name == "adb" {
		args = append([]string{"-P", "5038"}, args...)
	}
	c := exec.Command(filepath.Join(root, "tools", name+".exe"), args...)
	c.Dir = root
	return c
}
func adb(args ...string) *exec.Cmd {
	a := []string{}
	if serial != "" {
		a = append(a, "-s", serial)
	}
	return tool("adb", append(a, args...)...)
}
func output(c *exec.Cmd) string {
	b, e := c.CombinedOutput()
	if e != nil {
		panic(fmt.Errorf("%s: %w: %s", filepath.Base(c.Path), e, b))
	}
	return strings.TrimSpace(string(b))
}
func quoteSH(s string) string     { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func shell(command string) string { return output(adb("shell", "su -c "+quoteSH(command))) }
func capture(p string, command string) {
	f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	must(e)
	c := adb("exec-out", "su -c "+quoteSH(command))
	c.Stdout = f
	var stderr bytes.Buffer
	c.Stderr = &stderr
	e = c.Run()
	closeErr := f.Close()
	if e != nil {
		panic(fmt.Errorf("backup failed: %w: %s", e, stderr.String()))
	}
	must(closeErr)
}
func oneADB() string {
	rows := strings.Split(output(tool("adb", "devices")), "\n")
	var found []string
	for _, line := range rows {
		v := strings.Fields(line)
		if len(v) == 2 && v[1] == "device" {
			found = append(found, v[0])
		}
	}
	if len(found) != 1 {
		panic("请只连接一台已授权 USB 调试的 Android；检测到设备数: " + strconv.Itoa(len(found)))
	}
	return found[0]
}
func safeSerial(s string) bool { return regexp.MustCompile(`^[A-Za-z0-9._:-]{1,80}$`).MatchString(s) }
func verifyPackage() {
	readJSON(filepath.Join(root, "SHA256.json"), &manifest)
	for name, wanted := range manifest {
		p := filepath.Clean(filepath.Join(root, filepath.FromSlash(name)))
		rel, e := filepath.Rel(root, p)
		must(e)
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			panic("Invalid manifest path")
		}
		if digest(p) != wanted {
			panic("文件损坏 / SHA256 mismatch: " + name)
		}
	}
	readJSON(filepath.Join(root, "payload", "hardware.json"), &hw)
}
func guid(b []byte) string {
	return fmt.Sprintf("%08X-%04X-%04X-%02X%02X-%X", binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint16(b[4:]), binary.LittleEndian.Uint16(b[6:]), b[8], b[9], b[10:16])
}
func gptParts(data []byte, reference Hardware) error {
	if len(data) < 1048576 || string(data[512:520]) != "EFI PART" {
		return errors.New("Invalid primary GPT")
	}
	hdr := append([]byte{}, data[512:1024]...)
	size := binary.LittleEndian.Uint32(hdr[12:])
	if size < 92 || size > 512 {
		return errors.New("Invalid GPT header size")
	}
	want := binary.LittleEndian.Uint32(hdr[16:])
	binary.LittleEndian.PutUint32(hdr[16:], 0)
	if crc32.ChecksumIEEE(hdr[:size]) != want {
		return errors.New("GPT header CRC mismatch")
	}
	lba := binary.LittleEndian.Uint64(hdr[72:])
	count := binary.LittleEndian.Uint32(hdr[80:])
	stride := binary.LittleEndian.Uint32(hdr[84:])
	if stride != 128 || count < 38 || lba*512+uint64(count)*128 > uint64(len(data)) {
		return errors.New("Unsupported GPT entry table")
	}
	entries := data[lba*512 : lba*512+uint64(count)*128]
	if crc32.ChecksumIEEE(entries) != binary.LittleEndian.Uint32(hdr[88:]) {
		return errors.New("GPT entry CRC mismatch")
	}
	active := 0
	for i := uint32(0); i < count; i++ {
		p := entries[i*128 : (i+1)*128]
		if bytes.Equal(p[:16], make([]byte, 16)) {
			continue
		}
		active++
		if int(i) >= len(reference.Parts) {
			return errors.New("Unexpected extra partition")
		}
		r := reference.Parts[i]
		var u []uint16
		for j := 56; j < 128; j += 2 {
			v := binary.LittleEndian.Uint16(p[j:])
			if v == 0 {
				break
			}
			u = append(u, v)
		}
		name := string(utf16.Decode(u))
		start := binary.LittleEndian.Uint64(p[32:])
		end := binary.LittleEndian.Uint64(p[40:])
		if r.Number != int(i)+1 || name != r.Name || start != r.Start || end-start+1 != r.Size || guid(p[:16]) != strings.ToUpper(r.Type) {
			return fmt.Errorf("Partition %d does not match supported device", i+1)
		}
	}
	if active != 37 {
		return fmt.Errorf("Expected 37 stock partitions; got %d", active)
	}
	return nil
}
func preflight() (string, string) {
	protectBackup(filepath.Join(root, "work", "android"))
	fmt.Println("Starting bundled ADB on isolated port 5038; releasing old ADB server USB handles.")
	old := exec.Command(filepath.Join(root, "tools", "adb.exe"), "kill-server")
	old.Dir = root
	_ = old.Run()
	serial = oneADB()
	if !safeSerial(serial) {
		panic("Unsupported device serial")
	}
	if !strings.HasPrefix(output(adb("get-devpath")), "usb:") {
		panic("仅允许 USB 数据线设备，不接受 TCP/IP ADB 刷机")
	}
	if !strings.Contains(shell("id"), "uid=0(") {
		panic("Magisk/root authorization is required")
	}
	model := strings.ToLower(shell("getprop ro.product.model"))
	platform := strings.ToLower(shell("getprop ro.board.platform"))
	if model != "sp1099x" || platform != "mt6765" {
		panic("仅支持 SP1099X / mt6765；实际: " + model + " / " + platform)
	}
	if shell("getprop ro.boot.flash.locked") != "0" {
		panic("Bootloader must already be unlocked; root alone is insufficient")
	}
	if shell("getprop ro.build.version.sdk") != "28" {
		panic("本候选包只验证了 Android 9 (SDK28) 的出厂布局")
	}
	mem := regexp.MustCompile(`MemTotal:\s+(\d+)`).FindStringSubmatch(shell("cat /proc/meminfo"))
	if len(mem) != 2 {
		panic("无法读取 RAM 容量")
	}
	memKB, e := strconv.Atoi(mem[1])
	must(e)
	if memKB < 3500000 || memKB > 4100000 {
		panic("只支持已验证的 4GB RAM 批次")
	}
	bat := regexp.MustCompile(`level:\s*(\d+)`).FindStringSubmatch(shell("dumpsys battery"))
	if len(bat) != 2 {
		panic("无法确认电量；请先确认 Android 电池服务正常")
	}
	level, e := strconv.Atoi(bat[1])
	must(e)
	if level < 60 {
		panic("电量低于60%，请充电后再运行")
	}
	if shell("cat /sys/class/block/mmcblk0/size") != strconv.FormatUint(hw.Sectors, 10) {
		panic("Unsupported eMMC capacity")
	}
	cid := strings.ToLower(shell("cat /sys/class/block/mmcblk0/device/cid"))
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(cid) {
		panic("Invalid eMMC CID")
	}
	dtbo := strings.Fields(shell("sha256sum /dev/block/mmcblk0p27"))[0]
	if dtbo != hw.DTBO {
		panic("设备树/屏幕批次不匹配，拒绝刷入")
	}
	recovery := strings.Fields(shell("sha256sum /dev/block/mmcblk0p2"))[0]
	if recovery != hw.Recovery {
		panic("原厂 Recovery 不匹配；请先恢复该设备原厂 Recovery")
	}
	primary, e := adb("exec-out", "su -c "+quoteSH("dd if=/dev/block/mmcblk0 bs=512 count=2048 2>/dev/null")).Output()
	must(e)
	must(gptParts(primary, hw))
	fmt.Println("Verified model, root, unlocked bootloader, eMMC, DTBO, Recovery. CID:", cid)
	return serial, cid
}
func backupAll(dir, cid string) Backup {
	if _, err := os.Stat(dir); err == nil {
		panic("备份输出目录已经存在；请选择新的空路径，避免覆盖旧备份或改变已有目录权限")
	}
	must(os.MkdirAll(dir, 0700))
	protectBackup(dir)
	b := Backup{Format: 1, Serial: serial, CID: cid, Files: map[string]string{}}
	capture(filepath.Join(dir, "gpt-primary.bin"), "dd if=/dev/block/mmcblk0 bs=512 count=2048 2>/dev/null")
	capture(filepath.Join(dir, "gpt-secondary.bin"), "dd if=/dev/block/mmcblk0 bs=512 skip=60618752 count=2048 2>/dev/null")
	primary, e := os.ReadFile(filepath.Join(dir, "gpt-primary.bin"))
	must(e)
	must(gptParts(primary, hw))
	for _, p := range hw.Parts {
		if p.Number == 34 || p.Number == 35 {
			continue
		}
		name := fmt.Sprintf("p%02d-%s.img", p.Number, p.Name)
		fmt.Printf("Backing up %s (%d MiB)...\n", name, p.Size*512/1048576)
		path := filepath.Join(dir, name)
		capture(path, fmt.Sprintf("dd if=/dev/block/mmcblk0p%d bs=1048576 2>/dev/null", p.Number))
		st, e := os.Stat(path)
		must(e)
		if uint64(st.Size()) != p.Size*512 {
			panic("Short partition backup: " + name)
		}
		hash := digest(path)
		remote := strings.Fields(shell(fmt.Sprintf("sha256sum /dev/block/mmcblk0p%d", p.Number)))[0]
		if remote != hash {
			panic("Backup checksum mismatch: " + name)
		}
		b.Files[name] = hash
		if p.Number == 25 {
			b.OriginalBoot = hash
		}
		if p.Number == 27 {
			b.DTBO = hash
		}
	}
	for _, name := range []string{"mmcblk0boot0", "mmcblk0boot1"} {
		path := filepath.Join(dir, name+".img")
		capture(path, "dd if=/dev/block/"+name+" bs=1048576 2>/dev/null")
		b.Files[name+".img"] = digest(path)
		if strings.Fields(shell("sha256sum /dev/block/" + name))[0] != b.Files[name+".img"] {
			panic("Boot-area backup mismatch")
		}
	}
	for _, name := range []string{"gpt-primary.bin", "gpt-secondary.bin"} {
		b.Files[name] = digest(filepath.Join(dir, name))
	}
	writeJSON(filepath.Join(dir, "backup.json"), b)
	return b
}
func keyPair(dir string) {
	k, e := rsa.GenerateKey(rand.Reader, 3072)
	must(e)
	must(os.WriteFile(filepath.Join(dir, "maintenance-key.pem"), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}), 0600))
	field := func(b []byte) []byte {
		v := make([]byte, 4)
		binary.BigEndian.PutUint32(v, uint32(len(b)))
		return append(v, b...)
	}
	mp := func(n *big.Int) []byte {
		b := n.Bytes()
		if len(b) > 0 && b[0]&128 != 0 {
			b = append([]byte{0}, b...)
		}
		return field(b)
	}
	raw := field([]byte("ssh-rsa"))
	raw = append(raw, mp(big.NewInt(int64(k.E)))...)
	raw = append(raw, mp(k.N)...)
	pub := "ssh-rsa " + base64.StdEncoding.EncodeToString(raw) + " sp1099x-owner\n"
	must(os.WriteFile(filepath.Join(dir, "authorized_keys"), []byte(pub), 0600))
}
func powershell(s string) (string, error) {
	units := utf16.Encode([]rune(s))
	raw := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(raw[i*2:], v)
	}
	path := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	c := exec.Command(path, "-NoProfile", "-NonInteractive", "-OutputFormat", "Text", "-EncodedCommand", base64.StdEncoding.EncodeToString(raw))
	// Do not inherit PowerShell 7/module paths from the calling computer.
	// Windows inbox NetTCPIP/NetSecurity and ACL modules belong to WinPS 5.1.
	for _, v := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(v), "PSMODULEPATH=") {
			c.Env = append(c.Env, v)
		}
	}
	c.Env = append(c.Env, "PSModulePath="+filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "Modules"))
	b, e := c.CombinedOutput()
	return strings.TrimSpace(string(b)), e
}
func requireAdmin() {
	v, e := powershell(`if (([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { 'yes' } else { 'no' }`)
	if e != nil || v != "yes" {
		panic("请右键本程序，以管理员身份运行；只用于配置 USB 专用网络和临时防火墙规则")
	}
}
func confirm(serial, action string) {
	fmt.Printf("\n%s 将清空 Android userdata / 当前 NAS 数据。\n输入 ERASE-%s 才继续: ", action, serial)
	s := bufio.NewScanner(os.Stdin)
	if !s.Scan() || strings.TrimSpace(s.Text()) != "ERASE-"+serial {
		panic("Cancelled; no firmware write performed")
	}
}
func newSession(dir string, b Backup, mode string) (Session, map[string]string) {
	paths := map[string]string{"session.json": filepath.Join(dir, "session.json"), "rootfs.sha256": filepath.Join(dir, "rootfs.sha256"), "rootfs.squashfs": filepath.Join(root, "payload", "rootfs.squashfs"), "boot.img": filepath.Join(root, "payload", "boot.img"), "recovery.img": filepath.Join(dir, "p02-recovery.img"), "original-boot.img": filepath.Join(dir, "p25-boot.img"), "para.img": filepath.Join(dir, "p03-para.img"), "gpt-primary.bin": filepath.Join(dir, "gpt-primary.bin"), "gpt-secondary.bin": filepath.Join(dir, "gpt-secondary.bin"), "authorized_keys": filepath.Join(dir, "authorized_keys")}
	s := Session{Backup: Backup{Format: 1, Serial: b.Serial, CID: b.CID, OriginalBoot: b.OriginalBoot, DTBO: b.DTBO, Files: map[string]string{}}, Mode: mode, Confirmed: true}
	for name, p := range paths {
		if name == "session.json" || name == "rootfs.sha256" {
			continue
		}
		s.Files[name] = digest(p)
	}
	writeJSON(paths["session.json"], s)
	must(os.WriteFile(paths["rootfs.sha256"], []byte(s.Files["rootfs.squashfs"]+"  /run/rootfs.squashfs\n"), 0600))
	return s, paths
}
func transferAndWait(dir string, paths map[string]string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip != "192.168.77.1" {
			http.Error(w, "USB client only", 403)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		p, ok := paths[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, p)
	})
	listener, e := net.Listen("tcp4", "0.0.0.0:18777")
	must(e)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go server.Serve(listener)
	defer server.Close()
	exe, _ := os.Executable()
	escaped := strings.ReplaceAll(exe, "'", "''")
	ps := `$ErrorActionPreference='Stop'; Get-NetFirewallRule -Name 'SP1099X-Firmware-Transfer' -ErrorAction SilentlyContinue | Remove-NetFirewallRule; New-NetFirewallRule -Name 'SP1099X-Firmware-Transfer' -DisplayName 'SP1099X temporary USB installer' -Direction Inbound -Action Allow -Protocol TCP -LocalPort 18777 -LocalAddress 192.168.77.2 -RemoteAddress 192.168.77.1 -Program '` + escaped + `' -Profile Any | Out-Null`
	_, e = powershell(ps)
	must(e)
	defer powershell(`Get-NetFirewallRule -Name 'SP1099X-Firmware-Transfer' -ErrorAction SilentlyContinue | Remove-NetFirewallRule`)
	added := false
	restoreDHCP := false
	defer func() {
		if added {
			ps := `Get-NetAdapter -IncludeHidden | Where-Object {$_.MacAddress -eq '02-53-50-10-99-02'} | ForEach-Object { Get-NetIPAddress -InterfaceIndex $_.ifIndex -IPAddress 192.168.77.2 -ErrorAction SilentlyContinue | Remove-NetIPAddress -Confirm:$false`
			if restoreDHCP {
				ps += `; Set-NetIPInterface -InterfaceIndex $_.ifIndex -AddressFamily IPv4 -Dhcp Enabled`
			}
			ps += ` }`
			powershell(ps)
		}
	}()
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP("192.168.77.2")}}).DialContext}
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second}
	defer transport.CloseIdleConnections()
	fmt.Println("等待 USB RNDIS。若 Windows 未自动绑定驱动，请按文档选择 Microsoft 远程 NDIS 兼容设备。")
	deadline := time.Now().Add(45 * time.Minute)
	last := ""
	lastNet := time.Time{}
	for time.Now().Before(deadline) {
		if time.Since(lastNet) > 8*time.Second {
			v, _ := powershell(`$a=@(Get-NetAdapter -IncludeHidden | Where-Object {$_.MacAddress -eq '02-53-50-10-99-02' -and $_.Status -eq 'Up'}); if ($a.Count -eq 1) { if (-not (Get-NetIPAddress -InterfaceIndex $a[0].ifIndex -IPAddress 192.168.77.2 -ErrorAction SilentlyContinue)) { $was=(Get-NetIPInterface -InterfaceIndex $a[0].ifIndex -AddressFamily IPv4).Dhcp; New-NetIPAddress -InterfaceIndex $a[0].ifIndex -IPAddress 192.168.77.2 -PrefixLength 24 -AddressFamily IPv4 | Out-Null; 'added:'+ $was } }`)
			if strings.Contains(v, "added") {
				added = true
				restoreDHCP = strings.Contains(v, "Enabled")
			}
			lastNet = time.Now()
		}
		response, err := client.Get("http://192.168.77.1:8080/cgi-bin/status")
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 65536))
		response.Body.Close()
		if response.StatusCode != 200 {
			time.Sleep(2 * time.Second)
			continue
		}
		state := strings.TrimSpace(string(raw))
		if state != last {
			fmt.Println("安装阶段:", state)
			last = state
		}
		if state == "complete" || state == "restored" || state == "failed" {
			if log, e := client.Get("http://192.168.77.1:8080/cgi-bin/log"); e == nil {
				f, e := os.Create(filepath.Join(dir, "install.log"))
				if e == nil {
					io.Copy(f, io.LimitReader(log.Body, 16*1024*1024))
					f.Close()
				}
				log.Body.Close()
			}
			if state == "failed" {
				panic("安装环境报告失败；请保留数据线和备份，查看 install.log；不要重复格式化")
			}
			fmt.Println("完成。日志和本机专用维护密钥保存在:", dir)
			if state == "restored" {
				fmt.Println("接下来进入原厂 Recovery 执行 Wipe data/factory reset；旧 Android 数据无法恢复。")
			} else {
				fmt.Println("设备即将重启。首次进入 http://192.168.77.1:5666 创建你自己的管理员。")
			}
			return
		}
		time.Sleep(2 * time.Second)
	}
	panic("等待超时；保留备份与日志，按文档处理，勿盲目重复刷入")
}
func install(dir string) {
	requireAdmin()
	_, cid := preflight()
	confirm(serial, "安装 fnOS")
	b := backupAll(dir, cid)
	keyPair(dir)
	_, paths := newSession(dir, b, "install")
	fmt.Println("全部备份已逐分区比对 SHA256；现在写临时安装 Recovery。")
	output(adb("push", filepath.Join(root, "payload", "installer-recovery.img"), "/data/local/tmp/sp1099x-installer.img"))
	expected := digest(filepath.Join(root, "payload", "installer-recovery.img"))
	if strings.Fields(shell("sha256sum /data/local/tmp/sp1099x-installer.img"))[0] != expected {
		panic("Staged installer checksum mismatch")
	}
	shell("dd if=/data/local/tmp/sp1099x-installer.img of=/dev/block/mmcblk0p2 bs=1048576 && sync")
	if strings.Fields(shell("sha256sum /dev/block/mmcblk0p2"))[0] != expected {
		panic("Recovery readback mismatch; restore its verified backup before reboot")
	}
	output(adb("reboot", "recovery"))
	transferAndWait(dir, paths)
}
func restore(dir string) {
	requireAdmin()
	var b Backup
	readJSON(filepath.Join(dir, "backup.json"), &b)
	if !safeSerial(b.Serial) || b.Format != 1 {
		panic("Invalid backup metadata")
	}
	for name, sha := range b.Files {
		if filepath.Base(name) != name {
			panic("Invalid backup path")
		}
		if digest(filepath.Join(dir, name)) != sha {
			panic("Backup damaged: " + name)
		}
	}
	confirm(b.Serial, "恢复 Android 分区布局与原启动镜像")
	if _, e := os.Stat(filepath.Join(dir, "authorized_keys")); os.IsNotExist(e) {
		keyPair(dir)
	}
	_, paths := newSession(dir, b, "restore")
	rows := strings.Fields(output(tool("fastboot", "devices")))
	if len(rows) != 2 {
		panic("请先按文档进入 Bootloader/Fastboot，并只连接这一台目标设备")
	}
	fbserial := rows[0]
	if fbserial != b.Serial {
		panic("Fastboot 序列号与备份不一致，拒绝写入。请勿使用别人的备份。")
	}
	fmt.Println("Fastboot serial:", fbserial, "；RAM 安装器还会复核 eMMC CID。")
	output(tool("fastboot", "-s", fbserial, "flash", "boot", filepath.Join(root, "payload", "installer-boot.img")))
	output(tool("fastboot", "-s", fbserial, "reboot"))
	transferAndWait(dir, paths)
}
func main() {
	defer func() {
		if e := recover(); e != nil {
			fmt.Fprintln(os.Stderr, "ERROR:", e)
			os.Exit(1)
		}
	}()
	exe, e := os.Executable()
	must(e)
	root = filepath.Dir(exe)
	must(asciiPath(root))
	if real, e := filepath.EvalSymlinks(root); e == nil {
		must(asciiPath(real))
	}
	cwd, e := os.Getwd()
	must(e)
	must(asciiPath(cwd))
	for _, name := range []string{"ADB_SERVER_SOCKET", "ANDROID_ADB_SERVER_PORT", "ANDROID_SERIAL", "ADB_TRACE"} {
		must(os.Unsetenv(name))
	}
	localWork := filepath.Join(root, "work")
	must(os.MkdirAll(filepath.Join(localWork, "android"), 0700))
	must(os.MkdirAll(filepath.Join(localWork, "tmp"), 0700))
	must(os.Setenv("TEMP", filepath.Join(localWork, "tmp")))
	must(os.Setenv("TMP", filepath.Join(localWork, "tmp")))
	must(os.Setenv("ANDROID_USER_HOME", filepath.Join(localWork, "android")))
	must(os.Setenv("ADB_VENDOR_KEYS", filepath.Join(localWork, "android")))
	check := flag.Bool("check", false, "只读检查 Android root/硬件和包文件")
	verify := flag.Bool("verify", false, "只校验离线包，不连接设备")
	flash := flag.Bool("install", false, "备份后安装，必须手动输入 ERASE-序列号")
	backup := flag.Bool("backup", false, "只备份，不刷写")
	restorePath := flag.String("restore", "", "恢复指定设备备份，要求 Fastboot")
	wifiPath := flag.String("wifi", "", "使用本次安装专用维护密钥配置 Wi-Fi，参数为备份目录")
	statusPath := flag.String("status", "", "使用本次安装专用维护密钥检查 fnOS，参数为备份目录")
	sharePath := flag.String("share", "", "创建明确的私有 POSIX SMB 共享，参数为备份目录")
	host := flag.String("host", "192.168.77.1", "维护连接的设备 IP，默认 USB 地址")
	target := flag.String("output", "", "本次备份目录，必须为 ASCII 路径")
	flag.Parse()
	for _, p := range []string{*target, *restorePath, *wifiPath, *statusPath, *sharePath} {
		if p != "" {
			v, e := filepath.Abs(p)
			must(e)
			must(asciiPath(v))
		}
	}
	if !*verify && !*check && !*flash && !*backup && *restorePath == "" && *wifiPath == "" && *statusPath == "" && *sharePath == "" {
		fmt.Println("SP1099X fnOS 离线刷机工具 r1\n用法: SP1099X-Flasher.exe --check | --backup | --install | --restore BACKUP_DIR\n维护: --wifi BACKUP_DIR 或 --status BACKUP_DIR [--host DEVICE_IP]\n解压到纯英文目录，例如 D:\\SP1099X-fnOS；先阅读 docs 中的刷机说明。\n工具只使用本目录 tools 中的 ADB/Fastboot。其他机型/分区/设备树一律拒绝。")
		return
	}
	selected := 0
	for _, v := range []bool{*verify, *check, *flash, *backup, *restorePath != "", *wifiPath != "", *statusPath != "", *sharePath != ""} {
		if v {
			selected++
		}
	}
	if selected != 1 {
		panic("一次只能选择一个操作")
	}
	dir := *target
	if dir == "" {
		dir = filepath.Join(root, "backups", time.Now().Format("20060102-150405"))
	}
	dir, e = filepath.Abs(dir)
	must(e)
	must(asciiPath(dir))
	if *restorePath != "" {
		dir, e = filepath.Abs(*restorePath)
		must(e)
		must(asciiPath(dir))
	}
	fmt.Println("校验离线包完整性...")
	verifyPackage()
	if *verify {
		fmt.Println("离线包校验通过；未连接或修改设备。")
		return
	}
	if *sharePath != "" {
		dir, e = filepath.Abs(*sharePath)
		must(e)
		must(asciiPath(dir))
		manageShare(dir, *host)
		return
	}
	if *wifiPath != "" || *statusPath != "" {
		dir = *wifiPath
		if dir == "" {
			dir = *statusPath
		}
		dir, e = filepath.Abs(dir)
		must(e)
		must(asciiPath(dir))
		if *wifiPath != "" {
			manageWiFi(dir, *host)
		} else {
			manageStatus(dir, *host)
		}
		return
	}
	if *check {
		preflight()
		fmt.Println("硬件与 GPT 检查通过；未写入设备。")
		return
	}
	if *backup {
		_, cid := preflight()
		backupAll(dir, cid)
		fmt.Println("备份完成，未刷写:", dir)
		return
	}
	if *flash {
		install(dir)
		return
	}
	restore(dir)
}

// Keep context import available for platform-independent transport tests.
var _ = context.Background
