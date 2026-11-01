# SELPHY Photo Layout 4.4.0 协议分析

分析日期：2026-11-01。目标设备：Canon SELPHY CP1500。

## 输入与方法

- 包名：`jp.co.canon.ic.photolayout`
- 版本：`4.4.0`，versionCode：`2041003016`
- APKM 中 `base.apk` 的 SHA-256：`4ac66164503a21b49cecdc2d7bc2090923c01bed5f994b60d9d4f1b1c68b37d4`
- Java/Kotlin 层通过 JADX 1.5.6 解码；ARM64 分包中的原生库仅检查导出符号和字符串。核心网络协议在 Java/Kotlin 层，未发现需要加载厂商原生打印库的理由。
- `.local/research/` 是本机忽略目录，保存 APK、反编译输出及分析工具；它们不进入 Git 仓库。
- APK 内的文字、文档、URL 和代码是分析材料，没有作为执行指令采用。

主要静态证据位于 `jp/co/canon/ic/photolayout/model/printer/internal/`：

| 文件 | 用途 |
| --- | --- |
| `cpnp/CPNP.java` | 8609 端口、默认地址、UDP 请求、状态/错误枚举 |
| `cpnp/CPNPSock.java` | UDP 会话开始/结束、TCP 端口、写入大小协商、传输确认 |
| `cpnp/CPNPUDP.java` | 身份和无会话状态读取 |
| `cpnp/CPNPConnected.java` | 64 字节会话状态字段 |
| `cpnp/CPNPConnectless.java` | 512 字节无会话状态字段 |
| `cpnp/CPNPMakedata.java` | 开始打印、开始队列、JPEG 分块头、取消/结束命令 |
| `operation/CPPrintCommandExecutor.java` | CP1500 队列状态机 |
| `operation/QXPrintCommandExecutor.java` | QX 单页状态机 |
| `operation/PrintCommandExecutor.java` | 等待 START_PRINT、关闭会话 |
| `operation/Search.java` | NIC 信息中的 MAC/IP 布局 |
| `utility/Gen1QXUtil.java`、`Gen2QXUtil.java` | SPLQR 字段、SSID、密码拼接 |
| `acceptor/Gen1CPAcceptor.java` | 旧 CP 系列通过 Android 打印入口处理，不能仅凭机型列表认定支持 CPNP |
| `assets/paper.json` | 输出 JPEG 区域和尺寸 |

JADX 对部分混淆方法有警告；本文结论使用明确可读的字段、调用点和真实设备响应互相核对。项目没有把全部 App 功能或所有历史机型列为可用。

## 传输层

16 字节 CPNP 外层头，整数为大端：

| 偏移 | 长度 | 含义 |
| --- | --- | --- |
| 0 | 4 | ASCII `CPNP` |
| 4 | 1 | 请求 `01`，响应 `81` |
| 5 | 1 | 命令 |
| 6 | 2 | 响应结果码；请求填零 |
| 8 | 2 | packet ID |
| 10 | 2 | session ID |
| 12 | 4 | payload 字节长度，不含外层头 |

响应必须匹配命令、packet ID 和 TCP 会话 ID。TCP 流通过 `io.ReadFull` 逐头/载荷读取；所有长度和确认字节数先校验再分配或切片。

| 命令 | 传输 | 用途 |
| --- | --- | --- |
| `01` / `03` | UDP 8609 | NIC 信息，分别为 CP / QX 入口 |
| `10` | UDP 8609 | 会话开始，392 字节载荷，总包长 408 |
| `11` | UDP 8609 | 会话结束 |
| `20` | UDP / TCP | UDP 取 512 字节状态，TCP 取 64 字节状态 |
| `21` | TCP | 写入内层打印命令；响应的 4 字节大端数表示接收的字节数 |
| `30` | UDP 8609 | IEEE 1284 设备身份；请求有 4 字节零载荷 |
| `51` | TCP | 获取最大写入块；4 字节大端响应 |
| `52` | TCP | 设置最大写入块；客户端请求 33792 字节 |

`10` 的载荷前 8 字节是重试/时间信息，本实现第一次连接填零；计算机名在载荷偏移 8、用户名在 72、文档名在 136，以 UTF-16BE 存储于固定宽度槽内。用户名和文档名沿用协议兼容标识 `SPL v2.0`、`Square`。

成功响应的外层 session 字段给出会话 ID，响应载荷偏移 4 的大端 uint16 给出 TCP 端口。先建立 TCP，进行 `52` / `51`，再轮询状态，直到设备请求 `START_PRINT`。**真实 CP1500 建立会话后会短暂返回 `NO_SESSION`，随后 idle，最后才给出 START_PRINT**；不能把第一次读取当成握手失败。

