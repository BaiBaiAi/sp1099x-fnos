package main

import (
	"bytes"
	"encoding/json"
	"golang.org/x/crypto/ssh"
	"os"
	"path/filepath"
	"testing"
)

func TestPathGate(t *testing.T) {
	for _, s := range []string{`D:\SP1099X-fnOS`, `C:\My Tools\fnOS`, `E:\disk\pack`} {
		if asciiPath(s) != nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{`D:\刷机包`, `C:\Users\张三\fnOS`, `D:\café\fnOS`} {
		if asciiPath(s) == nil {
			t.Fatal("Accepted non-ASCII", s)
		}
	}
}
func TestStockGPTAndRejections(t *testing.T) {
	base := "testdata"
	raw, e := os.ReadFile(filepath.Join(base, "stock-gpt.bin"))
	if e != nil {
		t.Fatal(e)
	}
	var h Hardware
	b, e := os.ReadFile(filepath.Join(base, "hardware.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &h); e != nil {
		t.Fatal(e)
	}
	if e = gptParts(raw, h); e != nil {
		t.Fatal(e)
	}
	bad := append([]byte{}, raw...)
	bad[520] ^= 1
	if gptParts(bad, h) == nil {
		t.Fatal("Accepted bad header CRC")
	}
	bad = append([]byte{}, raw...)
	bad[1100] ^= 1
	if gptParts(bad, h) == nil {
		t.Fatal("Accepted bad entry CRC")
	}
	h.Parts[34].Size--
	if gptParts(raw, h) == nil {
		t.Fatal("Accepted wrong capacity")
	}
}
func TestRemoteShellQuote(t *testing.T) {
	got := quoteSH("a'b $x")
	if got != "'a'\"'\"'b $x'" {
		t.Fatal(got)
	}
}
func TestPrivateBackupACL(t *testing.T) { protectBackup(t.TempDir()) }
func TestGeneratedMaintenanceKey(t *testing.T) {
	d := t.TempDir()
	keyPair(d)
	private, e := os.ReadFile(filepath.Join(d, "maintenance-key.pem"))
	if e != nil {
		t.Fatal(e)
	}
	signer, e := ssh.ParsePrivateKey(private)
	if e != nil {
		t.Fatal(e)
	}
	public, e := os.ReadFile(filepath.Join(d, "authorized_keys"))
	if e != nil {
		t.Fatal(e)
	}
	key, _, _, _, e := ssh.ParseAuthorizedKey(public)
	if e != nil || !bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
		t.Fatal("Generated maintenance key mismatch", e)
	}
}
