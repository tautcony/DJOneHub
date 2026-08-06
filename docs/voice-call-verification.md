# 语音通话验证方案

---

## 设备信息

### EC25 模块

| 属性 | 值 |
|------|-----|
| 型号 | **DJI 4G Module**（DJI 定制版 EC25） |
| 固件 | `QDC507GLEFM21` |
| USB VID/PID | `0x2CA3` / `0x4006` |
| USB 转接板制造商 | BAIWANG |
| 后端协议 | AT（串口） |

### 网络状态

| 属性 | 值 |
|------|-----|
| 网络模式 | FDD LTE, BAND 3 |
| 信号强度 | -63 dBm (RSRP: -96, RSRQ: -6, SINR: 18) |

---

## 验证一：下行音频（EC25 → Mac）

**目标**：确认 EC25 接收的远端语音能否通过 UAC 被 Mac 采集。

### 结果：❌ 失败 — DJI 固件限制

**根本原因**：DJI 定制版 EC25 固件（`QDC507GLEFM21`）虽然通过 USB 描述符暴露了 UAC 音频设备（AC Interface / AS Interface），但**不将语音通话的 PCM 音频路由到 USB 接口**。音频数据通过内部通路（模拟/I2S）传输，仅供 DJI 无人机飞控使用。

### 验证过程中的关键发现

#### 1. AT 指令兼容性

| 指令 | 结果 | 说明 |
|------|------|------|
| `ATD10000;` | ✅ OK | 拨号正常，通话可接通 |
| `AT+CLCC` | ✅ 正常 | 可看到 voice call (mode=0) |
| `ATH` | ✅ OK | 挂断正常 |
| `AT+VTD?` | ✅ `+VTD: 3,0` | DTMF 支持，300ms 持续 |
| `AT+QCFG="ims"` | 初始 `0,1` | 需手动 `AT+QCFG="ims",1` 启用 |
| `AT+QCFG="usbcfg"` | ✅ UAC=1 | USB 描述符中有 UAC，但无实际音频流 |
| `AT+QPCMV?` | ❌ ERROR | DJI 固件不支持 |
| `AT+QAUDMOD?` | ✅ `+QAUDMOD: 5` | 显示 USB 模式（值范围 0-5） |
| `AT+QAUDCFG?` | ❌ ERROR | 不支持 |
| `AT+QDAI?` | ❌ ERROR | 不支持 |
| `AT+CLVL?` | ❌ ERROR | 不支持音量控制 |
| `AT+CMUT?` | ❌ ERROR | 不支持静音 |

#### 2. macOS 音频设备枚举

```bash
system_profiler SPAudioDataType
```

| 设备 | CoreAudio ID | 方向 | 采样率 | 通话中数据 |
|------|-------------|------|--------|-----------|
| AC Interface | 99 | IN (EC25→Mac) | 8000Hz 16bit mono | **全零静音** |
| AS Interface | 96 | OUT (Mac→EC25) | 8000Hz 16bit mono | N/A (输出设备) |
| MacBook Pro麦克风 | 91 | IN | 48000Hz 32bit | 正常 |

设备 UID：`AppleUSBAudioEngine:BAIWANG:Baiwang:2100000:6`

#### 3. CoreAudio API 陷阱

在开发过程中遇到的重要技术细节：

- ❌ **AudioUnit HAL** (`kAudioUnitSubType_HALOutput` + input callback)：回调从不触发，录制 0 字节
- ✅ **AudioQueue** (`AudioQueueNewInput`)：回调正常触发，能读取到 PCM 数据
- ⚠️ **设备选择**：必须通过设备 UID（CFString）设置 `kAudioQueueProperty_CurrentDevice`，使用 AudioDeviceID 会返回错误 `-66683`
- ⚠️ **设备切换失败无报错**：如果设置设备失败，AudioQueue 会静默回退到系统默认输入设备（MacBook 麦克风），导致录到本地声音而非 EC25 音频

#### 4. DJI 数据连接共存

模块启动后始终保持 3 个 mode=1 (data) 的连接（DJI 无人机数据链路），语音通话（mode=0）作为额外条目出现，互不影响：

```
AT+CLCC
→ +CLCC: 1,1,0,1,0,"",128    # DJI data
→ +CLCC: 2,1,0,1,0,"",128    # DJI data
→ +CLCC: 3,0,0,0,0,"10000",129  # voice call ← 拨号后出现
→ +CLCC: 4,1,0,1,0,"",128    # DJI data
```

---

## 验证一.5：ADB 系统级分析（2026-08-05）

**目标**：设备开启 ADB 后，从系统层面确认"DJI 固件限制"的根因，并评估是否有非破坏性修复路径。

