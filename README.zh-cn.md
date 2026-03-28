# gomoqt

<div align="center">
<sup align="center"><a href="README.md">English</a></sup>
</div>

在 Go 语言中实现 Media over QUIC (MOQ),遵循 MOQ Lite 规范,通过 QUIC 协议高效地传输和分发媒体内容。

[![Go](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml)
[![Lint](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml)
[![moq-web CI](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/OkutaniDaichi0106/gomoqt.svg)](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
[![codecov](https://codecov.io/gh/OkutaniDaichi0106/gomoqt/branch/main/graph/badge.svg?token=4LZCD3FEU3)](https://codecov.io/gh/OkutaniDaichi0106/gomoqt)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/okdaichi/gomoqt)

## 目录
- [概述](#概述)
- [快速开始](#快速开始)
- [特性](#特性)
- [组件](#组件)
- [示例](#示例)
- [文档](#文档)
- [规范合规性](#规范合规性)
- [开发](#开发)
- [贡献](#贡献)
- [许可证](#许可证)
- [致谢](#致谢)

## 概述
本实现遵循 [MOQ Lite 规范](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html),可以利用 QUIC 构建实时媒体流应用的通信基础。

## 快速开始
```bash
# 安装 Mage (需要 Go 1.25+)
go install github.com/magefile/mage@latest

# 在 Docker 容器中运行 interop 测试（TypeScript 客户端 + 服务器）
mage interop:ts
# 或运行 Go 客户端
mage interop:go
```

## 特性
- **MOQ Lite 协议** — MoQ 规范的轻量级版本
  - **低延迟播放** — 将从数据发现、传输到播放的整体延迟降至最低
  - **连续播放** — 通过独立的数据收发设计,提高系统对网络波动的适应能力
  - **网络环境优化** — 能根据实际网络条件对运行方式进行最优调整
  - **轨道管理** — 通过 Publisher/Subscriber 模式进行轨道数据的收发
  - **高效复用分发** — 通过 Track Announce 与 Subscribe 实现高效的多路复用
  - **Web 支持** — 使用 WebTransport 支持浏览器
  - **原生 QUIC 支持** — 通过 `quic` 包装器提供原生 QUIC 支持
- **灵活的依赖设计** — 将 QUIC、WebTransport 等依赖进行解耦,可按需启用或替换
- **示例与互通测试** — 在 `examples/` 与 `cmd/interop` 中提供示例及互操作性测试集(broadcast、echo、relay、native_quic、interop 服务器/客户端)

### 另请参阅
- [moqt/](moqt/) — 核心包(帧、会话、轨道复用)
- [quic/](quic/) — QUIC 包装器和 `examples/native_quic`
- [webtransport/](webtransport/)、[webtransport/webtransportgo/](webtransport/webtransportgo/)、[moq-web/](moq-web/) — WebTransport 和客户端代码
- [examples/](examples/) — 示例应用(广播、回显、原生 QUIC、中继)

## 组件
- `moqt` — 用于媒体传输(MOQ)协议的核心 Go 包。
- `moq-web` — Web 客户端的 TypeScript 实现。
- `quic` — 核心库和示例使用的 QUIC 包装器工具。
- `webtransport` — WebTransport 服务器包装器(包括 `webtransportgo`)。
- `cmd/interop` — 互操作性服务器和客户端(Go/TypeScript)。
- `examples` — 演示应用(广播、回显、原生 QUIC、中继)。

## 示例
[examples](examples) 目录包含演示如何使用 gomoqt 的示例应用:
- **互操作服务器和客户端**(`cmd/interop/`):不同 MOQ 实现之间的互操作性测试
- **广播示例**(`examples/broadcast/`):广播功能演示
- **回显示例**(`examples/echo/`):简单的回显服务器和客户端实现
- **原生 QUIC**(`examples/native_quic/`):直接 QUIC 连接示例
- **中继**(`examples/relay/`):媒体流中继实现示例

## 文档
- [GoDoc](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
- [MOQ Lite 规范](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html)
- [实现状态](moqt/README.md) — 详细的实现进度

## 规范合规性
本实现以 MOQ Lite 规范为目标。关于当前的实现进度及各章节的对应关系,请参考 [moqt 包 README](moqt/README.md)。

## 开发
### 先决条件
- Go 1.25.0 或更高版本
- [Mage](https://magefile.org/) 构建工具(使用 `go install github.com/magefile/mage@latest` 安装)

### 开发命令
```bash
# 格式化代码
mage fmt

# 运行代码检查器(需要 golangci-lint: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
mage lint

# 运行质量检查(fmt 和 lint)
mage check

# 运行所有测试
mage test:all

# 运行带覆盖率的测试
mage test:coverage
```

#### 构建与清理
```bash
# 构建代码
mage build

# 清理生成的文件
mage clean

# 显示可用命令
mage help
```

## 贡献
我们欢迎贡献!以下是您可以提供帮助的方式:
1. Fork 仓库。
2. 创建功能分支(`git checkout -b feature/amazing-feature`)。
3. 进行更改。
4. 验证代码质量:
   ```bash
   mage fmt
   mage lint
   mage test
   ```
5. 提交更改(`git commit -m 'Add amazing feature'`)。
6. 推送分支(`git push origin feature/amazing-feature`)。
7. 打开 Pull Request。

## 许可证
本项目采用 MIT 许可证;详情请参见 [LICENSE](LICENSE)。

## 致谢
- [quic-go](https://github.com/quic-go/quic-go) — Go 的 QUIC 实现
- [webtransport-go](https://github.com/okdaichi/webtransport-go) — Go 的 WebTransport 实现
- [MOQ Lite 规范](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html) — 本实现遵循的规范