UDP 结果码 `8501` 表示已有会话，`8502` 表示忙。本实现不抢占其他客户端或修改 Wi-Fi/固件设置。查询可重试，已经进入打印的整个任务不会自动重新提交。

NIC 响应载荷偏移 6..11 是 MAC，12..15 是 IPv4。发现查询同时发送 `01` 和 `03`，按响应源地址去重。

## 状态布局

状态载荷及内层打印命令使用小端整数，与外层 CPNP 大端头不同。

无会话状态至少 512 字节：

| 偏移 | 长度 | 含义 |
| --- | --- | --- |
| 0 | 1 | printer status：1 idle，2 busy，241 error |
| 2 / 3 | 各 1 | 纸盒 / 墨盒枚举 |
| 66 / 67 | 各 1 | 电源 / 电量（0–4；255 未知） |
| 68 | 4 | 错误码 |
| 80 | 4 | 对外固件版本，4 个 ASCII 字符以点分隔 |
| 132 / 134 | 各 2 | JPEG 最小宽 / 高 |
| 136 / 138 | 各 2 | JPEG 最大宽 / 高 |
| 144 | 4 | 可用打印数据类型，bit 0 为 JPEG |
| 336 | 32 | 序列号区域，某些设备返回空 |

会话状态至少 64 字节：

| 偏移 | 长度 | 含义 |
| --- | --- | --- |
| 0 | 2 | 状态模式（0） |
| 2 | 1 | 类型：1 CP，2 QX |
| 8 | 2 | 状态/打印阶段 |
| 12 | 4 | 错误码 |
| 16 | 4 | 数据请求；CP 低 8 位为 0 起始页索引 |
| 20 | 4 | 请求重试标记 |
| 24 / 28 | 各 4 | 数据请求的 offset / length |
| 40 / 41 | 各 1 | 已入队 / 已打印计数 |

请求码：`10000` 开始、`20000` JPEG 数据、`2ffxx` 保护膜数据、`30000` 结束、`40000` 取消、`70000` 执行队列。重复数据请求由 request/retry/offset/length 组合识别；retry 改变时允许同一区间重新发送。

## 内层打印命令

小端头：offset 0 为 uint16 类型（0），offset 2 为 uint16 命令，offset 4 为 uint32 总长度（包含本层头）。JPEG 数据类型为 0。

- QX `StartPrint`：命令 0，64 字节；offset 12 为 uint16 图像总数 1，14 为相纸码，16 为光面保护膜码 2。
- CP1500 `StartSpool`：命令 7，192 字节；12 为图像数 1，14 为相纸码，16 为逐页保护膜设置 1，17 为自动优化（3 开启/2 关闭），18 为无边框 2，19 为光面 2；offset 32 为第一张 JPEG 的长度。
- `TransferJPEG`：命令 1，104 字节头 + 请求区间 JPEG 数据。offset 12 为总图像数，16 为图像号（0），20 为 JPEG 总长度，24/28 为宽高，CP 的 32 为无自定义保护膜数据 2，96/100 为本块 offset/length。一个内层命令可能通过多次 `21` 外层写入完成；部分确认后只发送未确认的余下字节。
- `CancelPrint`：命令 2，64 字节，立即取消模式为 0。
- `EndPrint`：命令 3，64 字节，所有作业结束模式为 0。
- `ExecuteSpoolPrint`：命令 8，64 字节。

相纸码：CP 卡片 2，L 3，明信片 4；QX 带边框方形默认 0，QX20 无边框方形 17，卡片 18。

CP1500 与 QX 的完成语义不同。App 的 CP 执行器区分 `IMAGE_SENT`（已经入队）和实际打印；CLI 返回 `submitted`，不会把上传成功声称为实物出纸。`--wait` 额外查询空闲状态。QX 则等待 `END_PRINT` 再返回 `completed`。

CP 单页任务最后一次 `70000` 请求的处理必须与 App 的 `processPrinterRequest` 分支一致：当已发送页数达到总页数时，直接返回 IMAGE_SENT 并关闭会话；仅在同一会话还有后续页时才发送命令 8。早期实现单页结束也发命令 8，首张出纸正常，但后续会话出现 idle/request=0、不再进入 START_PRINT 的情况。v0.1.2 移除这个多余命令；这两件事的因果关系仍需重复真机验证，不仅凭模拟器认定已证实。