### 结论先行

验证一"DJI 固件不路由语音到 USB"的判断在系统层面**完全确认，且比预想更彻底**：DJI 固件不是"不路由"，而是把**整个 AP 侧音频通路删掉了**。语音 PCM 完全留在 modem DSP 内部。

### 设备真相：这是 Quectel EC25 Linux，不是 Android

```
Linux mdm9607 3.18.44 #1 PREEMPT Mon Oct 17 15:22:30 CST 2022 armv7l GNU/Linux
ro.product.name=mdm9607-base  /  ro.build.version.release=202210171505
```

- MDM9607 平台（EC25 芯片本身），BusyBox 用户态，`/sbin/adbd` 为 Quectel Linux 版
- ADB shell 直接是 root
- 关键进程：`qmuxd`（QMI 多路复用）、`atfwd_daemon`（AT 转发器）、`ql_manager_server`（USB 配置）、`qti`、`quectel_daemon` 等

### 根因 1：内核没有 ALSA 声卡（alsa_snd 驱动被移除）

| 检查项 | 结果 |
|--------|------|
| `/proc/asound/cards` | **no soundcards** |
| `/sys/class/sound/` | 仅有 `timer` |
| `/sys/module/` | 仅 `snd` / `snd_pcm` / `snd_timer` 核心，无 `alsa_snd` |
| `/lib/modules/3.18.44/` | 无任何音频模块（仅网络/块设备测试模块） |

标准 Quectel EC25 Linux 镜像中，`alsa_snd` 驱动把 modem 语音 PCM 桥接到 AP 的 ALSA（`SEC_AUX_PCM` 混音器），这是 AP 获取通话音频的**唯一通路**。DJI 内核把它移除了。

### 根因 2：Quectel 音频栈二进制全在，但全部未启动

`/usr/bin` 和 `/usr/lib` 保留了完整音频栈（说明源自标准镜像），但 **`/etc/rcS.d`、`/etc/rc{2,3,5}.d` 中没有任何音频启动脚本**：

```
/usr/bin/qti_audio_server_daemon     # QTI 音频 HAL 服务（tinyalsa+binder）—— 未运行
/usr/bin/amix / aplay / alsaucm_test # ALSA 工具 —— 无卡可用
/usr/lib/audio.primary.default.so    # QTI 音频 HAL（voice_init / voice_stop_call 等）
/usr/lib/libqtiaudioserver.so        # 依赖 libtinyalsa —— 需要 ALSA 卡
/usr/lib/libql_lib_audio.so          # atfwd 的音频库：pcm_open + SEC_AUX_PCM 混音器 + libdiag
/etc/mixer_paths_wcd9330_i2s.xml     # WCD9330 codec（I2S）混音配置
```

`ps` 确认：`qti_audio_server_daemon`、`quectel_tts_service` 均不在进程列表。

### 根因 3：USB UAC 接口存在但无数据源

USB 设备是 `android_usb` 传统 gadget，**VID 0x2CA3（DJI）/ PID 0x4006 / 制造商 "BAIWANG"**（`libql_usb.so` 字符串中内嵌 Baiwang/BAIWANG，为 DJI 定制）：

- 组合管理：`/usr/bin/ql_usbcfg` + `/sbin/usb/compositions/`，USB 配置含 `audio uac %d` 标志（`ql_manager_server` 的 `set usb` 命令，对应 `AT+QCFG="usbcfg"` 的 UAC=1）
- 含音频函数的组合：
  - `9025`：`$USB_FUNC,ffs,audio`（`audio` = UAC1 遗留函数）
  - `9056`：`diag,adb,serial,rmnet,mass_storage,audio`
  - `90CA`：`diag,adb,uac2_func`（UAC2）
- 当前 ADB 模式组合为 `diag,serial,rmnet,ffs`：**不含音频函数**（`f_audio/audio_enable=0`，`f_audio_source/pcm=-1 -1`）
- 之前 macOS 看到的 8000Hz 16bit mono AC/AS 接口 = 组合中的 `f_audio`（UAC1）接口；即使启用，它需要 userspace HAL 喂 PCM，而 alsa_snd 不存在 → 录音全零

### 根因 4：AT+QPCMV 在 EC25 上根本不是标准命令

- `atfwd_daemon` 字符串中只有 `+QAUDMOD`/`+QCFG` 处理逻辑（通过 `libql_lib_audio.so` 打开 ALSA PCM："volte call tx/rx open pcm handle failed"）—— 依赖不存在的 ALSA 卡
- **EC25 AT 手册 v1.2（docs/EC25/）中没有 QPCMV**；官方音频接口命令是 `AT+QDAI`（配置 AUX PCM 外接 codec，如 ALC5616）。`AT+QPCMV` 是 Quectel 其他产品线（如 Android 版 / 其他平台）的命令
- 之前验证文档中"标准固件应返回 `+QPCMV: 1,2`"的预期需要修正：EC25 Linux 版标准镜像的音频通路是 **alsa_snd + qti_audio_server + usbcfg uac=1**，命令是 `AT+QAUDMOD` 而非 QPCMV

