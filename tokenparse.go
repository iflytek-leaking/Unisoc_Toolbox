// identifier token 提取与多行拼接 —— 最终规则（与用户逐条确认后定稿）：
//
// 【范围】从含 "identifier token" 的锚点行开始（冒号右侧为基底字符串），
// 到第一个含 OK / OKAY / Finished / FAILED 的行为止（该行不参与拼接）。
//
// 【提取】范围内每一行（含基底）只提取 hex 字符：0-9 与 A-F（小写自动转大写）。
// (bootloader) 前缀、括号、单词、空格、tab 等一切非 hex 字符一律视为干扰丢弃。
// 空行/纯噪声行贡献为零，但【不中断】范围——只有停止词结束范围。
//
// 【拼接】所有行按先后顺序、无任何长度/形状挑拣地拼成一整串：
// "114" + "5141919" + "810" + "00000" + "22222" → "11451419198100000022222"。
//
// 【校验】拼接本身无条件；但 hex 必须成对才能解码成字节——奇数长度只在
// ValidateTokenHex（签名前）报红字警告并允许手动修正，绝不静默通过
// （原 sh 脚本遇奇数会把末位当字面字符塞进二进制，签出错误结果且不报错）。
package main

import (
	"fmt"
	"strings"
)

// token 最长 64 字节 → 128 hex 字符（与原 unlockbl.sh 的 64 字节上限一致）
const tokenMaxHexLen = identifierMaxSize * 2

const bootPrefix = "(bootloader)"

// 停止词：命中任一即结束拼接。这些词里的字母（o/k/i/s/h 等）都不是 hex 字符，
// 因此真正的 token 数据行绝不可能误命中。
var stopWords = []string{"okay", "ok", "finished", "failed"}

func isStopLine(ln string) bool {
	low := strings.ToLower(ln)
	for _, w := range stopWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// stripBootPrefix 去掉行首的 fastboot 通道前缀（若不过滤，"bootloader"
// 中的 b/a/d/e 会被误认为 hex 数据）。
func stripBootPrefix(ln string) string {
	ln = strings.TrimSpace(ln)
	if strings.HasPrefix(ln, bootPrefix) {
		ln = strings.TrimSpace(ln[len(bootPrefix):])
	}
	return ln
}

// hexCharsOnly 只保留 0-9A-Fa-f（统一转大写），其余字符全部丢弃。
func hexCharsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'a' && r <= 'f':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'F':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// concatHexRange 从 lines[begin] 开始逐行提取 hex 并拼接，遇停止词行立即结束。
func concatHexRange(lines []string, begin int) string {
	var b strings.Builder
	for i := begin; i < len(lines); i++ {
		if isStopLine(lines[i]) {
			break
		}
		b.WriteString(hexCharsOnly(stripBootPrefix(lines[i])))
	}
	return b.String()
}

// findAnchor 定位含 "identifier token"（忽略大小写、可带 (bootloader) 前缀）的行。
func findAnchor(lines []string) int {
	for i, ln := range lines {
		if strings.Contains(strings.ToLower(stripBootPrefix(ln)), "identifier token") {
			return i
		}
	}
	return -1
}

// anchorBase 取锚点行的基底：优先"最后一个冒号右侧"；无冒号时取 "token" 一词之后。
// 锚点行绝不做整行提取——"identifier" 里的 d/e 会污染数据。
func anchorBase(ln string) string {
	s := stripBootPrefix(ln)
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return hexCharsOnly(s[i+1:])
	}
	low := strings.ToLower(s)
	if i := strings.Index(low, "token"); i >= 0 {
		return hexCharsOnly(s[i+len("token"):])
	}
	return ""
}

// ExtractIdentifierToken 从 fastboot_sprd 原始输出提取并拼接完整 token。
// 只在"一个字节都没提取到"时报错；长度是否合法由 ValidateTokenHex 在校验环节判定。
func ExtractIdentifierToken(output string) (string, error) {
	lines := strings.Split(output, "\n")
	if a := findAnchor(lines); a >= 0 {
		tok := anchorBase(lines[a]) + concatHexRange(lines, a+1)
		if tok == "" {
			return "", fmt.Errorf("找到了 token 标记，但没有提取到任何 hex 数据，请核对原始输出")
		}
		return tok, nil
	}
	// 无锚点（手工粘贴纯 token 行等场景）：同一套规则套用全文
	tok := concatHexRange(lines, 0)
	if tok == "" {
		return "", fmt.Errorf("未找到 identifier token，也未发现可拼接的 hex 数据")
	}
	return tok, nil
}

// CleanIdentifierToken 处理用户手动粘贴的文本（与 ExtractIdentifierToken 同规则）。
func CleanIdentifierToken(raw string) (string, error) {
	return ExtractIdentifierToken(raw)
}

// ValidateTokenHex 签名前的合法性校验。通过时返回解码后的字节数。
// 不通过绝不静默放行（原 sh 脚本奇数长度会签出错误签名，这是已知坑）。
//
// 注意：拼接阶段（ExtractIdentifierToken）按用户定稿规则不做任何挑拣；
// 偶数/长度/下限三道闸全部集中在签名前这一关，由调用方决定警告或放行。
func ValidateTokenHex(tok string) (int, error) {
	if tok == "" {
		return 0, fmt.Errorf("token 为空")
	}
	if len(tok)%2 != 0 {
		return 0, fmt.Errorf("token 共 %d 个 hex 字符，为奇数——hex 必须两两成对才能解码成字节，请核对是否少复制了一位", len(tok))
	}
	n := len(tok) / 2
	if n < 4 {
		return 0, fmt.Errorf("token 过短（%d 字节）——真实 identifier 至少 4 字节，拼接结果可能有误，请核对原始输出", n)
	}
	if n > identifierMaxSize {
		return 0, fmt.Errorf("Identifier string too long（%d 字节 > %d 字节上限）", n, identifierMaxSize)
	}
	return n, nil
}
