# gomoqt

<div align="center">
<sup align="center"><a href="README.md">English</a></sup>
</div>

Media over QUIC (MOQ) の Go 実装で、MOQ Lite 仕様に基づき QUIC 上で効率的なメディア配信を行います。

[![Go](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml)
[![Lint](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml)
[![moq-web CI](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/OkutaniDaichi0106/gomoqt.svg)](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
[![codecov](https://codecov.io/gh/OkutaniDaichi0106/gomoqt/branch/main/graph/badge.svg?token=4LZCD3FEU3)](https://codecov.io/gh/OkutaniDaichi0106/gomoqt)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/okdaichi/gomoqt)

## 目次
- [概要](#概要)
- [クイックスタート](#クイックスタート)
- [機能](#機能)
- [コンポーネント](#コンポーネント)
- [サンプル](#サンプル)
- [ドキュメント](#ドキュメント)
- [仕様準拠](#仕様準拠)
- [開発](#開発)
- [コントリビューション](#コントリビューション)
- [ライセンス](#ライセンス)
- [付録](#付録)

## 概要
本実装は [MOQ Lite 仕様](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html) に準拠し、QUIC を用いたリアルタイム・メディアストリーミングアプリケーションの通信基盤を構築することができます。

## クイックスタート
```bash
# Mage をインストール (Go 1.25+)
go install github.com/magefile/mage@latest

# Docker コンテナ内で interop テストを実行（TypeScript クライアント + サーバ）
mage interop:ts
# または Go クライアントを実行
mage interop:go
```

## 機能
- **MOQ Lite プロトコル** — MoQ の軽量版仕様
  - **低遅延再生** — データの発見、送受信から再生までの遅延を最小化
  - **途切れない再生** — 独立したデータ送受信によるネットワーク変動に強い設計
  - **ネットワーク環境への最適化** — ネットワーク環境に応じて動作を最適化することが可能
  - **トラック管理** — Publisher/Subscriber モデルによるトラックデータの送受信
  - **効率的な多重化配信** — トラックのアナウンスとサブスクリプションによる効率的な多重化
  - **Webサポート** — WebTransport を利用したブラウザ対応
  - **QUICネイティブサポート** — `quic` ラッパーによるネイティブ QUIC 対応
- **柔軟な依存設計** — QUIC や WebTransport などの依存を分離し、必要な部分だけを利用可能
- **サンプル & 相互接続テスト** — `examples/` と `cmd/interop` にサンプルと相互運用テスト群 (broadcast, echo, relay, native_quic, interop サーバー/クライアント)

### あわせて参照
- [moqt/](moqt/) — コアパッケージ (フレーム、セッション、トラック多重化)
- [quic/](quic/) — QUIC ラッパーと `examples/native_quic`
- [webtransport/](webtransport/), [webtransport/webtransportgo/](webtransport/webtransportgo/), [moq-web/](moq-web/) — WebTransport とクライアントコード
- [examples/](examples/) — サンプル (broadcast, echo, native_quic, relay)

## コンポーネント
- `moqt` — Media over QUIC (MOQ) プロトコルの Go 実装パッケージ
- `moq-web` — Web クライアント向け TypeScript 実装
- `quic` — コアとサンプルで使う QUIC ラッパー
- `webtransport` — WebTransport サーバー向けラッパー (`webtransportgo` を含む)
- `cmd/interop` — Interop サーバー/クライアント (Go/TypeScript)
- `examples` — デモアプリ (broadcast, echo, native_quic, relay)

## サンプル
[examples](examples) ディレクトリには様々な gomoqt の利用例が含まれています:
- **Interop サーバー/クライアント** (`cmd/interop/`): 異なる MOQ 実装間の相互運用テスト
- **ブロードキャスト** (`examples/broadcast/`): ブロードキャスト機能のデモ
- **エコー** (`examples/echo/`): シンプルなエコーサーバーとクライアントの実装例
- **ネイティブ QUIC** (`examples/native_quic/`): 直接 QUIC で接続する例
- **リレー** (`examples/relay/`): メディアストリームの中継実装の例

## ドキュメント
- [GoDoc](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
- [MOQ Lite 仕様](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html)
- [実装状況](moqt/README.md) — 実装進捗の詳細

## 仕様準拠
本実装は MOQ Lite 仕様をターゲットとしています。実装状況と各セクションへの対応は [moqt パッケージの README](moqt/README.md) を参照してください。

## 開発する
### 前提条件
- Go 1.25.0 以上
- [Mage](https://magefile.org/) ビルドツール (`go install github.com/magefile/mage@latest`)

### 開発用コマンド
```bash
# フォーマット
mage fmt

# リント (golangci-lint が必要: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
mage lint

# 品質チェック (fmt + lint)
mage check

# 全テスト
mage test:all

# カバレッジ付きテスト
mage test:coverage
```

#### ビルドとクリーン
```bash
# ビルド
mage build

# 生成物のクリーン
mage clean

# 利用可能なコマンドを表示
mage help
```

## コントリビューション
ご協力お待ちしています！日本語対応できます。
1. リポジトリをフォーク
2. 機能ブランチを作成 (`git checkout -b feature/amazing-feature`)
3. 変更を加える
4. コード品質を確認:
   ```bash
   mage fmt
   mage lint
   mage test
   ```
5. コミット (`git commit -m 'Add amazing feature'`)
6. プッシュ (`git push origin feature/amazing-feature`)
7. プルリクエストを作成

## ライセンス
本プロジェクトは MIT ライセンスです。詳細は [LICENSE](LICENSE) を参照してください。

## 謝辞
- [quic-go](https://github.com/quic-go/quic-go) — Go による QUIC 実装
- [webtransport-go](https://github.com/okdaichi/webtransport-go) — Go による WebTransport 実装
- [MOQ Lite 仕様](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html) — 本実装が準拠する仕様