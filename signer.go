// unlockbl.sh 的 Go 原生实现。
//
// 原脚本（展讯 secureboot avb2.0 sign identifier，作者 shijiu.ren@spreadtrum.com，
// 来自 zhuofan-16/Spectrum_UnlockBL_Tool）流程：
//  1. identifier（hex 字符串）-> 二进制 identifier.bin
//  2. 右侧补零到 64 字节（超长直接报错）
//  3. openssl dgst -sha256 -sign PRIVATE_KEY  =>  RSA PKCS#1 v1.5 + SHA-256 签名
//
// Go 标准库 (crypto/rsa + crypto/sha256 + crypto/x509) 与 openssl 的
// `dgst -sign` 严格等价（PKCS#1 v1.5 是确定性算法，无随机数参与输出），
// signer_test.go 里有与 openssl 生成结果的字节级对比测试（golden file）。
//
// 由此我们彻底不需要：openssl.exe / busybox.exe / libcrypto-3-x64.dll /
// libssl-3-x64.dll / VC++ 运行库（仅剩 spd_dump 需要，见 steps.go 的处理）。
package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
)

// identifierMaxSize 与脚本中的 IDENTIFIER_BIN_MAX_SIZE 一致
const identifierMaxSize = 64

// token 文本的规范化（多行拼接、去标签、去干扰）在 tokenparse.go 中实现，
// SignToken 会调用 CleanIdentifierToken。

// IdentifierToBin 复刻 doStringToBinary：hex -> bytes -> 右侧补零到 64 字节。
func IdentifierToBin(hexStr string) ([]byte, error) {
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("hex 解码失败: %w", err)
	}
	if len(raw) > identifierMaxSize {
		return nil, fmt.Errorf("Identifier string too long（%d 字节 > %d 字节上限）",
			len(raw), identifierMaxSize)
	}
	padded := make([]byte, identifierMaxSize)
	copy(padded, raw)
	return padded, nil
}

// SignIdentifierBin 复刻 doIdentifierSign：RSA PKCS#1 v1.5 + SHA-256。
// 支持 PKCS#8（BEGIN PRIVATE KEY）与 PKCS#1（BEGIN RSA PRIVATE KEY）两种 PEM，
// 以及加密的传统 PEM（暂不需要，sign.pem 是无加密的 PKCS#8 RSA-4096）。
func SignIdentifierBin(id64 []byte, pemBytes []byte) ([]byte, error) {
	if len(id64) != identifierMaxSize {
		return nil, fmt.Errorf("identifier 必须为 %d 字节", identifierMaxSize)
	}
	key, err := parseRSAPrivateKey(pemBytes)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(id64)
	// rand.Reader 仅用于 RSA blinding 防时序侧信道，不影响输出内容（确定性）
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return nil, fmt.Errorf("签名失败: %w", err)
	}
	return sig, nil
}

// SignToken 一步封装：raw token 字符串 -> (64 字节 identifier, 签名)。
func SignToken(rawToken string, pemBytes []byte) (id64 []byte, sig []byte, err error) {
	tok, err := CleanIdentifierToken(rawToken)
	if err != nil {
		return nil, nil, err
	}
	if _, err := ValidateTokenHex(tok); err != nil {
		return nil, nil, err
	}
	id64, err = IdentifierToBin(tok)
	if err != nil {
		return nil, nil, err
	}
	sig, err = SignIdentifierBin(id64, pemBytes)
	if err != nil {
		return nil, nil, err
	}
	return id64, sig, nil
}

// parseRSAPrivateKey 兼容常见 PEM 形式的 RSA 私钥。
func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("无效的 PEM 私钥文件")
	}
	// PKCS#8（sign.pem 就是这种："BEGIN PRIVATE KEY"）
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := k.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("PEM 中的私钥不是 RSA")
	}
	// PKCS#1（"BEGIN RSA PRIVATE KEY"）
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, fmt.Errorf("无法解析私钥（支持 PKCS#8 / PKCS#1 PEM）")
}
