// 各功能步骤的实现。每一步对应原「无SPRD4通用解BL工具」里的一个/一组 BAT，
// 但全部内聚到单个二进制内完成：工具内嵌、token 自动捕获拼接、签名本地完成。
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// App 保存会话状态（目录、已确认的 token）。
type App struct {
	P     *Paths
	Token string
}

// parsedTokenFile 已确认 token 的持久化位置（重启工具后可直接复用）。
func (p Paths) parsedTokenFile() string {
	return filepath.Join(p.OutDir, "parsed_identifier_token.txt")
}

// sigWorkFile 供 fastboot 使用的签名副本（永远在 ASCII 安全路径下）；
// OutDir 里那份是给用户看的。
func (p Paths) sigWorkFile() string { return filepath.Join(p.RunDir, "work", "signature.bin") }

// ============================================================
// [1] 安装驱动（含 VC++ 运行库自动检测修复）
// ============================================================

func (a *App) stepInstallDrivers() {
	title("安装驱动与运行环境")
	fmt.Println("将安装：紫光展锐 USB 驱动 R4.21.3201（sprd 深刷/ADB/Fastboot 全套）。")
	fmt.Println("这一刻必须拥有管理员权限。若当前不是管理员，可自动触发 UAC 提权。")
	fmt.Println()

	if !isElevated() {
		warn("当前不是管理员权限。")
		if confirm("是否自动弹出 UAC 并以管理员身份继续安装？（推荐）") {
			if err := relaunchElevated("--install-drivers"); err != nil {
				fail("自动提权失败: " + err.Error())
				warn("请关闭本程序，改为右键「以管理员身份运行」。")
				return
			}
			ok("已弹出新的管理员窗口，请在新窗口中完成驱动安装。本窗口可继续其他操作。")
			return
		}
		warn("已跳过。驱动未安装前，后续所有需要连接设备的步骤都会失败。")
		return
	}
	a.runDriverInstall()
}

// runDriverInstall 实际的安装流程（要求已提权）；--install-drivers 子命令也走这里。
func (a *App) runDriverInstall() {
	// 1) 解压驱动包
	info("▶ 正在解压内嵌驱动包...")
	drvRoot, err := ExtractDrivers(a.P)
	if err != nil {
		fail("解压驱动失败: " + err.Error())
		return
	}

	// 2) 按系统版本选驱动目录：Win10/11 → DriversForWin10；Win7/8.x → DriversForWin78
	sub := "DriversForWin78"
	if windowsMajorVersion() >= 10 {
		sub = "DriversForWin10"
	}
	drvDir := filepath.Join(drvRoot, sub)
	info("▶ 系统版本驱动目录: " + sub)

	// 3) 导入 USB 配置（原 BAT 的 regedit /s config.reg）
	reg := filepath.Join(drvDir, "config.reg")
	if _, err := os.Stat(reg); err == nil {
		info("▶ 导入 USB 配置 config.reg ...")
		if out, err := runCapture(drvDir, "regedit", "/s", reg); err != nil {
			warn("config.reg 导入可能失败: " + err.Error() + " " + out)
		}
	}

	// 4) 安装驱动：Win10+ 优先用系统自带 pnputil 静默装 INF；失败/老系统则用官方 DriverSetup.exe
	installed := false
	if windowsMajorVersion() >= 10 {
		info("▶ 使用 pnputil 安装驱动 INF（sprdvcom / sprdvmdm / rdavcom / sprdadb）...")
		out, err := runCapture(drvDir, "pnputil", "/add-driver",
			filepath.Join(drvDir, "Drivers", "*.inf"), "/subdirs", "/install")
		if err == nil && !strings.Contains(out, "失败") {
			installed = true
		} else {
			warn("pnputil 安装未完全成功，改用官方安装程序。")
		}
	}
	if !installed {
		setup := filepath.Join(drvDir, "DriverSetup.exe")
		if _, err := os.Stat(setup); err == nil {
			info("▶ 启动官方驱动安装程序 DriverSetup.exe（请在弹出的窗口中完成安装）...")
			_ = runInteractive(drvDir, setup)
			installed = true
		} else {
			fail("找不到 DriverSetup.exe，请手动到以下目录安装: " + filepath.Join(drvDir, "Drivers"))
		}
	}

	// 5) VC++ 运行库：本工具自身不需要（Go 静态编译），但 spd_dump.exe 依赖
	//    MSVCP140/VCRUNTIME140 —— 原 BAT 方案装运行库就是为了它和 openssl。
	a.ensureVCRuntime()

	// 6) 探测 spd_dump 能否正常启动（0xC0000135 = 缺 DLL）
	info("▶ 自检 spd_dump 是否可启动...")
	err = exec.Command(a.P.tool("spd_dump.exe"), "--help").Run()
	if isMissingDLL(err) {
		explainRunErr("spd_dump.exe", err)
	} else {
		ok("spd_dump 自检通过。")
	}

	fmt.Println()
	ok("驱动安装流程结束。若设备管理器里仍有感叹号设备，可手动将驱动指向：")
	fmt.Println("    " + filepath.Join(drvDir, "Drivers"))
	pause("")
}

