package main

import "testing"

func mustExtract(t *testing.T, out string) string {
	t.Helper()
	tok, err := ExtractIdentifierToken(out)
	if err != nil {
		t.Fatalf("解析失败: %v\n输入:\n%s", err, out)
	}
	return tok
}

// 例1 真机抓图标准输出：两段（长行 + "37"），OKAY/Finished 行不参与
func TestExample1RealMachine(t *testing.T) {
	tok := mustExtract(t, `identifier token:
53413032323031333234333932393634
37
OKAY [  0.031s]
Finished. Total time: 0.031s
`)
	if want := "5341303232303133323433393239363437"; tok != want {
		t.Fatalf("\n got: %s\nwant: %s", tok, want)
	}
}

// 例2 (bootloader) 前缀 + 三段 + A-F 字母必须作为数据保留
func TestExample2BootloaderPrefix(t *testing.T) {
	tok := mustExtract(t, `(bootloader) identifier token:
(bootloader) 303132333435363038
(bootloader) 3941424344
(bootloader) 4546
OKAY [  0.002s]
Finished. Total time: 0.002s
`)
	if want := "30313233343536303839414243444546"; tok != want {
		t.Fatalf("\n got: %s\nwant: %s", tok, want)
	}
}

// 例3 每行混入括号/单词/分组空格 —— 干扰全部滤掉，结果与例1一致
func TestExample3NoisyLines(t *testing.T) {
	tok := mustExtract(t, `identifier token:
5341 (sys) 3032 tmp 3230
(oops) 3133 3234 [xxx] 3339
3239 str 3634
37
OKAY [  0.031s]
Finished. Total time: 0.031s
`)
	if want := "5341303232303133323433393239363437"; tok != want {
		t.Fatalf("\n got: %s\nwant: %s", tok, want)
	}
}

// 例4 锚点行内联基底 + 小写转大写 + "OK" 提前停止（0.01 不参与）
func TestExample4InlineBaseAndOKStop(t *testing.T) {
	tok := mustExtract(t, `Identifier token: 5a41
3032 junk 3230
OK 0.01
3032
`)
	if want := "5A4130323230"; tok != want {
		t.Fatalf("\n got: %s\nwant: %s", tok, want)
	}
}

// 例5 用户极端例子：行长五花八门、无锚点 —— 无条件按序拼接
func TestExample5ArbitraryLines(t *testing.T) {
	tok := mustExtract(t, "114\n5141919\n810\n00000\n22222\n")
	if want := "11451419198100000022222"; tok != want {
		t.Fatalf("\n got: %s\nwant: %s", tok, want)
	}
	// 23 位为奇数：拼接照旧，但校验必须报警（hex 必须成对）
	if _, err := ValidateTokenHex(tok); err == nil {
		t.Fatal("奇数长度必须触发校验警告")
	}
}

// 范围内出现空行不中断拼接
func TestEmptyLineInsideRange(t *testing.T) {
	tok := mustExtract(t, "identifier token:\n5341\n\n3032\nOKAY\n")
	if want := "53413032"; tok != want {
		t.Fatalf("\n got: %s\nwant: %s", tok, want)
	}
}

// 无 OKAY 时 Finished 也结束范围，其后内容不参与
func TestFinishedStopsRange(t *testing.T) {
	tok := mustExtract(t, "identifier token:\n5341\nFinished. Total time: 0.031s\n3032\n")
	if want := "5341"; tok != want {
		t.Fatalf("\n got: %s\nwant: %s", tok, want)
	}
}

// 无数据才报错。真实的 fastboot 失败输出必含 FAILED（停止词），因此范围内零数据。
func TestNoData(t *testing.T) {
	if _, err := ExtractIdentifierToken("FAILED (remote: not supported)\nFinished. Total time: 0.001s\n"); err == nil {
		t.Fatal("无任何 hex 数据时应报错")
	}
}

// 边界展示：无锚点的垃圾文本理论上可能凑出 hex 字母（如 remote error -> EEE），
// 但三层防御保证它不会造成事故：
//  1. 自动流程中失败输出必含 FAILED → 零数据 → 直接报失败；
//  2. 侥幸产出的碎片过不了 ValidateTokenHex 的 4 字节下限（自动流程会硬停）；
//  3. 手工粘贴流程中拼接结果必然先展示给用户 [y/N] 确认后才用于签名。
func TestGarbageOnlyFailsValidation(t *testing.T) {
	tok := mustExtract(t, "(remote error)\n") // 无条件提取规则下的诚实结果：EEE
	if _, err := ValidateTokenHex(tok); err == nil {
		t.Fatal("垃圾碎片必须被 ValidateTokenHex 拦截")
	}
}

func TestValidate(t *testing.T) {
	if n, err := ValidateTokenHex("5341303232303133323433393239363437"); err != nil || n != 17 {
		t.Fatalf("合法 token 校验异常: n=%d err=%v", n, err)
	}
	if _, err := ValidateTokenHex(""); err == nil {
		t.Fatal("空 token 必须报错")
	}
	long := make([]byte, 0, 130)
	for i := 0; i < 130; i++ {
		long = append(long, '3')
	}
	if _, err := ValidateTokenHex(string(long)); err == nil {
		t.Fatal("超长 token 必须报错")
	}
}
