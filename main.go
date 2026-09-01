// 科大讯飞 AI 学习机 · 紫光展锐（无SPRD4）BL 解锁工具箱 —— 单文件版
//
// 整合来源：
//   - iflytek-leaking/KDXF-LEAKiNG-Project 的「无SPRD4通用解BL工具」全套 BAT 流程
//   - zhuofan-16/Spectrum_UnlockBL_Tool 的 unlockbl.sh（Go 原生实现，经字节级一致性测试）
//   - 紫光驱动 R4.21.3201（用户指定版本，编译期内嵌）
//
// 设计亮点：
//
//	✔ 单文件：spd_dump / adb / fastboot / fastboot_sprd / 驱动 / 私钥全部内嵌
//	✔ 自解压（SFX）语义：资源在每次运行时解到会话私有临时目录，
//	  【程序一关闭（菜单退出 / 点 X / Ctrl+C），临时文件与产出数据全部自动删除】
//	✔ 无需 openssl / busybox / VC++ 运行库（签名在进程内完成；spd_dump 的运行库缺失会自动修复）
//	✔ token 自动捕获 + 多行拼接 + 校验（原方案手抄极易出错）
//	✔ 控制台强制 UTF-8，杜绝 BAT 编码混杂导致的乱码
//	✔ 运行目录恒为 ASCII 安全路径，不再要求用户手工避让中文目录
package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const version = "1.0.0"

var cleanupOnce sync.Once

// makeCleanup 返回幂等的清理函数：杀子进程 → 删用户产出 → 删临时解压目录。
// 两条路径都会触发：正常退出走 defer；点 X / Ctrl+C 走控制台事件回调。
func makeCleanup(p *Paths) func() {
	return func() {
		cleanupOnce.Do(func() {
			killAllCmds()
			time.Sleep(300 * time.Millisecond) // 等刚杀掉的进程释放文件句柄
			if !childMode {
				p.RemoveOutputs()
			}
			p.Cleanup()
		})
	}
}

func banner() {
	fmt.Println()
	fmt.Println("  ┌────────────────────────────────────────────────────────┐")
	fmt.Println("  │   科大讯飞学习机 · 紫光展锐 BL 解锁工具箱  v" + version + "        │")
	fmt.Println("  │   无SPRD4机型通用（SPRD3 kick 方案） · 单文件自解压版  │")
	fmt.Println("  └────────────────────────────────────────────────────────┘")
	fmt.Println()
	info("  自解压模式：关闭本程序即自动删除全部临时文件与产出数据。")
	fmt.Println()
}

func disclaimer() {
	hr()
	warn("【风险警告】（源自原项目 KDXF-LEAKiNG-Project，务必阅读）")
	fmt.Println("  · 解锁/刷机会清空设备全部数据，请先备份；")
	fmt.Println("  · 刷机将失去官方保修（见科大讯飞用户协议）；")
	fmt.Println("  · 操作不当可能变砖，官方救砖费用约 60-100 元；")
	fmt.Println("  · 本工具仅供学习研究，造成的设备损坏、数据丢失由使用者自行承担。")
	hr()
	pause("已知悉上述风险，按回车继续...")
	fmt.Println()
	hr()
	warn("【操作前检查清单】（逐条确认后再开始）")
	fmt.Println("  1. 已【拔掉】电脑上所有与本操作无关的 USB 设备（其他手机/平板/安卓盒")
	fmt.Println("     子等）——adb/fastboot 没有设备区分能力，连着别的设备会误判、误操作；")
	fmt.Println("  2. 已确认学习机上【没有需要备份的数据】——解锁会清空全部数据，不可恢复。")
	hr()
	pause("两项均已确认，按回车进入主菜单...")
}

func main() {
	initConsole()

	// 解析内部子命令
	sub := ""
	if len(os.Args) > 1 {
		sub = os.Args[1]
	}
	switch sub {
	case "--version", "-v":
		fmt.Println("kdxf-unlock-toolbox v" + version)
		return
	case "--help", "-h":
		fmt.Println("用法: 直接双击运行进入交互菜单；--install-drivers 仅内部提权使用。")
		return
	case "--install-drivers":
		childMode = true // 提权后的驱动安装子进程：不动主进程的输出目录
	}

	banner()
	if childMode {
		info("【管理员模式】正在执行驱动安装...")
		fmt.Println()
	} else {
		disclaimer()
	}

	p, err := PrepareAssets()
	if err != nil {
		fail("资源初始化失败: " + err.Error())
		pause("")
		return
	}
	cleanup := makeCleanup(p)
	defer cleanup()
	registerConsoleCleanup(func() { cleanup(); os.Exit(1) })

	app := &App{P: p}

	if childMode {
		app.runDriverInstall()
		return
	}

	info("本 次 输 出 目 录（token / signature 在此查看，退出程序后自动清空）:")
	fmt.Println("  " + p.OutDir)
	fmt.Println()

	mainMenu(app)
	fmt.Println("正在清理临时文件与产出数据...")
}

