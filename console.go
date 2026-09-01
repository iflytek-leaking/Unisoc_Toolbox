// 控制台交互的跨平台通用部分。
// Windows 特有的初始化（UTF-8 代码页、提权、版本检测）在 console_windows.go，
// 其他平台在 console_others.go（空实现，保证代码可编译）。
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// vtEnabled 为 true 时可以输出 ANSI 颜色（Windows 10+ 的 conhost/Windows Terminal 支持）
var vtEnabled = false

// ANSI 颜色码
const (
	cReset  = "\x1b[0m"
	cRed    = "\x1b[31m"
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cCyan   = "\x1b[36m"
	cBold   = "\x1b[1m"
)

// 染色输出（终端不支持时原样返回）
func color(code, s string) string {
	if !vtEnabled {
		return s
	}
	return code + s + cReset
}

func info(s string) { fmt.Println(color(cCyan, s)) }
func ok(s string)   { fmt.Println(color(cGreen, s)) }
func warn(s string) { fmt.Println(color(cYellow, s)) }
func fail(s string) { fmt.Println(color(cRed, s)) }

func hr() {
	fmt.Println("============================================================")
}

func title(s string) {
	fmt.Println()
	hr()
	fmt.Println("  " + s)
	hr()
}

// readLine 打印提示语并读取一整行输入。
// 每次新建 bufio.Reader，避免共享 reader 把外部子进程（如 FDL2 交互会话）
// 之后用户输入的字符预先吞掉。
func readLine(prompt string) string {
	fmt.Print(prompt)
	r := bufio.NewReader(os.Stdin)
	s, _ := r.ReadString('\n')
	return strings.TrimSpace(s)
}

// pause 暂停等待用户按回车（等价于 BAT 的 pause，但更跨平台可靠）。
func pause(msg string) {
	if msg == "" {
		msg = "按回车继续..."
	}
	readLine(msg)
}

// confirm 询问 yes/no，默认 no。输入 y/yes（不区分大小写）返回 true。
func confirm(prompt string) bool {
	s := readLine(prompt + " [y/N]: ")
	return strings.EqualFold(s, "y") || strings.EqualFold(s, "yes")
}

// readMultiLine 读取多行输入（粘贴多行 token 等），连续两次回车（空行）结束。
func readMultiLine(prompt string) string {
	fmt.Println(prompt)
	fmt.Println("（可一次粘贴多行，粘贴完成后按回车结束输入）")
	r := bufio.NewReader(os.Stdin)
	var sb strings.Builder
	for {
		line, err := r.ReadString('\n')
		if strings.TrimSpace(line) == "" {
			if sb.Len() > 0 || err != nil {
				break
			}
			continue
		}
		sb.WriteString(line)
		if err != nil {
			break
		}
	}
	return sb.String()
}