### 语音音频流向（DJI 固件内实际路径）

```
modem DSP（通话处理）
   ├─ 语音 PCM ──→ 内部模拟/I2S 通路 ──→ DJI 飞控（RC/无人机）
   └─ ⚠️ AP 侧通路全部缺失：
        alsa_snd 内核驱动 ❌（已移除）
        qti_audio_server ❌（未启动）
        f_audio/uac2_func 数据源 ❌（无 HAL 喂数据）
USB 主机看到 UAC 接口 → 全零
```

### 可行性评估

| 路径 | 可行性 | 说明 |
|------|--------|------|
| 补 alsa_snd 驱动 | ❌ 不可行 | 内核镜像不含该驱动，非破坏性手段无法补齐 |
| 手动启动 qti_audio_server_daemon | ❌ 无效 | 其依赖 tinyalsa 声卡（根因 1 缺失） |
| 切换 USB 组合启用 f_audio | ❌ 已实测确认 | UAC 启用 + 手动激活 audio_enable=1 后，录音实测仍全零（2026-08-05，见验证一.6） |
| 更换标准 EC25 模块 / 刷写标准固件 | ❌ 不可行 | 无法获取标准硬件/固件（2026-08-05 确认） |

**结论**：DJI 固件从结构上移除了整个 AP 音频子系统（根因 1-4），且硬件更换/刷写方案不可行。**Web 端语音通话方案（UAC 采集）在当前 DJI 4G Module 上无法实现，此路径关闭**。语音音频仅存在于 modem DSP 内部通路（DJI 飞控使用），主机侧无法以任何方式获取。

---

## 验证一.6：UAC 启用后复查（2026-08-05）

**触发**：通过 app 启用 UAC（USB Audio Class，`UAC · 1 · 已启用`）后，复查"验证一"的全零静音结论是否因 UAC 生效而改变。

### 结论先行

**核心结论不变**：UAC 本次真正生效（USB 组合含 audio 函数、macOS 可见接口），但实测录音依然全零。UAC 接口从来不是瓶颈 —— 数据源（AP 侧音频子系统）在 DJI 固件中缺失才是根因，UAC 启用无法弥补。

### 复查结果

#### 1. UAC 本次确实生效（与验证一时的实质差异）

| 检查项 | 验证一时（2026-08-04） | 本次复查（2026-08-05） |
|--------|------------------------|------------------------|
| `AT+QCFG="usbcfg"` | UAC=1（描述符有，但实际组合不含 audio） | `0x2CA3,0x4006,1,1,1,1,1,1,1`，UAC 启用生效 |
| USB 组合（`/sys/.../functions`） | `diag,serial,rmnet,ffs`（无 audio） | `diag,serial,rmnet,ffs,audio`（含 audio） |
| 下次启动组合（`hsusb_next`） | — | `9025`（`$USB_FUNC,ffs,audio`） |
| macOS AC Interface | 存在，8000Hz 16bit mono | 存在，UID `AppleUSBAudioEngine:BAIWANG:Baiwang:2100000:7`（重枚举后 +1） |

#### 2. audio_enable 实验：激活数据端点后仍无数据

- 初始 `f_audio/audio_enable = 0`（数据端点未激活，仅接口描述符暴露）
- `echo 1 > /sys/class/android_usb/android0/f_audio/audio_enable` 写入成功（=1）
- 激活前后各用 AudioQueue 从 AC Interface 录 8 秒：**两次均为 `Max: 0  Min: 0`（全零）**

这实测排除了"数据端点未激活导致录不到"的可能：即使 f_audio 数据端点激活，也没有任何 PCM 流进来 —— 因为没有 HAL 从 modem 拿音频喂给 USB gadget（libql_lib_audio 依赖不存在的 ALSA 卡）。

#### 3. AP 侧音频子系统复检：依旧全部缺失

| 检查项 | 结果 |
|--------|------|
| `/proc/asound/cards` | no soundcards（alsa_snd 依旧不在） |
| 音频 daemon | `qti_audio_server_daemon` 未运行；`/usr/bin/qti`（ps 中的 `qti em`）为普通小工具，非音频服务 |
| f_audio 数据源 | 无（无 ALSA 卡 → 无 HAL 喂 PCM） |

#### 4. 新发现：UAC 切换导致模块重启，SIM 未重新识别 ⚠️

