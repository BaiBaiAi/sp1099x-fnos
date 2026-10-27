package main

import (
	"bufio"
	"bytes"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func managedClient(dir, host string) *ssh.Client {
	private, err := os.ReadFile(filepath.Join(dir, "maintenance-key.pem"))
	must(err)
	signer, err := ssh.ParsePrivateKey(private)
	must(err)
	log, err := os.ReadFile(filepath.Join(dir, "install.log"))
	must(err)
	var expected ssh.PublicKey
	for _, line := range strings.Split(string(log), "\n") {
		if i := strings.Index(line, "SSH_HOST_KEY "); i >= 0 {
			key, _, _, _, e := ssh.ParseAuthorizedKey([]byte(line[i+13:]))
			if e == nil {
				expected = key
			}
		}
	}
	if expected == nil {
		panic("安装日志中没有设备 SSH 公钥，拒绝跳过身份校验；请先核对该设备的安装日志")
	}
	cfg := &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, Timeout: 8 * time.Second, HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
		if !bytes.Equal(k.Marshal(), expected.Marshal()) {
			return fmt.Errorf("设备 SSH 身份与本次安装记录不一致")
		}
		return nil
	}}
	client, err := ssh.Dial("tcp", net.JoinHostPort(host, "2222"), cfg)
	must(err)
	return client
}
func managedRun(c *ssh.Client, command, input string) string {
	s, e := c.NewSession()
	must(e)
	defer s.Close()
	s.Stdin = strings.NewReader(input)
	out, e := s.CombinedOutput(command)
	if e != nil {
		panic(fmt.Errorf("设备命令失败: %w: %s", e, out))
	}
	return string(out)
}
func manageWiFi(dir, host string) {
	c := managedClient(dir, host)
	defer c.Close()
	fmt.Print(managedRun(c, "nmcli -f IN-USE,SSID,CHAN,SIGNAL,SECURITY device wifi list ifname wlan0 --rescan yes", ""))
	fmt.Print("Wi-Fi 名称 SSID: ")
	reader := bufio.NewReader(os.Stdin)
	ssid, e := reader.ReadString('\n')
	must(e)
	ssid = strings.TrimSpace(ssid)
	if ssid == "" {
		panic("SSID 不能为空")
	}
	fmt.Print("Wi-Fi 密码（不回显）: ")
	raw, e := term.ReadPassword(int(os.Stdin.Fd()))
	must(e)
	fmt.Println()
	password := string(raw)
	if strings.ContainsAny(password, "\r\n") {
		panic("不支持含换行的密码")
	}
	// A dedicated profile; no changes to wired or private USB configuration.
	command := "if nmcli connection show sp1099x-wifi >/dev/null 2>&1; then nmcli connection modify sp1099x-wifi 802-11-wireless.ssid " + quoteSH(ssid) + " 802-11-wireless-security.psk ''; else nmcli connection add type wifi ifname wlan0 con-name sp1099x-wifi ssid " + quoteSH(ssid) + "; fi; nmcli connection modify sp1099x-wifi 802-11-wireless-security.key-mgmt wpa-psk 802-11-wireless-security.proto rsn ipv4.method auto ipv4.route-metric 600 ipv6.method disabled connection.autoconnect yes connection.stable-id sp1099x-wifi 802-11-wireless.cloned-mac-address stable 802-11-wireless.powersave 2; nmcli --wait 30 connection up sp1099x-wifi passwd-file /dev/stdin"
	fmt.Print(managedRun(c, "set -eu; "+command, "802-11-wireless-security.psk:"+password+"\n"))
	for i := range raw {
		raw[i] = 0
	}
	password = ""
	fmt.Print(managedRun(c, "nmcli -f GENERAL.STATE,GENERAL.CONNECTION,IP4.ADDRESS,IP4.GATEWAY device show wlan0", ""))
}
func manageStatus(dir, host string) {
	c := managedClient(dir, host)
	defer c.Close()
	fmt.Print(managedRun(c, "uname -a; cat /proc/uptime; systemctl is-active sp1099x-release-firstboot sp1099x-native-wifi sp1099x-screen docker trim_app_center; ip -br address; df -h / /vol1; cat /sys/class/power_supply/battery/capacity", ""))
}

func manageShare(dir, host string) {
	c := managedClient(dir, host)
	defer c.Close()
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("已创建的飞牛管理员/用户账号: ")
	account, e := reader.ReadString('\n')
	must(e)
	fmt.Print("新的私有共享名称（英文字母开头，英文数字下划线）: ")
	share, e := reader.ReadString('\n')
	must(e)
	fmt.Print(managedRun(c, "/bin/bash /usr/local/libexec/sp1099x-release/native-share.sh "+quoteSH(strings.TrimSpace(account))+" "+quoteSH(strings.TrimSpace(share)), ""))
}
