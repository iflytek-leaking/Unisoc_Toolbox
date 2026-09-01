// 内嵌资源管理 —— 自解压（SFX）语义。
//
// 所有外部工具（spd_dump / adb / fastboot / fastboot_sprd / 签名私钥 / FDL 文件 /
// 紫光驱动 R4.21.3201）在编译期通过 go:embed 打进二进制，对外只有一个 exe。
//
// 运行期行为按“自解压归档”设计：
//   - 每次启动把资源解压到一个【本次会话私有】的临时目录
//     （C:\ProgramData\KDXFUnlockTool\run-XXXXXX，路径恒为 ASCII 安全——
//     因为 spd_dump/fastboot 这类 CRT 程序用 ANSI 代码页解析参数，中文路径必炸，
//     这正是原教程反复强调“路径不能含中文”的根本原因）；
//   - 运行期间产生的输出（identifier_token.txt / signature.bin 等）
//     放在 exe 旁的 KDXF_out，供用户随时查看；
//   - 【程序一关闭，上述临时目录与产出数据全部自动删除】（包括点 X、Ctrl+C），
//     退出后系统里不留下任何痕迹。
package main

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed assets.zip
var assetsZip []byte

// childMode 为 true 表示当前是提权后的驱动安装子进程（--install-drivers）：
// 它有自己的私有临时目录，且不得触碰主进程的 KDXF_out。
var childMode = false

// Paths 汇总工具箱运行期要用的所有目录。
type Paths struct {
	RunDir  string // 本次会话的私有临时解压目录（退出即删）
	BinDir  string // 工具 exe 所在目录
	KeyFile string // sign.pem 落盘位置（备份用；签名直接读内存）
	FdlRoot string // FDL 文件根目录
	OutDir  string // 面向用户的输出目录（token / signature）
}

// SigFile 解锁签名文件的输出位置（OutDir 下，退出时会被清理）。
func (p Paths) SigFile() string { return filepath.Join(p.OutDir, "signature.bin") }

// TokenFile 原始 fastboot 输出的落盘位置。
func (p Paths) TokenFile() string { return filepath.Join(p.OutDir, "identifier_token.txt") }

// PrepareAssets 程序启动时调用：创建会话临时目录并解压必要资源。
func PrepareAssets() (*Paths, error) {
	zr, err := zip.NewReader(bytes.NewReader(assetsZip), int64(len(assetsZip)))
	if err != nil {
		return nil, fmt.Errorf("内嵌资源损坏: %w", err)
	}

	runDir, err := makeRunDir()
	if err != nil {
		return nil, fmt.Errorf("创建临时解压目录失败: %w", err)
	}
	if !isASCIISafePath(runDir) {
		// 极端情况（如中文用户名且 ProgramData 不可写）：提前明示风险
		warn("注意：运行路径包含非 ASCII 字符，个别工具可能异常：" + runDir)
	}

	// 解压除驱动外的全部资源（驱动 zip 20MB，装驱动时才解，见 ExtractDrivers）
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "drivers/") {
			continue
		}
		if err := extractZipFile(f, runDir); err != nil {
			os.RemoveAll(runDir)
			return nil, err
		}
	}

	p := &Paths{
		RunDir:  runDir,
		BinDir:  filepath.Join(runDir, "bin"),
		KeyFile: filepath.Join(runDir, "key", "sign.pem"),
		FdlRoot: filepath.Join(runDir, "fdls"),
		OutDir:  pickOutDir(runDir),
	}
	if err := os.MkdirAll(p.OutDir, 0o755); err != nil {
		return nil, err
	}
	return p, nil
}

// makeRunDir 创建本次会话私有、ASCII 安全的临时目录。
// 优先 ProgramData（恒为纯 ASCII 路径），退 LocalAppData，最后 Temp。
func makeRunDir() (string, error) {
	for _, name := range []string{"ProgramData", "LOCALAPPDATA"} {
		base := os.Getenv(name)
		if base == "" || !isASCIISafePath(base) {
			continue
		}
		parent := filepath.Join(base, "KDXFUnlockTool")
		if dir, err := os.MkdirTemp(parent, "run-"); err == nil {
			return dir, nil
		}
	}
	return os.MkdirTemp("", "KDXFUnlockTool-")
}