func printStatus(a *App) {
	tokStat := color(cYellow, "未获取")
	if a.Token != "" {
		tokStat = color(cGreen, fmt.Sprintf("已获取 (%d hex)", len(a.Token)))
	} else if saved := a.loadSavedToken(); saved != "" {
		tokStat = color(cGreen, fmt.Sprintf("已保存 (%d hex)", len(saved)))
	}
	sigStat := color(cYellow, "未生成")
	if _, err := os.Stat(a.P.SigFile()); err == nil {
		sigStat = color(cGreen, "已生成")
	}
	fmt.Printf("  状态 │ token: %s │ signature.bin: %s\n", tokStat, sigStat)
	hr()
}

func mainMenu(a *App) {
	for {
		fmt.Println()
		hr()
		fmt.Println("  主菜单")
		hr()
		printStatus(a)
		fmt.Println("  [1] 安装驱动与运行环境（管理员，首次必做）")
		fmt.Println("  ── 解 BL 四步 ─────────────────────────────")
		fmt.Println("  [2] 进入 Bootloader（关机 kickto 法）")
		fmt.Println("  [3] 获取 identifier_token（自动捕获+拼接）")
		fmt.Println("  [4] 生成 signature.bin（内置签名器）")
		fmt.Println("  [5] 解锁 Bootloader（设备上按音量 YES + 电源确认）")
		fmt.Println("  [6] 检查解锁状态")
		fmt.Println("  ── 快捷 ─────────────────────────────────")
		fmt.Println("  [7] ★ 一键自动解锁（2→3→4→5→6 连贯执行）")
		fmt.Println("  ── 进阶 ─────────────────────────────────")
		fmt.Println("  [8] 进入 FDL2 深刷读写模式（备份/刷机）")
		fmt.Println("  [9] 重新上锁 Bootloader")
		fmt.Println("  [0] 打开工具命令行（adb/fastboot 可用）")
		fmt.Println("  [Q] 退出（自动清理全部临时数据）")
		fmt.Println()

		switch strings.ToUpper(readLine("请选择: ")) {
		case "1":
			a.stepInstallDrivers()
		case "2":
			if err := a.stepEnterBootloader(); err != nil {
				warn("未完成，请根据提示排查后重试。")
				pause("")
			}
		case "3":
			if err := a.stepGetToken(); err != nil {
				warn("token 未确认。")
				pause("")
			}
		case "4":
			if err := a.stepMakeSignature(); err != nil {
				pause("")
			}
		case "5":
			if err := a.stepUnlock(false); err != nil {
				pause("")
			}
		case "6":
			_ = a.stepUnlock(true)
			pause("")
		case "7":
			a.stepOneClick()
			pause("")
		case "8":
			a.stepFDL2()
		case "9":
			a.stepRelock()
			pause("")
		case "0":
			a.stepShell()
		case "Q", "EXIT":
			fmt.Println("再见。祝折腾顺利！")
			return
		default:
			warn("无效输入，请输入菜单编号。")
		}
	}
}

// stepOneClick 一键自动解锁：串起 2→3→4→5→6，任一步失败即中断并提示。
func (a *App) stepOneClick() {
	title("★ 一键自动解锁 Bootloader")
	fmt.Println("将依次自动执行：进入 Bootloader → 获取并拼接 token → 生成签名 → 解锁 → 校验。")
	fmt.Println("全程只需：关机插线等待 + 解锁时在设备屏幕上【按音量下键确认】。")
	hr()
	warn("解锁会清空设备数据！请确认已备份。")
	if !confirm("开始一键流程？") {
		return
	}

	steps := []struct {
		name string
		fn   func() error
	}{
		{"进入 Bootloader", a.stepEnterBootloader},
		{"获取 identifier_token", a.stepGetToken},
		{"生成 signature.bin", a.stepMakeSignature},
		{"解锁 Bootloader", func() error { return a.stepUnlock(false) }},
		{"检查解锁状态", func() error { return a.stepUnlock(true) }},
	}
	for i, s := range steps {
		fmt.Println()
		info(fmt.Sprintf("━━━ 一键流程 第 %d/%d 步：%s ━━━", i+1, len(steps), s.name))
		if err := s.fn(); err != nil {
			fail(fmt.Sprintf("一键流程中断于「%s」。", s.name))
			info("排查后可回到主菜单，从对应步骤单独继续（本次会话中 token/signature 均保留）。")
			return
		}
	}
	fmt.Println()
	ok("🎉 一键解锁流程全部完成！设备将恢复出厂并重启。")
	fmt.Println("开机后屏幕底部出现 info:lock flag is unlock 即为成功（按两次电源键跳过警告）。")
}