// ensureVCRuntime 检测并（按需）在线安装官方 VC++ 运行库。
func (a *App) ensureVCRuntime() {
	sys32 := filepath.Join(os.Getenv("SystemRoot"), "System32")
	msvcp := filepath.Join(sys32, "msvcp140.dll")
	vcrun := filepath.Join(sys32, "vcruntime140.dll")
	_, e1 := os.Stat(msvcp)
	_, e2 := os.Stat(vcrun)
	if e1 == nil && e2 == nil {
		ok("VC++ 运行库已存在（spd_dump 依赖满足）。")
		return
	}
	warn("未检测到 VC++ 运行库（MSVCP140/VCRUNTIME140），spd_dump 将无法启动。")
	if !confirm("是否现在从微软官方下载安装 VC++ 运行库？（需要联网，约 24MB）") {
		warn("已跳过。之后若 spd_dump 报缺 DLL，请回到菜单 [1] 重装。")
		return
	}
	const url = "https://aka.ms/vs/17/release/vc_redist.x64.exe"
	dst := filepath.Join(a.P.RunDir, "vc_redist.x64.exe")
	info("▶ 正在下载 " + url + " ...")
	if err := downloadFile(url, dst); err != nil {
		fail("下载失败: " + err.Error())
		warn("可稍后手动下载安装: " + url)
		return
	}
	info("▶ 正在静默安装 VC++ 运行库...")
	if err := exec.Command(dst, "/install", "/quiet", "/norestart").Run(); err != nil {
		warn("运行库安装器返回错误: " + err.Error() + "（若已装过更新版本可忽略）")
		return
	}
	ok("VC++ 运行库安装完成。")
}

// downloadFile 简易下载（带超时）。
func downloadFile(url, dst string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, resp.Body)
	return err
}

// ============================================================
// [2] 进入 Bootloader（关机 kickto 法，对应 0-关机方法进入bootloader.bat）
// ============================================================

