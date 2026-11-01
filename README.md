# selphy-cli

用 Go 在电脑上连接 Canon SELPHY，查询状态、准备照片和打印。CLI 风格参考本机的 flomo-cli：独立二进制、Cobra 子命令、`--json`、环境变量和 `agent-guide`。

基于用户提供的 **SELPHY Photo Layout 4.4.0** APK 独立还原 CPNP 协议，不依赖 Android、Java、Canon 账户或云服务。原始 APK、反编译代码和厂商原生库不随项目分发。

## 当前支持情况

| 机型 | 实现 | 验证范围 |
| --- | --- | --- |
| CP1500 | UDP 发现/身份/状态、TCP 会话、JPEG 分块上传、单页队列、三种相纸 | 真机完成发现、状态、会话协商及一张 RP-108 校准图提交，设备由 busy 回到 idle、无报错；实物裁切需读图确认 |
| QX10 | CPNP 打印流程、方形照片、二维码 Wi-Fi 信息解码 | APK 静态分析与网络模拟器；尚无真机验证 |
| QX20 | CPNP 打印流程、方形/无边框/卡片照片、二维码解码 | APK 静态分析与网络模拟器；尚无真机验证 |

CP900/CP910/CP1200/CP1300 在这个版本的 App 中走另一条 Android 打印入口；本项目没有把它们标记为已支持。当前也不提供 USB、固件升级、自定义保护膜花纹或 App 的拼贴编辑器。

## 安装

需要 Go 1.23 或更新版本：

```sh
go install github.com/songquanpeng/selphy-cli/cmd/selphy@latest
```

私有仓库可先 `git clone`，再在仓库内执行：

```sh
make install
selphy --version
```

默认安装到 `$(go env GOPATH)/bin/selphy`（设置了 `GOBIN` 时沿用该目录）。运行时无需 Go 或其他工具。

## CP1500 快速开始

先在打印机上连接路由器，让电脑和打印机处于同一个局域网；也可以让电脑加入打印机显示的直接连接 Wi-Fi。

```sh
selphy discover --json
selphy info --host PRINTER_IP --json
selphy status --host PRINTER_IP
selphy config set --host PRINTER_IP --model CP1500
```

保存后不必每次传地址和型号。DHCP 地址变化时重新运行 `discover` 并更新配置；也可在路由器里固定打印机地址。支持系统能解析的主机名。

先离线检查实际发给打印机的 JPEG：

```sh
selphy print photo.jpg --dry-run --preview preview.jpg --json
selphy prepare photo.png --paper postcard --fit contain --output ready.jpg
```

打印一张照片：

```sh
selphy print photo.jpg
selphy print photo.jpg --paper postcard --area safe --fit contain --wait
```

`print` 每次提交一张，会消耗相纸和墨。`--dry-run` 完全离线，不建立打印连接。预览和 `prepare` 都拒绝覆盖现有文件。

- 默认 `--area safe --fit contain`：完整图片放入安全区，四周留白，避免撕边损失布局。
- `--area paper`：按撕边后的成品范围排版；`--area bleed --fit cover`：填满含出血的整幅图像，适合允许裁切的满版照片。
- `--margin-mm 1`：在所选区域内再增加约 1 mm 留白；`--fit cover` 会主动居中裁切照片，`contain` 保留整张图片。
- 自动处理 JPEG 的 EXIF 方向；可用 `--rotate 90` 额外顺时针旋转。
- 支持 JPEG、PNG、WebP；照片只在本机读取和处理。
- 输入 HEIC 可先用 macOS 自带工具转换：`sips -s format jpeg photo.heic --out photo.jpg`。
- 默认 JPEG 质量 95，可用 `--quality 1..100` 调整。暂不提供 ICC 色彩管理。
- 默认启用 CP1500 自动图像优化，表面效果为光面。
- 一张照片的整体期限默认三分钟：`--job-timeout 5m`。
- 失败后不会自动重新提交整个任务，避免重复出片；请先检查打印机和已经打印的照片。

CP 的 `submitted` 表示照片已交入打印队列；不等于实物已出纸。`--wait` 再通过 UDP 轮询空闲状态，返回 `idle-after-submission`，仍不能代替肉眼检查实物。QX 的 `completed` 表示设备返回 `END_PRINT`。

生成 RP-108 裁切/边框校准图（四边 0–8 mm 刻度、成品边界、安全区、50 mm 标尺和 20 mm 圆）：

```sh
selphy test-page --output test-page.png
selphy print test-page.png --area bleed --fit cover --dry-run --preview test-page.jpg
selphy print test-page.png --area bleed --fit cover --wait
```

## 相纸规格

尺寸来自 APK 的 `assets/paper.json` 中打印图像区域，包含设备需要的裁边区域；不直接用物理纸张毫米数换算。