// Cleanup 会话结束清理：删除临时解压目录（含父目录若变空）。
// 用户产出数据由 RemoveOutputs 处理，两者配合实现“关闭即自清”。
func (p Paths) Cleanup() {
	removeWithRetry(p.RunDir)
	// run-XXXX 的父目录若已空，顺手移除（保持 ProgramData 干净）
	if parent := filepath.Dir(p.RunDir); parent != "" {
		os.Remove(parent)
	}
}

// RemoveOutputs 删除本次为用户产生的数据文件；目录若因此变空则一并删除
// （用户自己放进去的文件不受影响：只删我们命名的这几个）。
func (p Paths) RemoveOutputs() {
	for _, f := range []string{
		p.TokenFile(),
		filepath.Join(p.OutDir, "parsed_identifier_token.txt"),
		p.SigFile(),
	} {
		os.Remove(f)
	}
	os.Remove(p.OutDir) // 仅在目录为空时才会成功
}

// removeWithRetry 删除目录树（带重试）。点 X 关闭时可能有刚被杀掉的子进程
// 尚未完全释放 exe 文件句柄，稍等重试可显著提高清干净的概率。
func removeWithRetry(dir string) {
	for i := 0; i < 5; i++ {
		if err := os.RemoveAll(dir); err == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	os.RemoveAll(dir)
}

// ReadAsset 直接从内嵌 zip 读文件内容（不落地），用于签名私钥。
func ReadAsset(name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(assetsZip), int64(len(assetsZip)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("内嵌资源中找不到 %s", name)
}

// ExtractDrivers 把驱动包解到 RunDir\drivers，返回 Driver_R4.21.3201 根目录。
// 只在装驱动时调用，跳过无用的 Doc 目录；装完后随本次会话目录一并自动清理。
func ExtractDrivers(p *Paths) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(assetsZip), int64(len(assetsZip)))
	if err != nil {
		return "", err
	}
	var driverZip *zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "drivers/") && strings.HasSuffix(f.Name, ".zip") {
			driverZip = f
		}
	}
	if driverZip == nil {
		return "", fmt.Errorf("内嵌资源里找不到驱动包")
	}

	rc, err := driverZip.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	dzr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("驱动包损坏: %w", err)
	}
	dst := filepath.Join(p.RunDir, "drivers")
	for _, f := range dzr.File {
		if strings.HasPrefix(filepath.ToSlash(f.Name), "Driver_R4.21.3201/Doc/") {
			continue // 说明书 PDF 不需要
		}
		if err := extractZipFile(f, dst); err != nil {
			return "", err
		}
	}
	return filepath.Join(dst, "Driver_R4.21.3201"), nil
}

// extractZipFile 解压 zip 内的单个文件到 root 下的相对路径。
func extractZipFile(f *zip.File, root string) error {
	name := filepath.FromSlash(f.Name)
	// 防 Zip Slip
	clean := filepath.Clean(name)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return fmt.Errorf("非法资源路径: %s", name)
	}
	dst := filepath.Join(root, clean)

	if f.FileInfo().IsDir() {
		return os.MkdirAll(dst, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, rc)
	return err
}

// pickOutDir 用户输出目录：exe 旁的 KDXF_out（用户最容易找到），
// 不可写则退回临时目录内（退出时同样被清理，语义一致）；
// 驱动安装子进程直接用临时目录，避免父子进程互删对方数据。
func pickOutDir(runDir string) string {
	if childMode {
		return filepath.Join(runDir, "out")
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "KDXF_out")
		if probeWritable(cand) {
			return cand
		}
	}
	return filepath.Join(runDir, "out")
}

// probeWritable 通过实际创建文件来探测目录可写性。
func probeWritable(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".wprobe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return false
	}
	os.Remove(probe)
	return true
}

// isASCIISafePath 检查路径是否只含 ASCII（喂给 CRT 程序的参数要求）。
func isASCIISafePath(p string) bool {
	for _, r := range p {
		if r > 127 {
			return false
		}
	}
	return true
}