// stepEnterBootloader kick 阶段设计（按真机流程定稿）：
// spd_dump --kickto 与 adb 设备侦测从第 0 秒【并发】起跑——真机上 adb 出现
// recovery 条目的速度通常快于 spd_dump 自己宣布 connected；adb 一见到
// recovery/device 设备即视为 kick 成功，立刻强杀 spd_dump 进程，不陪它读秒。
// 前置要求（启动检查清单已让用户确认）：电脑上不得连接其他 USB/安卓设备，
// 否则 adb/fastboot 无法区分会误判/误操作。
func (a *App) stepEnterBootloader() error {
	title("进入 Bootloader（关机方法）")
	fmt.Println("操作步骤：")
	fmt.Println("  1. 将学习机【完全关机】；")
	fmt.Println("  2. 确认电脑上【没有连接其他 USB/安卓设备】；")
	fmt.Println("  3. 回车后程序并发执行 spd_dump 等待 + adb 设备侦测；")
	fmt.Println("  4. 用 USB 数据线连接学习机（此时不要按平板上任何按键）。")
	fmt.Println()
	pause("准备好后按回车开始等待设备...")

	cmd := exec.Command(a.P.tool("spd_dump.exe"), "--kickto", "1", "--wait", "100")
	trackCmd(cmd)
	cmd.Dir = a.P.BinDir
	pr, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		explainRunErr("spd_dump.exe", err)
		return err
	}
	// spd_dump 的输出照旧转发到控制台（能看到读秒进度）
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 256*1024), 1024*1024)
		for sc.Scan() {
			fmt.Println(sc.Text())
		}
	}()

	// adb 侦测协程：静默轮询 adb devices，出现 recovery/device/sideload 即命中
	stopPoll := make(chan struct{})
	adbFound := make(chan string, 1) // 命中时回传 adb devices 完整输出
	go func() {
		for {
			select {
			case <-stopPoll:
				return
			default:
			}
			out, _ := runQuiet(a.P.BinDir, a.P.tool("adb.exe"), "devices")
			for _, ln := range strings.Split(out, "\n") {
				f := strings.Fields(ln)
				if len(f) == 2 && (f[1] == "recovery" || f[1] == "device" || f[1] == "sideload") {
					select {
					case adbFound <- out:
					default:
					}
					return
				}
			}
			time.Sleep(2 * time.Second)
		}
	}()

	dumpExit := make(chan error, 1)
	go func() { dumpExit <- cmd.Wait() }()

	select {
	case out := <-adbFound:
		close(stopPoll)
		fmt.Println()
		ok("检测到 ADB 设备上线，kick 已成功（黑屏 Recovery / Cali 模式）：")
		fmt.Print(out)
		info("▶ 强行结束 spd_dump 等待进程，立即推进后续步骤...")
		_ = cmd.Process.Kill()
		<-dumpExit
	case err := <-dumpExit:
		close(stopPoll)
		if err != nil {
			explainRunErr("spd_dump.exe", err)
		}
		fail("spd_dump 已退出，但未检测到任何 ADB 设备。")
		warn("排查：1) 设备是否完全关机  2) 数据线是否支持数据传输  3) 换机箱后置 USB 口  4) 驱动是否已装（菜单 1）")
		return fmt.Errorf("kick 未连接设备")
	case <-time.After(130 * time.Second):
		close(stopPoll)
		_ = cmd.Process.Kill()
		fail("等待设备超时。")
		warn("排查：1) 设备是否完全关机  2) 数据线是否支持数据传输  3) 换机箱后置 USB 口  4) 驱动是否已装（菜单 1）")
		return fmt.Errorf("等待设备超时")
	}

	// ADB 设备在 kick 命中时已经确认在线，直接推进，不再重复等待。
	// 阶段 2：重启进 Bootloader（fastboot）
	fmt.Println()
	time.Sleep(1 * time.Second) // 给 adb 守护和设备半秒稳定
	info("▶ 正在重启到 Bootloader（fastboot）...")
	if _, err := runQuiet(a.P.BinDir, a.P.tool("adb.exe"), "reboot", "bootloader"); err != nil {
		warn("adb reboot 异常: " + err.Error())
	}

	// 阶段 3：等待 fastboot 设备
	info("▶ 正在等待 Fastboot 设备上线（最多 45 秒）...")
	found := waitFor(45*time.Second, 2*time.Second, func() bool {
		out, _ := runQuiet(a.P.BinDir, a.P.tool("fastboot.exe"), "devices")
		for _, ln := range strings.Split(out, "\n") {
			f := strings.Fields(ln)
			if len(f) >= 2 && f[1] == "fastboot" {
				ok("Fastboot 设备已就绪: " + f[0])
				return true
			}
		}
		return false
	})
	if !found {
		fail("未发现 Fastboot 设备。")
		warn("排查：重点怀疑 fastboot 驱动未装好（菜单 1），也可手动执行 fastboot devices 查看。")
		return fmt.Errorf("fastboot 设备未发现")
	}

	fmt.Println()
	ok("✔ 设备已进入 Bootloader，可以继续执行获取 token / 解锁。")
	return nil
}