RP-108 按[佳能规格](https://cam.start.canon/en/P001/manual/html/UG-07_Reference_0070.html)撕边前为 100×177 mm、撕边后为 100×148 mm。上下各约 14.5 mm 的进纸边不计入成品。APK 将 CP 明信片成品映射到整张 1248×1872 JPEG 内的 `(32,60,1184,1752)` 区域，另给出 `(55,84,1138,1704)` 的保证打印区域，约比成品边缘缩进 2 mm。这就是默认 `safe` 区域；实际偏移仍应使用校准图检查。

文字、二维码、细边框使用默认安全区；需要更宽保护边时增加 `--margin-mm`。`--dry-run --json` 输出 `content_rect`、`paper_rect` 和 `safe_rect`，坐标全部相对于实际发送的 JPEG。标尺毫米数按 APK 的 11.835 px/mm 标定，为近似参考。

| `--paper` | 适用机型 | JPEG 像素 |
| --- | --- | --- |
| `postcard` | CP1500，明信片 | 1248 × 1872 |
| `l` | CP1500，L | 1152 × 1472 |
| `card` | CP1500，卡片 | 1088 × 668 |
| `qx-square` | QX10、QX20，带边框方形 | 768 × 768 |
| `qx-square-borderless` | QX20，无边框方形 | 848 × 826 |
| `qx-card` | QX20，卡片 | 644 × 826 |

`auto` 默认按型号选择：CP1500 用 `postcard`，QX 用 `qx-square`；它不会自动切换成当前装入的纸张。打印前会检查机型、状态、JPEG 能力、尺寸范围及墨盒/纸盒，遇到不匹配会报错。

## 诊断和脚本

```sh
selphy doctor --json
selphy doctor --session --json
selphy config show --json
selphy papers --json
selphy agent-guide
selphy completion zsh
```

`discover` 默认向直接连接地址及本机接口的子网广播地址发送 NIC 信息查询。网络禁止广播时可指定地址：

```sh
selphy discover --target PRINTER_IP --duration 4s --json
selphy status --host PRINTER_IP --timeout 3s --json
```

Wi-Fi 来宾网络、AP 隔离或电脑的 VPN 可能阻止设备互访。macOS 首次询问局域网访问时，需要允许当前终端/宿主应用访问。打印机忙时先关闭手机 App 中的连接，等待现有任务结束。

全局参数：`--host`、`--model`、`--home`、`--timeout`、`--port`、`--json` / `--format json`。排版参数：`--area`、`--fit`、`--margin-mm`、`--rotate`、`--quality`。网络交换默认两秒，UDP 查询最多三次尝试；`--job-timeout` 约束整个打印任务。使用 Ctrl-C 会尝试取消当前任务并释放会话，已经进入物理打印的任务可能继续出纸。

`doctor --session` 会进一步完成会话协商，等待打印机报告就绪后释放会话，不发送打印任务。

配置优先级为：命令行参数 > `SELPHY_HOST` / `SELPHY_MODEL` > 配置文件。配置目录由 `--home` / `SELPHY_HOME` 指定，默认是系统用户配置目录下的 `selphy-cli/config.json`（macOS：`~/Library/Application Support/selphy-cli/config.json`）。文件只存默认地址和机型，不存 Wi-Fi 密码。

`--json` 时成功结果写 stdout，打印进度写 stderr；错误为 stderr 上的一条 `{"error":"..."}` JSON。退出码 0 为成功，1 为失败。打印期间 stderr 可以同时包含文本进度和最终 JSON 错误，脚本应以退出码和 stdout 的最终结构为准。

## QX 配对

先把二维码解码后的 `SPLQR:...` 文本存入本地文件，或从 stdin 输入：

```sh
selphy pair --file qr.txt
selphy pair --file qr.txt --show-password --json
```

默认隐藏密码，显式 `--show-password` 才输出。CLI 不保存二维码或密码；请在系统 Wi-Fi 设置中加入显示的网络，再使用 `--host 192.168.0.1 --model QX10` 或 `QX20`。`pair` 目前只解码 QX 文本，不解码图片、不自动切换电脑网络；CP1500 使用打印机屏幕的连接设置。

## 开发与协议分析

```sh
make test
make build
```

测试用真正的本地 UDP/TCP 模拟打印机验证会话、碎片化 TCP 读取、部分写入确认、重复请求、数据重试、JPEG 请求范围、错误取消和会话释放；另覆盖图片裁切、留白、安全区坐标、毫米留白、8 种 EXIF 方向、离线 dry-run 和配置权限。

协议布局、APK 哈希、分析证据和验证边界见 [docs/protocol.md](docs/protocol.md)。代码仅重实现互操作所需协议；Canon 的应用、商标、固件和素材属于原权利人。
