package main

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// 与 unlockbl.sh + openssl 的输出做字节级对比（golden file）。
// golden 由以下等价命令生成（PKCS#1 v1.5 签名是确定性的，多次运行结果一致）：
//
//	printf "3031...4546" | xxd -r -p > id.bin; head -c 48 /dev/zero >> id.bin
//	openssl dgst -sha256 -sign sign.pem -out sig.bin id.bin
const sampleToken = "30313233343536303839414243444546"

func TestSignMatchesOpenSSL(t *testing.T) {
	pemBytes, err := os.ReadFile(filepath.Join("assets_src", "key", "sign.pem"))
	if err != nil {
		t.Skip("找不到 sign.pem，跳过（CI 打包前请先生成 assets）:", err)
	}
	goldHex, err := os.ReadFile(filepath.Join("testdata", "sig_gold.hex"))
	if err != nil {
		t.Fatal("找不到 golden 签名:", err)
	}
	gold, err := hex.DecodeString(string(bytes.TrimSpace(goldHex)))
	if err != nil {
		t.Fatal("golden 签名 hex 解码失败:", err)
	}

	id64, sig, err := SignToken(sampleToken, pemBytes)
	if err != nil {
		t.Fatal("Go 签名失败:", err)
	}

	if len(id64) != identifierMaxSize {
		t.Fatalf("identifier 长度错误: %d", len(id64))
	}
	// 原脚本语义：16 字节 identifier + 48 字节零填充
	if !bytes.Equal(id64[:16], bytes.Repeat([]byte{0}, 0)) && string(id64[:16]) != "0123456089ABCDEF" {
		t.Fatalf("identifier 前 16 字节应为 hex 解码后的 ASCII: %q", id64[:16])
	}
	if !bytes.Equal(id64[16:], make([]byte, 48)) {
		t.Fatal("identifier 右侧应补零到 64 字节")
	}

	if !bytes.Equal(sig, gold) {
		t.Fatalf("Go 签名与 openssl 签名不一致！\ngo: %x\ngold: %x", sig[:32], gold[:32])
	}
	t.Logf("签名与 openssl 字节级一致（%d 字节，RSA-%d）", len(sig), len(sig)*8)
}

// 用真机 token 走一遍完整流程，确认端到端可用。
func TestSignRealMachineToken(t *testing.T) {
	pemBytes, err := os.ReadFile(filepath.Join("assets_src", "key", "sign.pem"))
	if err != nil {
		t.Skip("找不到 sign.pem:", err)
	}
	tok, err := ExtractIdentifierToken(`identifier token:
53413032323031333234333932393634
37
OKAY [  0.031s]
Finished. Total time: 0.031s
`)
	if err != nil {
		t.Fatal(err)
	}
	id64, sig, err := SignToken(tok, pemBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("token=%s (%d hex)", tok, len(tok))
	t.Logf("identifier 前 17 字节(ASCII)=%q，总长度 %d，签名 %d 字节",
		id64[:17], len(id64), len(sig))
}

func TestIdentifierTooLong(t *testing.T) {
	longTok := hex.EncodeToString(make([]byte, 65)) // 65 字节 > 64 上限
	if _, err := IdentifierToBin(longTok); err == nil {
		t.Fatal("应当拒绝超过 64 字节的 identifier")
	}
}