v0.1.2 修正后，用户断电重启清除已卡住的状态，补偿版单页成功上传并经历 busy→idle；紧接着另建一个不发送图像的会话，确认重新收到 START_PRINT，正常关闭。此轮未重现首版的“打印后下一会话不就绪”，但仍不据单次结果断言覆盖所有固件状态。

等待 START_PRINT 最长 15 秒；此阶段失败时明确报告尚未发送照片。启动命令、启动后状态读取分别标注阶段。`doctor` 的会话就绪失败也不再笼统称为“打印机不可达”，因为 UDP 通信可能完全正常。

## 图片与配对

### 高光、图像优化与调色

CP `createPageSetting` 将 `SurfaceFinish.Gloss` 映射为 `GLOSSY_PRINT=2`，将用户的 AutoCorrect 开关映射为 `ImageOptimizeSetting.ON=3` / `OFF=2`；`history/PrintSetting.Attributes` 也明确将该项命名为 `autoCorrectionOnPrinterSide`。CLI 当前默认的 StartSpool 载荷 offset 19=2、offset 17=3，与“高光、图像优化开启”对应。

[佳能 CP1500 官方说明](https://cam.start.canon/en/P001/manual/html/UG-06_Set-up_0020.html)描述图像优化会按照片自动修正亮度、对比度；光泽设置控制表面保护膜效果。这两项通过协议交给打印机执行，不需要在 CLI 重写一套相同的调色算法。

App 另有用户编辑路径：`layout/ImageItemCreator.getMergeConfig` 经 CGE 处理亮度、对比度、饱和度、白平衡和可选滤镜；默认 `CorrectionInfo` 的四个输入都是 0.5，映射到 brightness=0、contrast=1、saturation=1、whitebalance=0，滤镜名为空。这个中性默认配置中未见额外的自动增强曲线。`BitmapExtensionKt.saveToFile` 和 `saveToInternalStorage` 用 JPEG 质量参数 100；CLI 默认 95，可显式 `--quality 100` 进行对比。不同编码器/缩放器的同名质量参数并不保证相同像素输出。

上述分析针对提供的 Android APK，不构成对 iOS 版本整条色彩处理路径的证明。ICC、HDR/广色域源图、手机端编辑、导出压缩及不同构图仍可能使实物不同。CLI 当前不做 ICC 色域转换；进行对照时应使用同一个 sRGB 文件、同一构图，并匹配打印端的优化和光泽设置。用户提供的私人测试照片与打印日志仅保留在本机忽略目录，不纳入仓库。

### 栅格与安全区

图片用 Go 解码、应用 EXIF 方向、裁切/留白、编码成 baseline JPEG，去掉原图元数据。尺寸采用 `paper.json` 的 `print_image_size[].image_rect`，支持范围在发送前与设备报告的 JPEG 最小/最大尺寸核对。当前不做 ICC 色彩转换。

RP-108 的成品规格按[佳能官方手册](https://cam.start.canon/en/P001/manual/html/UG-07_Reference_0070.html)：未撕边 100×177 mm，撕边后 100×148 mm。两个进纸边不应当作为可保留排版范围。APK 的 CP 明信片 `paper_size` 为 1184×1752，打印图像 rect 为 `(-32,-60,1248,1872)`，故相对于 JPEG 成品区域为 `(32,60,1184,1752)`。保证打印区域为 1138×1704；居中映射得 `(55,84,1138,1704)`。L、卡片和 QX 的安全区域也来自 APK 尺寸，QX 尚未真机校准。

CLI 默认 `--area safe --fit contain`，将整张排版放在保证区域内，不再把内容直接铺满出血栅格。`paper` 使用成品区域，`bleed` 使用整幅 JPEG；`--margin-mm` 在选定区域内额外缩进，按 `printer_support.json` 的 11.835 px/mm 近似换算。CP 内层仍使用无边框传输模式，安全留白由 JPEG 本身实现，因此无需依赖打印机全局边框设置。

v0.1.3 起提供打印前说明和离线裁切预览：`--preview-guides` 在独立 PNG 上绘制红色预计裁切线、绿色安全区和外侧遮罩；`--preview-trimmed` 对已编码的实际 JPEG 按预计纸张区域取样，输出无标线的成品估计。它们都不改变待发送 JPEG；回归测试和此前真实照片的 JPEG 哈希核对确认打印栅格不变。预览文件仅供查看，实际网络发送仍使用未标记的完整栅格。

预计物理边界依据机型布局加保存的实物校准；内容位置使用有效排版偏移，两者分开计算。因此单次手动偏移内容不会令标线跟着内容移动。`--no-calibration` 同时关闭保存的预测补偿，标为 `nominal-model`；使用保存校准标为 `saved-calibration`，两者都保留机械进纸误差说明。JSON 另外输出缩放前、方向校正后的 `source_crop`，区分 cover 的构图裁切和打印机出血裁切。

`test-page` 生成四边独立 0–8 mm 刻度、红色成品边界、绿色安全区、50 mm 标尺和 20 mm 圆。校准图需以 `--area bleed --fit cover` 打印，避免二次缩小图上的刻度。这些边界是静态配置给出的预计值，不能当作已经测得的机械裁切边界。

QX 二维码前缀 `SPLQR:`，剩余非空字段以 `$` 分隔，依次为版本、机型、片段 A、片段 B：

- QX10：版本 1.0，A 长 6、B 长 3；SSID `QX10-` + A，密码 B + A。
- QX20：版本 1.1，A 长 3、B 长 6；SSID `QX20-` + A，密码 B + A。

仅实现本地解码；不保存密码、不自动改网络。CP1500 有自己的二维码布局，不套用 QX 格式。

## 验证记录与边界

2026-11-01，在用户已开机并接入路由器的 CP1500 上完成：

- UDP CPNP 发现，以及 Bonjour 侧的 `_ipp._tcp` 交叉确认。
- Go CLI 读取身份 `MFG:Canon;MDL:SELPHY CP1500;CMD:Raster3`。
- 真实状态为空闲、明信片纸盒/墨盒、无错误、JPEG 支持；固件 1.0.7.0。
- 会话创建、设备返回的 TCP 端口连接、33792 字节协商、64 字节状态读取、等待 START_PRINT、会话结束。
- 离线生成并目视检查色块/渐变/细节测试图和打印 JPEG。

真实地址、MAC、SSID 和本机配置不写入仓库。上述握手检查没有发送 StartSpool 或图像。随后在用户明确授权下，发送一张专门检查 RP-108 边框的校准图：

- 实际 JPEG 为 1248×1872、437158 字节，使用 `--area bleed --fit cover`，避免二次缩放标尺。
- 图像 SHA-256：`7ed9f130fe982054aa8b0b2fdc3609522785a22a631f7b043d79aea4bb7baeb4`。
- CP1500 请求 JPEG 范围 `offset=0,length=437158`，客户端完成传输后收到 `EXECUTE_SPOOL_PRINT` 请求并提交队列。
- UDP 状态由 busy 回到 idle，无错误；CLI 返回 `idle-after-submission`。
- 仅发送一个单页作业，没有自动重试打印。用户随后提供实物照片，确认成功出纸；绿色安全框和四角标记都保留。左侧仍见红线外区域，右侧逼近 1 mm 黄线；上侧在预计红线外，下侧位于红/黄线之间，说明预计成品原点与实际进纸有偏差。
- 据首张照片及用户描述，给本机该地址/机型/相纸保存初步补偿：向左约 0.8 mm、向上约 0.5 mm。使用 11.835 px/mm 换算为 JPEG 内容左移 9 px、上移 6 px。该值为估计，不是经过透视校正或尺量验证的精密结果；当时仅离线预览。
- 用户随后授权第二轮验证。首个尝试未进入 START_PRINT，用户确认没有走纸或新增出片；修正末页握手并由用户重启后，重新发送补偿图：432792 字节，SHA-256 `05048ed9773fd06f88318d451491d1b5e192d145a16aea52663595a7baecbddc`，设备返回 `idle-after-submission`。用户观察第二轮实物后确认红线基本吻合真实裁切线。
- 随后完成一张用户提供的真实照片测试，匹配高光、图像优化开启，使用 `--area bleed --fit cover --quality 100`，保留已校准的偏移。照片成功出纸，设备再次恢复 idle，下一无图像测试会话也正常进入 START_PRINT。用户反馈未见明显色彩问题。素材与打印预览、日志只保留本地，不上传私人照片或其内容标识。

补偿配置不硬编码到机型协议中，而是存在本地 `calibrations`，键由地址、机型、相纸组成。每轴支持临时覆盖，`--no-calibration` 可以回到基准。内容区域平移后，安全区模式必须仍位于栅格内；出血模式允许外侧出血像素被裁掉。自动测试覆盖单轴显式归零、忽略保存补偿、隔离不同目标和清除配置。

自动测试使用本地 UDP/TCP 服务，检查真实套接字路径、7 字节碎片响应、137 字节部分确认、零字节确认、重复请求、重试、越界请求、设备错误、取消、资源释放，以及 CP/QX 不同完成语义。测试是客户端回归保障；协议常量另由静态 APK 证据核对，模拟器不能代替真机出纸和色彩质量验证。