// ============================================================
// [3] 获取 identifier_token（自动捕获+多行拼接，对应 1-获取identifier_token.bat）
// ============================================================

func (a *App) stepGetToken() error {
	title("获取 identifier_token")
	fmt.Println("请确保设备已处于 Fastboot 模式（未完成请先执行第 2 步）。")
	pause("设备就绪后按回车开始获取...")

	out, err := runCapture(a.P.BinDir, a.P.tool("fastboot_sprd.exe"), "oem", "get_identifier_token")
	if err != nil {
		warn("fastboot 返回错误码: " + err.Error())
	}

	// 原始输出完整落盘（与原 BAT 行为一致，便于核对）
	if werr := os.WriteFile(a.P.TokenFile(), []byte(out), 0o644); werr == nil {
		info("原始输出已保存: " + a.P.TokenFile())
	}

	// 自动解析：锚点行 → OK 之前的所有 hex 行依次拼接
	tok, perr := ExtractIdentifierToken(out)
	if perr != nil {
		warn("自动识别失败: " + perr.Error())
		tok = ""
	}

	if tok != "" {
		printTokenSummary(tok)
		if confirm("以上自动拼接结果是否正确？") {
			return a.acceptToken(tok)
		}
		warn("好的，进入手动模式。")
	}

	fmt.Println()
	fmt.Println("请打开原始输出文件（上面给了路径），从 identifier token: 那行开始")
	fmt.Println("把 token（可能有多行）原样复制，粘贴到下面：")
	raw := readMultiLine("")
	tok, err = CleanIdentifierToken(raw)
	if err != nil {
		fail("手动解析失败: " + err.Error())
		return err
	}
	printTokenSummary(tok)
	if !confirm("确认使用这个拼接结果？") {
		return fmt.Errorf("用户放弃该 token")
	}
	return a.acceptToken(tok)
}

// printTokenSummary 展示拼接结果及人类可读的核对信息。
func printTokenSummary(tok string) {
	fmt.Println()
	hr()
	fmt.Println("拼接后的 identifier token（共 " + fmt.Sprint(len(tok)) + " 个字符）：")
	ok("  " + tok)
	if n, err := ValidateTokenHex(tok); err != nil {
		fail("  校验警告: " + err.Error())
	} else {
		fmt.Printf("  长度校验通过: %d 字节（将右侧补零到 64 字节后签名）\n", n)
		if b, err := hexDecode(tok); err == nil {
			if s := asciiPreview(b); s != "" {
				fmt.Println("  内容预览(ASCII): \"" + s + "\"（可与设备序列号比对）")
			}
		}
	}
	hr()
}

// hexDecode token hex -> bytes（仅用于展示预览，签名校验走 IdentifierToBin）。
func hexDecode(tok string) ([]byte, error) {
	b := make([]byte, len(tok)/2)
	for i := 0; i < len(b); i++ {
		var v int
		if _, err := fmt.Sscanf(tok[i*2:i*2+2], "%02x", &v); err != nil {
			return nil, err
		}
		b[i] = byte(v)
	}
	return b, nil
}

// asciiPreview token 解码后若是可打印 ASCII（设备序列号常见），原样展示辅助人工核对。
func asciiPreview(b []byte) string {
	end := bytes.IndexByte(b, 0)
	if end < 0 {
		end = len(b)
	}
	if end == 0 {
		return ""
	}
	for _, c := range b[:end] {
		if c < 0x20 || c > 0x7E {
			return ""
		}
	}
	return string(b[:end])
}

// acceptToken 持久化并记录到会话状态。
func (a *App) acceptToken(tok string) error {
	a.Token = tok
	content := "identifier_token=" + tok + "\n"
	if err := os.WriteFile(a.P.parsedTokenFile(), []byte(content), 0o644); err != nil {
		warn("token 持久化失败（不影响本次会话使用）: " + err.Error())
	} else {
		ok("token 已保存: " + a.P.parsedTokenFile())
	}
	return nil
}

