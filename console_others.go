//go:build !windows

// 非 Windows 平台的占位实现：本工具面向 Windows（驱动和 fastboot_sprd 均为 Win-only），
// 但保留可编译性，方便在 Linux/macOS 上跑单元测试（签名算法、token 解析等纯逻辑）。
package main

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
)

func initConsole()     {}
func isElevated() bool { return false }
func relaunchElevated(args string) error {
	return errors.New("仅 Windows 支持自动提权")
}
func windowsMajorVersion() uint32 { return 10 }

// registerConsoleCleanup 非 Windows 下用信号实现同等的退出清理。
func registerConsoleCleanup(fn func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		fn()
		os.Exit(1)
	}()
}
