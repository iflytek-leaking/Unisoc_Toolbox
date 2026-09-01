// 子进程执行辅助。
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 已启动子进程登记表：程序被关闭（点 X / Ctrl+C）时先杀掉所有子进程，
// 再删除临时目录——否则被占用的 exe 删不掉，"自动清理"会失败。
var (
	cmdMu   sync.Mutex
	cmdList []*exec.Cmd
)

func trackCmd(c *exec.Cmd) {
	cmdMu.Lock()
	cmdList = append(cmdList, c)
	cmdMu.Unlock()
}

// killAllCmds 杀掉全部登记在册的子进程（清理阶段调用）。
func killAllCmds() {
	cmdMu.Lock()
	defer cmdMu.Unlock()
	for _, c := range cmdList {
		if c != nil && c.Process != nil {
			c.Process.Kill()
		}
	}
}

// runCapture 执行命令并返回合并输出（stdout+stderr）。
// fastboot/adb 的标准信息大多打到 stderr，合并捕获最省事。
func runCapture(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	trackCmd(cmd)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	fmt.Print(out) // 同步回显给用户看
	return out, err
}

// runQuiet 执行命令返回合并输出但不回显——用于后台轮询（adb/fastboot devices），
// 避免每 2 秒刷一屏。
func runQuiet(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	trackCmd(cmd)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// runInteractive 把命令接到当前控制台（stdin/stdout/stderr 直通），
// 用于 spd_dump FDL2 交互会话、DriverSetup 这类需要用户直接操作的程序。
func runInteractive(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	trackCmd(cmd)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runStream 流式执行：每收到一行输出就打印并回调 onLine；
// 返回收集到的全文输出（供事后解析）与执行错误。
func runStream(dir, name string, args []string, onLine func(string)) (string, error) {
	cmd := exec.Command(name, args...)
	trackCmd(cmd)
	cmd.Dir = dir
	pr, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = cmd.Stdout // sprd 系工具信息走 stderr 的也一并合并
	if err := cmd.Start(); err != nil {
		return "", err
	}
	var full strings.Builder
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		fmt.Println(line)
		full.WriteString(line + "\n")
		if onLine != nil {
			onLine(line)
		}
	}
	err = cmd.Wait()
	return full.String(), err
}

// toolPath 返回 BinDir 下某个工具的完整路径。
func (p Paths) tool(name string) string { return filepath.Join(p.BinDir, name) }

// 退出码 0xC0000135 = -1073741515：STATUS_DLL_NOT_FOUND，
// 典型场景就是缺 VC++ 运行库（MSVCP140.dll / VCRUNTIME140.dll）。
func isMissingDLL(err error) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		code := ee.ExitCode()
		return code == -1073741515 || code == -1073741519 || code == -1073741792
	}
	return false
}

// 统一处理“工具起不来”的常见原因，给出可操作的中文指引。
func explainRunErr(tool string, err error) {
	if err == nil {
		return
	}
	if isMissingDLL(err) {
		fail(fmt.Sprintf("【%s 启动失败】缺少 VC++ 运行库（MSVCP140/VCRUNTIME140）。", tool))
		warn("请回到主菜单执行 [1] 安装驱动与运行库，程序会自动检测并安装官方运行库。")
		return
	}
	if strings.Contains(err.Error(), "executable file not found") {
		fail(fmt.Sprintf("【%s 启动失败】找不到程序文件，内嵌资源可能未正确解压。", tool))
		return
	}
	fail(fmt.Sprintf("【%s 执行异常】%v", tool, err))
}

// waitFor 周期性执行 probe，直到返回 true 或超时。返回是否等到。
func waitFor(timeout time.Duration, interval time.Duration, probe func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if probe() {
			return true
		}
		time.Sleep(interval)
	}
	return false
}