// loadSavedToken 尝试从历史文件恢复 token（重启工具后继续流程的场景）。
func (a *App) loadSavedToken() string {
	raw, err := os.ReadFile(a.P.parsedTokenFile())
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(ln, "identifier_token=") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "identifier_token="))
		}
	}
	return ""
}

// ============================================================
// [4] 生成 signature.bin（对应 2-生成signature.bat + unlockbl.sh，Go 原生）
// ============================================================

func (a *App) stepMakeSignature() error {
	title("生成 signature.bin（内置 RSA 签名）")
	fmt.Println("使用内置签名器（等价于 unlockbl.sh + openssl），无需安装任何运行库。")

	tok := a.Token
	if tok == "" {
		if saved := a.loadSavedToken(); saved != "" {
			fmt.Println("检测到上次保存的 token:")
			printTokenSummary(saved)
			if confirm("使用该 token？") {
				tok = saved
			}
		}
	}
	if tok == "" {
		fmt.Println("尚未获取 token。可先去第 3 步自动获取，或在下面手动粘贴（支持多行）：")
		raw := readMultiLine("")
		var err error
		tok, err = CleanIdentifierToken(raw)
		if err != nil {
			fail("解析失败: " + err.Error())
			return err
		}
		printTokenSummary(tok)
		if !confirm("确认使用该 token？") {
			return fmt.Errorf("用户放弃")
		}
	}

	// 签前校验（奇数/过短/超长在这里硬性拦截，绝不静默签出错误文件）
	if _, err := ValidateTokenHex(tok); err != nil {
		fail("token 校验未通过: " + err.Error())
		warn("请回到第 3 步重新获取。")
		return err
	}

	pemBytes, err := ReadAsset("key/sign.pem")
	if err != nil {
		fail("读取内嵌私钥失败: " + err.Error())
		return err
	}
	id64, sig, err := SignToken(tok, pemBytes)
	if err != nil {
		fail("签名失败: " + err.Error())
		return err
	}
	_ = id64

	// 两份落盘：OutDir 给用户看；RunDir/work 给 fastboot 用（ASCII 安全路径）
	if err := os.MkdirAll(filepath.Dir(a.P.sigWorkFile()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(a.P.sigWorkFile(), sig, 0o644); err != nil {
		fail("写入签名失败: " + err.Error())
		return err
	}
	if err := os.WriteFile(a.P.SigFile(), sig, 0o644); err != nil {
		warn("写入用户目录副本失败（不影响解锁）: " + err.Error())
	}

	fmt.Println()
	ok("✔ 签名生成成功（Identifier sign successfully）")
	fmt.Printf("  文件: %s（%d 字节，RSA-%d 签名）\n", a.P.SigFile(), len(sig), len(sig)*8)
	a.Token = tok
	return nil
}

// ============================================================
// [5] 解锁 BL / [6] 检查解锁状态（对应 3-/4-，同一命令不同解读）
// ============================================================

// stepUnlock 解锁/校验。
// 真机观察（用户实拍）：fastboot 打印到 "unlocking bootloader" 后会一动不动地等
// 用户在设备上按键——
//
//	Warning: Unlock device may erase user data.
//	Press volume down button to confirm that.   <- 音量下＝确认
//	Press volume up button to cancel.           <- 音量上＝取消
//
// 直到按键后才继续输出（Begin to erase user data...）。期间程序看似"假死"，
// 故内置停滞看门狗：出现 unlocking bootloader 后 5 秒无新输出即提醒用户按键，
// 每 20 秒重复提醒直到输出恢复；另加 15 秒全静默兜底（防管道缓冲遮蔽标志行）。
func (a *App) stepUnlock(checkOnly bool) error {
	if checkOnly {
		title("检查解锁状态")
	} else {
		title("解锁 Bootloader")
		fmt.Println("即将执行：fastboot_sprd flashing unlock_bootloader signature.bin")
		warn("执行后请留意【设备屏幕】：")
		warn("  出现确认提示时 —— 【按音量下键＝确认解锁】；【按音量上键＝取消】。")
		warn("  （个别机型为：音量键选择 YES、电源键确认，以屏幕实际文字为准）")
		warn("解锁会【清空全部数据并恢复出厂设置】！")
		if !confirm("确认继续解锁？") {
			return fmt.Errorf("用户取消")
		}
	}

	if _, err := os.Stat(a.P.sigWorkFile()); err != nil {
		fail("找不到 signature.bin，请先生成（第 4 步，或一键模式）。")
		return err
	}
	fmt.Println("请确保设备处于 Fastboot 模式。")
	pause("设备就绪后按回车执行...")

	var mu sync.Mutex
	lastLine := time.Now()
	sawUnlocking := false
	lastNotice := time.Time{}

	stopWatch := make(chan struct{})
	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case <-t.C:
				mu.Lock()
				idle := time.Since(lastLine)
				need, generic := false, false
				switch {
				case sawUnlocking && idle > 5*time.Second:
					need = true // 已见 unlocking bootloader 且 5 秒无变化 → 在等设备按键
				case !sawUnlocking && idle > 15*time.Second:
					need, generic = true, true // 全静默兜底
				}
				if need && (lastNotice.IsZero() || time.Since(lastNotice) > 20*time.Second) {
					lastNotice = time.Now()
					if generic {
						warn("【提醒】若设备屏幕上出现解锁确认提示，请按【音量下键】确认（音量上键＝取消）。")
					} else {
						warn("【提醒】正在等待设备确认！请看设备屏幕：")
						warn("  Press volume down button to confirm = 【按音量下键＝确认解锁】")
						warn("  Press volume up button to cancel   = 【按音量上键＝取消】")
					}
				}
				mu.Unlock()
			}
		}
	}()

	out, _ := runStream(a.P.BinDir, a.P.tool("fastboot_sprd.exe"),
		[]string{"flashing", "unlock_bootloader", a.P.sigWorkFile()},
		func(line string) {
			mu.Lock()
			lastLine = time.Now()
			if strings.Contains(strings.ToLower(line), "unlocking bootloader") {
				sawUnlocking = true
			}
			mu.Unlock()
		})
	close(stopWatch)

	low := strings.ToLower(out)

	fmt.Println()
	switch {
	case strings.Contains(low, "repeatly"):
		// “Bootloader can not been unlocked repeatly” = 设备已处于解锁状态
		ok("✔ 设备已是解锁状态（Bootloader can not been unlocked repeatly）。")
		if !checkOnly {
			ok("之前已经解锁过，无需重复操作。")
		}
		return nil
	case strings.Contains(low, "failed"):
		fail("✘ 解锁失败，fastboot 返回 FAILED。可尝试重新执行第 2~5 步。")
		return fmt.Errorf("unlock failed")
	case strings.Contains(low, "okay") || strings.Contains(low, "finished"):
		if checkOnly {
			warn("设备响应正常，但未检测到“已解锁”标记——大概率尚未解锁，请执行解锁。")
		} else {
			ok("✔ 解锁指令已执行成功。设备将自动恢复出厂并重启。")
			fmt.Println("  重启后屏幕底部应显示: info:lock flag is unlock!!! 属正常现象。")
		}
		return nil
	default:
		warn("未识别设备响应，请根据上方 fastboot 输出人工判断。")
		return nil
	}
}

