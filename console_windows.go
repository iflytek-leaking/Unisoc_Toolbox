//go:build windows

// Windows 控制台初始化与系统能力检测。
//
// 这一层彻底解决原 BAT 方案的两个顽疾：
//  1. BAT 文件 GBK/UTF-8 编码混杂导致的中文乱码 —— 我们在程序启动时
//     强制把控制台输入/输出代码页切到 UTF-8 (CP65001)，程序内字符串
//     统一 UTF-8，不再依赖 .bat 文件自身编码。
//  2. “需要右键以管理员身份运行” —— 程序可自行检测并用 UAC 弹窗自助提权。
//
// 全部通过 syscall 直接调 Win32 API，零第三方依赖。
package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	k32 = syscall.NewLazyDLL("kernel32.dll")

	pSetConsoleOutputCP    = k32.NewProc("SetConsoleOutputCP")
	pSetConsoleCP          = k32.NewProc("SetConsoleCP")
	pGetConsoleMode        = k32.NewProc("GetConsoleMode")
	pSetConsoleMode        = k32.NewProc("SetConsoleMode")
	pSetConsoleCtrlHandler = k32.NewProc("SetConsoleCtrlHandler")

	shell32        = syscall.NewLazyDLL("shell32.dll")
	pShellExecuteW = shell32.NewProc("ShellExecuteW")
	advapi32       = syscall.NewLazyDLL("advapi32.dll")
	pGetTokenInfo  = advapi32.NewProc("GetTokenInformation")
	ntdll          = syscall.NewLazyDLL("ntdll.dll")
	pRtlGetVersion = ntdll.NewProc("RtlGetVersion")
)

const (
	cpUTF8                 = 65001
	enableVirtualTerminal  = 0x0004
	tokenElevation         = 20                  // TOKEN_INFORMATION_CLASS.TokenElevation
	stdOutputHandle        = uintptr(0xFFFFFFF5) // (DWORD)-11
	swShowNormal           = 1
	shellExecuteMinSuccess = 32
)

// initConsole Windows 启动初期调用：切 UTF-8、开 ANSI 转义支持。
func initConsole() {
	// 输入/输出代码页切换到 UTF-8
	pSetConsoleOutputCP.Call(cpUTF8)
	pSetConsoleCP.Call(cpUTF8)

	// 尝试开启 ENABLE_VIRTUAL_TERMINAL_PROCESSING（Win10 TH2+ 有效）
	h, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil {
		return
	}
	var mode uint32
	r, _, _ := pGetConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return
	}
	r, _, _ = pSetConsoleMode.Call(uintptr(h), uintptr(mode|enableVirtualTerminal))
	vtEnabled = r != 0
}

// isElevated 判断当前进程是否已拥有管理员权限（UAC 提升后）。
func isElevated() bool {
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return false
	}
	var token syscall.Token
	if err := syscall.OpenProcessToken(h, syscall.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()

	var elevation uint32
	var outLen uint32
	r, _, _ := pGetTokenInfo.Call(
		uintptr(token),
		tokenElevation,
		uintptr(unsafe.Pointer(&elevation)),
		unsafe.Sizeof(elevation),
		uintptr(unsafe.Pointer(&outLen)),
	)
	return r != 0 && elevation != 0
}

// relaunchElevated 以“以管理员身份运行”方式重新启动自身（触发 UAC 弹窗）。
// args 透传给新进程；成功后当前进程应提示用户并退出或等待。
func relaunchElevated(args string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	params, _ := syscall.UTF16PtrFromString(args)
	dir, _ := syscall.UTF16PtrFromString("")

	r, _, callErr := pShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		swShowNormal,
	)
	if r <= shellExecuteMinSuccess {
		return fmt.Errorf("ShellExecuteW 失败(code=%d): %v", r, callErr)
	}
	return nil
}

// osVersionInfoExW 对应 RTL_OSVERSIONINFOEXW（RtlGetVersion 不受兼容性 shim 影响，
// 在 Win10/11 上也能拿到真实版本号，这对选驱动目录很关键）。
type osVersionInfoExW struct {
	dwOSVersionInfoSize uint32
	dwMajorVersion      uint32
	dwMinorVersion      uint32
	dwBuildNumber       uint32
	dwPlatformId        uint32
	szCSDVersion        [128]uint16
	wServicePackMajor   uint16
	wServicePackMinor   uint16
	wSuiteMask          uint16
	wProductType        byte
	wReserved           byte
}

// windowsMajorVersion 返回真实系统主版本号：10 => Win10/11，6 => Win7/8.x。
func windowsMajorVersion() uint32 {
	var v osVersionInfoExW
	v.dwOSVersionInfoSize = uint32(unsafe.Sizeof(v))
	r, _, _ := pRtlGetVersion.Call(uintptr(unsafe.Pointer(&v)))
	if r != 0 { // STATUS_SUCCESS = 0
		return 6 // 兜底按老系统处理
	}
	return v.dwMajorVersion
}

// registerConsoleCleanup 注册控制台事件回调：点 X 关闭 / Ctrl+C / 注销 / 关机
// 都会先执行 fn（清理临时数据），再让进程退出——这是"关闭即自清"的关键一环。
func registerConsoleCleanup(fn func()) {
	cb := syscall.NewCallback(func(ctrlType uint32) uintptr {
		// CTRL_C_EVENT=0 CTRL_BREAK_EVENT=1 CTRL_CLOSE_EVENT=2
		// CTRL_LOGOFF_EVENT=5 CTRL_SHUTDOWN_EVENT=6
		fn()
		return 0 // FALSE：交还默认处理（终止进程），清理已在 fn 内完成
	})
	pSetConsoleCtrlHandler.Call(cb, 1)
}
