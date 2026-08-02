# DJOneHub 精简源码结构

这份目录描述 DJOneHub 的当前源码结构。Vue 前端位于 `web/`，由新的 Go 服务提供 API 和静态页面。

## 目录树

```text
DJOneHub-source-minimal/
├── cmd/
│   └── djonehub/             # 唯一跨平台服务入口和 Vue 页面托管
├── internal/
│   ├── apduarbiter/          # SIM/eUICC APDU 通道并发协调
│   ├── backend/              # AT、MBIM、QMI 后端的统一能力接口
│   ├── config/               # 运行配置与设备配置
│   ├── esim/                 # eUICC/Profile 读取、下载、切换和删除
│   ├── modem/                # 调制解调器发现、AT 指令和状态解析
│   └── simaid/               # SIM 应用 AID 发现与选择
├── pkg/
│   ├── logger/               # 日志适配
│   ├── mbim/                 # MBIM 协议实现
│   └── smscodec/             # SMS PDU 编解码与长短信重组
├── packaging/
│   ├── djonehub              # 终端 start/stop/status/logs/open 启动器
│   ├── install               # /usr/local 安装脚本
│   ├── README.md             # 发行包内的安装说明
│   └── THIRD_PARTY_NOTICES.md
├── scripts/
│   ├── build-macos.sh        # 本地开发构建
│   └── package-macos-arm64.sh# Apple Silicon 发行包构建
├── third_party/              # 当前构建实际使用的本地第三方源码
├── go.mod
├── go.sum
├── LICENSE
├── THIRD_PARTY_NOTICES.md
├── README.md
└── MACOS.md
```

## 关键入口

- `cmd/djonehub/main.go`：唯一 HTTP 服务入口，托管 API 和 Vue 页面。
- `internal/platform/darwin/`：macOS 上通过 libusb 接管大疆模块 USB AT 接口，并提供平台适配。
- `internal/application/` 与 `internal/backend/`：承载设备状态、短信、eSIM、网络和 AT 能力。
- `web/`：Vue/Vite 管理前端；macOS 管理入口不再嵌入独立的原生页面。

## 为什么仍有 internal、pkg 和 third_party

Go 以“包”为编译边界。所有平台都从 `cmd/djonehub` 启动，macOS 差异只位于 `internal/platform/darwin`，业务能力依赖共享的 application、backend、modem 和协议包。

`third_party` 中只保留当前依赖图实际使用的本地替换模块。保留本地副本可以确保当前修改版协议实现与已验证发行包一致，同时保留各上游组件的许可证和来源信息。

## 已排除内容

- `node_modules/` 依赖缓存
- 原 Linux 服务端入口、容器配置和网络命名空间工具
- Telegram、飞书、QQ 等机器人与转发功能
- 原项目未被 macOS 入口引用的 API、任务、数据库和后台页面
- `dist/`、下载包、日志、缓存及其他生成文件

## 验证方式

运行全部保留包的测试：

```sh
go test -mod=mod ./...
```

生成 Apple Silicon 发行包：

```sh
./scripts/package-macos-arm64.sh v0.1.0-preview
```

构建脚本会从 libusb 官方 Release 下载源码、核对 SHA-256，并将编译后的动态库与 DJOneHub 一起打包。

## 注意

当前 Go module 路径仍为 `github.com/iniwex5/vohive`，这是为了保持现有共享包导入路径及上游来源关系不变。确定最终 GitHub 仓库地址后，可以再进行一次独立的模块路径迁移，但这不是构建和发布 DJOneHub 的前置条件。