// ============================================================
// [8] 进入 FDL2 深刷读写模式（对应 一键进入FDL2_*.bat，解锁后备份/刷机用）
// ============================================================

var fdlSets = []struct {
	name, dir, f1, f2 string
}{
	{"UD710 chip0/chip1（Z1/X2/X2Pro/X3Pro/T10/T20 等）", "ud710_c0c1", "0x5500_ud710", "0x9efffe00_ud710"},
	{"UD710 chip2（SA30/SA30Pro/TX20/C10 系 等）", "ud710_c2", "c2_0x5500", "c2_0x9efffe00"},
	{"T310 / ums312（Q10）", "t310", "0x5500_t310", "0x9efffe00_t310"},
}

func (a *App) stepFDL2() {
	title("进入 FDL2 深刷读写模式 (spd_dump)")
	warn("注意：不同芯片的 FDL 文件完全不同，选错不会变砖但会推送失败，重新选择即可。")
	for i, s := range fdlSets {
		fmt.Printf("  [%d] %s\n", i+1, s.name)
	}
	choice := readLine("请选择芯片型号 [1-3]: ")
	idx := -1
	switch strings.TrimSpace(choice) {
	case "1":
		idx = 0
	case "2":
		idx = 1
	case "3":
		idx = 2
	default:
		warn("无效选择。")
		return
	}
	set := fdlSets[idx]
	f1 := filepath.Join(a.P.FdlRoot, set.dir, set.f1)
	f2 := filepath.Join(a.P.FdlRoot, set.dir, set.f2)

	fmt.Println()
	fmt.Println("操作步骤：")
	fmt.Println("  1. 将设备【完全关机】")
	fmt.Println("  2. 回车后程序开始等待（600 秒窗口期）")
	fmt.Println("  3. 用 USB 线连接电脑（ud710 部分机型需按住音量+与电源键再插线）")
	fmt.Println("  4. 出现 FDL2> 提示符后即可输入命令，例如：")
	fmt.Println("       path D:\\backup     （设置备份目录）")
	fmt.Println("       r all              （备份全部分区，救砖保障，强烈建议）")
	fmt.Println("       r boot / w boot xx.bin / e userdata / reset")
	pause("准备好后按回车开始推送 FDL...")

	err := runInteractive(a.P.BinDir, a.P.tool("spd_dump.exe"),
		"--wait", "600", "loadfdl", f1, "loadfdl", f2, "exec")
	if err != nil {
		explainRunErr("spd_dump.exe", err)
	}
	fmt.Println()
	info("已退出 FDL2 会话，返回主菜单。")
}

