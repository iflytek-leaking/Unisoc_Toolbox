# 科大讯飞学习机 · 紫光展锐 BL 解锁工具箱（单文件版）

把原项目 [KDXF-LEAKiNG-Project](https://github.com/iflytek-leaking/KDXF-LEAKiNG-Project) 里
**「无SPRD4通用解BL工具」的全套 BAT 流程** 封装成了一个单文件 Windows 可执行程序：
**`kdxf-unlock-toolbox.exe`（约 35MB）**——双击即用，无需准备任何依赖。

适用机型：紫光展锐 UD710（chip0/chip1/chip2）、T310/ums312、T760/ums9620 平台机型
（Z1 / X2 / X2Pro / X3Pro / T10 / T20 / C6 / C8 / SA30 / SA30Pro / TX20 / C10系 / Q10 等，SPRD4 被阉割、走 SPRD3 kick 方案的设备）。

## 内置组件（全部随 exe 自带）

| 组件 | 说明 |
|---|---|
| `spd_dump.exe` | 深刷/kick 工具（含 Channel9.dll、Channel.ini） |
| `adb.exe` / `fastboot.exe` | 通用调试桥（含 AdbWinApi/AdbWinUsbApi） |
| `fastboot_sprd.exe` | 展锐专用 fastboot（get_identifier_token / unlock_bootloader） |
| `sign.pem`（RSA-4096） | 展锐通用解锁签名私钥 |
| FDL 文件 | ud710 chip0/1、ud710 chip2、t310(ums312)、ums9620(T760, dram1/dram2) 四类 |
| 紫光驱动 R4.21.3201 | sprdvcom/sprdvmdm/rdavcom/sprdadb（Win10 + Win7/8 两套） |

## 与原 BAT 方案的对照

| 原方案痛点 | 本工具 |
|---|---|
| 6 个 BAT 分步执行、互相传文件 | 一个 exe，菜单/一键流程，`KDXF_out` 统一输出 |
| BAT 文件 GBK/UTF-8 编码混杂 → cmd 里乱码 | 程序启动即强制控制台 UTF-8，永不乱码 |
| 缺 unlockbl.sh / 需要 busybox+openssl+VC++ 运行库才能签名 | **Go 原生实现 RSA-PKCS1v15+SHA-256 签名**（与 openssl 输出做了字节级一致性测试），这些组件全部退役 |
| token 要开记事本手抄、多行要自己拼 | fastboot 输出**自动捕获**，`Identifier token:` 到 `OK` 之间所有行的 hex **自动按序拼接**，自动校验，人眼确认即可 |
| 路径必须无中文无空格 | 工作目录自动落位 ASCII 安全路径；exe 本体放哪都行，**包括中文目录** |
| spd_dump 缺 MSVCP140 时一脸懵 | 自动检测退出码 0xC0000135，一键从微软官方装运行库 |
| 驱动要手动解压找安装程序 | 菜单 [1] 自动提权、导 reg、装 INF（pnputil 静默 + DriverSetup 兜底） |
| 解压出一堆文件散落各地，用完还得手动删 | **自解压（SFX）模式**：资源解到会话私有临时目录，**关闭程序（菜单退出/点 X/Ctrl+C）即自动删除全部临时文件与产出数据**，不留痕迹 |

## 使用方法

**首次使用**：`[1] 安装驱动` → `[7] ★一键自动解锁`。

一键流程会自动串联：

```
进入 Bootloader → 获取并拼接 token → 生成 signature.bin → 解锁 → 校验状态
   (2)              (3)                (4)            (5)      (6)
```

中途任何一步失败都会停住并给出中文排查建议；token 和 signature 会自动保存，
可在主菜单从对应步骤单独继续，不必从头再来。

分步操作（和传统流程一致）：

1. **[1]** 安装驱动与运行环境（会先请求 UAC 管理员权限）
2. **[2]** 进入 Bootloader：设备完全关机 → 回车 → 插 USB 线（不按键）。
   程序并发执行 `spd_dump --kickto` 与 adb 侦测——一旦 adb 见到 recovery 设备
   即判 kick 成功，**立即强杀 spd_dump 的读秒进程**并自动接力 reboot → fastboot
3. **[3]** 获取 identifier_token：自动解析并展示拼接结果（含 ASCII 预览，可和设备序列号比对）
4. **[4]** 生成 signature.bin（纯本地签名，无需联网）
5. **[5]** 解锁：留意设备屏幕——出现 `Unlock device may erase user data` 时
   **按音量下键＝确认解锁**（音量上＝取消；个别机型为音量选择+电源确认，以屏幕为准）。
   输出停在 `unlocking bootloader` 超过 5 秒时程序会主动提醒你按键
6. **[6]** 校验：返回 "Bootloader can not been unlocked repeatly" 即解锁成功

> 启动时的【操作前检查清单】请务必照做：①拔掉所有无关 USB/安卓设备
> （adb/fastboot 没有设备区分能力，会误操作到其他手机）；②确认数据已备份。

进阶：`[8]` FDL2 深刷读写模式（ud710 c0c1 / c2 / t310 / ums9620(T760, dram1/dram2) 多组 FDL 内置，
进去后直接 `path D:\backup` + `r all` 备份）、`[9]` 重新上锁、`[0]` 工具命令行。

输出文件在 exe 旁边的 **`KDXF_out\`**：`identifier_token.txt`（fastboot 原始输出）、
`parsed_identifier_token.txt`（确认后的 token）、`signature.bin`。

> **自解压/自清说明**：本程序是“用完即净”的自解压模式——内嵌工具在每次运行时
> 解到临时目录，**关闭程序时临时目录与 `KDXF_out` 中的产出数据会被自动删除**。
> 若想把 signature.bin 留作他用，请在关闭程序前自行复制一份。

## identifier token 拼接规则（与使用者逐条确认的定稿）

1. 找到以 `Identifier token:` 开头的锚点行，**冒号右侧为基底字符串**；
2. 继续向下读取每一行，把行内所有 **hex 字符（0-9 和 A-F，小写转大写）**
   按原有顺序追加到基底末尾；括号、单词、空格等一切非 hex 字符一律过滤；
3. 遇到含 `OK` / `OKAY` / `Finished` / `FAILED` 的行**立即停止**，该行不参与；
4. 空行只是跳过，不中断范围；行长不做任何挑拣
   （`114`+`5141919`+`810`+`00000`+`22222` → `11451419198100000022222`）；
5. 拼接结果在签名前经过校验：必须为偶数长度（hex 两两成对才能解码）、
   4~64 字节。奇数会红字警告并允许手动修正——原 sh 脚本奇数时会静默签出错误结果，此处不继承该缺陷。

完整示例（这 5 个用例已在 `tokenparse_test.go` 中固化为单元测试）：

```
identifier token:                    ┐
5341 (sys) 3032 tmp 3230             │  (sys)/tmp 等干扰词整体滤掉
(oops) 3133 3234 [xxx] 3339          ├──→ 5341303232303133323433393239363437
3239 str 3634                        │
37                                   │
OKAY [  0.031s]                     ←┘ 停止，0.031 不参与
```

## 构建与开发

需要 Go 1.21+（不需要任何第三方依赖，纯标准库）：

```bash
./build.sh        # 打包 assets_src → assets.zip，go test，交叉编译 exe
```

手动分步：

```bash
go run ./cmd/assetpack -src assets_src -out assets.zip   # 先产出内嵌资源
go test -v ./...                                         # 13 项单元测试
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  go build -trimpath -ldflags "-s -w" -o kdxf-unlock-toolbox.exe .
```

仓库自带 GitHub Actions（`.github/workflows/build.yml`）：push 自动构建并上传产物，
打 `v*` 标签自动发 Release。

**更新内置组件**：直接替换 `assets_src/` 下对应文件后重新构建即可——
新驱动放 `assets_src/drivers/*.zip`（自动识别），新工具放 `assets_src/bin/`，
新增芯片 FDL 在 `steps.go` 的 `fdlSets` 里加一行。

## 目录结构

```
kdxf-unlock-toolbox/
├── main.go                 # 入口、主菜单、一键流程
├── steps.go                # 装驱动/进BL/取token/签名/解锁/FDL2/上锁/命令行
├── signer.go               # unlockbl.sh 的 Go 原生实现（RSA-PKCS1v15+SHA256）
├── tokenparse.go           # token 自动捕获与多行拼接（用户定稿规则）
├── assets.go               # 内嵌资源解压、ASCII 安全运行目录
├── proc.go                 # 子进程执行辅助（捕获/流式/交互/0xC0000135 识别）
├── console.go / console_windows.go / console_others.go
├── cmd/assetpack/          # 构建期资源打包器
├── assets_src/             # 内嵌资源源文件（驱动/工具/FDL/私钥）
├── patches/unlockbl.sh     # 原项目缺失的 unlockbl.sh（放回 配套文件/展讯/无SPRD4通用解BL工具/ 即可用）
└── testdata/sig_gold.hex   # openssl 生成的金标准签名（一致性测试用）
```

## 常见问题

- **spd_dump 一闪而过/提示缺 DLL**：菜单 [1] 会自动检测并安装官方 VC++ 运行库。
- **kick 等待超时**：确认设备已完全关机；换机箱后置 USB 口和数据线；确认驱动已装。
- **获取 token 卡住**：设备是否在 fastboot 模式（菜单 2 会自动接力到 fastboot）。
- **signature 验证不过 / 解锁失败**：第 3 步会展示 token 的 ASCII 预览，
  先和设备序列号比对；token 文件也保留在 `KDXF_out` 供手工核对。

## 来源与致谢

- 破解教程与工具流：[iflytek-leaking/KDXF-LEAKiNG-Project](https://github.com/iflytek-leaking/KDXF-LEAKiNG-Project)（原「无SPRD4通用解BL工具」各 BAT 作者）
- 签名算法来源：[zhuofan-16/Spectrum_UnlockBL_Tool](https://github.com/zhuofan-16/Spectrum_UnlockBL_Tool) 的 `unlockbl.sh`（展讯 secureboot avb2.0，shijiu.ren@spreadtrum.com）
- FDL 文件与 spd_dump：[TomKing062/spreadtrum_flash](https://github.com/TomKing062/spreadtrum_flash)
- 驱动：紫光展锐官方 R4.21.3201

## 免责声明

解锁/刷机会清空设备数据并使设备失去官方保修，操作不当可能变砖。
本工具仅供学习研究，使用造成的设备损坏、数据丢失由使用者自行承担。