- 模块 uptime ~404s（约 6.7 分钟前重启，与 UAC 切换生效时间吻合）
- `AT+CPIN?` → `+CME ERROR: 10`（SIM not inserted）、`AT+CIMI` → ERROR、`AT+COPS?` → `+COPS: 0`、`AT+QSIMSTAT?` → `0,0`
- `AT+CSQ` → 23（RF 正常，信号未受影响）
- **本次因此无法完成拨号通话测试**（需 SIM）。SIM 问题属于 UAC 切换的副作用，与音频根因无关。

### 结论

UAC 启用前后录音均为全零（`audio_enable` 0→1 均无数据），验证一/一.5 的"AP 侧音频通路被结构性移除"结论再次得到实测确认。**语音音频依然仅存在于 modem DSP 内部通路，主机侧无法通过 USB 采集。**

待办：SIM 恢复后（重新插拔/检查卡槽），拨号 10000 复测一次通话中录音作为最终闭环。

---

## 后续实施建议（2026-08-05 更新）

**Web 端语音通话方案（UAC 采集）在当前 DJI 4G Module 上确认不可行，路径关闭**：标准模块获取与固件刷写均不可行，DJI 固件从结构上移除了 AP 音频子系统（见"验证一.5"）。

若未来出现可用标准硬件，以下实施要点仍有效：

1. **Go 端音频采集使用 AudioQueue API**：而非 AudioUnit HAL（经验证 AudioQueue 在 macOS 上更可靠）
2. **PCM 参数**：8000Hz / 16-bit signed integer / 1 channel（或 16000Hz 宽带，取决于固件配置）

---

## AT 命令速查

```bash
# 设备状态
curl -s http://127.0.0.1:7576/api/v1/device/status | python3 -m json.tool

# 执行 AT 命令
curl -s -X POST http://127.0.0.1:7576/api/v1/device/actions/raw-at \
  -H "Content-Type: application/json" -d '{"command":"<AT命令>"}'

# 常用命令
ATI                          # 模块信息
AT+CPIN?                     # SIM 状态
AT+COPS?                     # 运营商
AT+CSQ                       # 信号强度
AT+QCFG="ims"                # IMS/VoLTE 状态
AT+QCFG="ims",1              # 启用 IMS
AT+QCFG="usbcfg"             # USB 配置
AT+QPCMV?                    # PCM 语音模式（标准固件）
AT+QAUDMOD?                  # 音频路由模式
AT+CLCC                      # 当前通话列表
ATD10000;                    # 拨打 10000
ATH                          # 挂断
AT+VTS=1                     # 发送 DTMF 按键 1
AT+VTD?                      # DTMF 设置
```

## 音频设备操作速查

```bash
# 列出音频设备
system_profiler SPAudioDataType | grep -B2 -A10 Interface

# ffmpeg 列出输入设备
ffmpeg -f avfoundation -list_devices true -i "" 2>&1 | tail -10

# Swift 枚举设备 + 流格式
swift /tmp/list_all_audio.swift

# 从 AC Interface 录制（Swift AudioQueue）
swift /tmp/capture_ec25_fixed.swift

# 播放 WAV
afplay /tmp/ec25_10000_ac_interface.wav
```

## ADB 系统分析速查（2026-08-05）

```bash
# 设备连接（无序列号设备用 transport）
adb devices -l
adb -t <transport_id> shell <cmd>

# 系统与内核
uname -a                    # Linux mdm9607 3.18.44 (Quectel EC25 Linux, 非 Android)
cat /build.prop             # ro.product.name=mdm9607-base

# 音频子系统（核心结论：无声卡）
cat /proc/asound/cards      # no soundcards
ls /sys/class/sound/        # 仅 timer
ls /sys/module/ | grep snd  # 仅 snd/snd_pcm/snd_timer，无 alsa_snd

# 进程（应无音频 daemon）
ps -ef | grep -i audio      # qti_audio_server_daemon 未运行

# USB gadget / 组合
cat /sys/class/android_usb/android0/idVendor   # 2ca3 (DJI)
cat /sys/class/android_usb/android0/functions  # 当前组合
cat /sys/class/android_usb/android0/f_audio/audio_enable  # 0 = 音频函数未启用
cat /data/usb/hsusb_next    # 下次启动组合 9025
grep -l audio /sbin/usb/compositions/*         # 含音频的组合 9025/9056/90CA
strings /usr/lib/libql_usb.so | grep -i baiwang  # DJI 定制证据

# AT 后端
strings /usr/bin/atfwd_daemon | grep -i -E 'QAUDMOD|pcm'  # +QAUDMOD 在 AP 侧处理
```