// ============================================================
// [9] 重新上锁 / [0] 命令行
// ============================================================

func (a *App) stepRelock() {
	title("重新上锁 Bootloader")
	warn("上锁前请确保系统分区为官方完整系统，否则可能无法开机！")
	warn("上锁同样会清空数据。")
	if !confirm("确定要重新上锁？") || !confirm("再次确认：已恢复官方系统，确定上锁？") {
		info("已取消。")
		return
	}
	fmt.Println("请确保设备处于 Fastboot 模式。")
	pause("")
	out, _ := runCapture(a.P.BinDir, a.P.tool("fastboot_sprd.exe"), "flashing", "lock_bootloader")
	if strings.Contains(strings.ToLower(out), "okay") {
		ok("上锁指令已发送，按设备屏幕提示完成确认。")
	} else {
		warn("请根据上方输出判断结果。")
	}
}

func (a *App) stepShell() {
	title("工具命令行")
	fmt.Println("将打开一个新的 cmd 窗口：PATH 已注入工具目录，可直接使用")
	fmt.Println("  adb / fastboot / fastboot_sprd / spd_dump")
	fmt.Println("常用命令：")
	fmt.Println("  fastboot_sprd oem get_identifier_token        获取 token")
	fmt.Println("  fastboot_sprd flashing unlock_bootloader xx   解锁")
	fmt.Println("  fastboot_sprd flashing lock_bootloader        上锁")
	fmt.Println("  adb reboot bootloader                         重启到 fastboot")
	cmd := exec.Command("cmd", "/c", "start", "KDXF-BL-Tool",
		"cmd", "/k", "set PATH="+a.P.BinDir+";%PATH%&& cd /d "+a.P.BinDir)
	if err := cmd.Run(); err != nil {
		fail("打开失败: " + err.Error())
		return
	}
	ok("命令行窗口已打开。")
}
