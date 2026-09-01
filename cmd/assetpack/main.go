// assetpack 把 assets_src 目录打成 assets.zip 供主程序 go:embed 内嵌。
// 用法: go run ./cmd/assetpack -src assets_src -out assets.zip
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	src := flag.String("src", "assets_src", "资源源目录")
	out := flag.String("out", "assets.zip", "输出 zip 路径")
	flag.Parse()

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建输出失败:", err)
		os.Exit(1)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	count := 0
	err = filepath.Walk(*src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(*src, path)
		if err != nil {
			return err
		}
		// zip 内部统一用正斜杠
		rel = filepath.ToSlash(rel)

		w, err := zw.Create(rel)
		if err != nil {
			return err
		}
		r, err := os.Open(path)
		if err != nil {
			return err
		}
		defer r.Close()
		if _, err := io.Copy(w, r); err != nil {
			return err
		}
		count++
		fmt.Println("  +", rel)
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "打包失败:", err)
		os.Exit(1)
	}
	if err := zw.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "写入 zip 失败:", err)
		os.Exit(1)
	}

	fi, _ := os.Stat(*out)
	fmt.Printf("打包完成: %s (%d 个文件, %.1f MB)\n", *out, count,
		float64(fi.Size())/1024/1024)
	_ = strings.TrimSpace // 防御性保留
}
