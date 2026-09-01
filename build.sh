#!/usr/bin/env bash
# 一键构建脚本：打包内嵌资源 → 跑单元测试 → 交叉编译 Windows 单文件 exe
set -e
cd "$(dirname "$0")"

echo "[1/3] 打包内嵌资源 assets_src -> assets.zip ..."
go run ./cmd/assetpack -src assets_src -out assets.zip

echo "[2/3] 单元测试（含与 openssl 的字节级一致性对比）..."
go test ./...

echo "[3/3] 交叉编译 Windows/amd64 ..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  go build -trimpath -ldflags "-s -w" -o kdxf-unlock-toolbox.exe .

echo ""
echo "[√] 构建完成: kdxf-unlock-toolbox.exe"
ls -lh kdxf-unlock-toolbox.exe
