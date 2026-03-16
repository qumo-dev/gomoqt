# gomoqt

<div align="center">
<sup align="center"><a href="README.md">English</a></sup>
</div>

QUIC을 통한 효율적인 미디어 스트리밍을 위해 MOQ Lite 사양을 구현한 Media over QUIC Transport(MOQT)의 Go 구현체입니다.

[![Go](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml)
[![Lint](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml)
[![moq-web CI](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/OkutaniDaichi0106/gomoqt.svg)](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
[![codecov](https://codecov.io/gh/OkutaniDaichi0106/gomoqt/branch/main/graph/badge.svg?token=4LZCD3FEU3)](https://codecov.io/gh/OkutaniDaichi0106/gomoqt)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/okdaichi/gomoqt)

## 목차
- [개요](#개요)
- [빠른 시작](#빠른-시작)
- [기능](#기능)
- [구성 요소](#구성-요소)
- [예제](#예제)
- [문서](#문서)
- [사양 준수](#사양-준수)
- [개발](#개발)
- [기여하기](#기여하기)
- [라이센스](#라이센스)
- [감사의 글](#감사의-글)

## 개요
본 구현체는 [MOQ Lite 사양](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html)을 따르며, QUIC 전송을 사용하는 실시간 미디어 스트리밍 애플리케이션의 통신 인프라를 구축할 수 있습니다.

## 빠른 시작
```bash
# Mage 설치 (Go 1.25+)
go install github.com/magefile/mage@latest

# Docker 컨테이너에서 interop 테스트 실행 (TypeScript 클라이언트 + 서버)
mage interop:ts
# 또는 Go 클라이언트를 실행
mage interop:go
```

## 기능
- **MOQ Lite 프로토콜** — MoQ 사양의 경량 버전
  - **저지연 재생** — 데이터 검색, 송수신부터 재생까지의 지연 시간 최소화
  - **끊김 없는 재생** — 독립적인 데이터 송수신을 통한 네트워크 변동에 강한 설계
  - **네트워크 환경 최적화** — 네트워크 조건에 따른 동작 최적화 가능
  - **트랙 관리** — 트랙 데이터 송수신을 위한 Publisher/Subscriber 모델
  - **효율적인 멀티플렉싱 전송** — 트랙 공지 및 구독을 통한 효율적인 멀티플렉싱
  - **웹 지원** — WebTransport를 사용한 브라우저 지원
  - **QUIC 네이티브 지원** — `quic` 래퍼를 통한 네이티브 QUIC 지원
- **유연한 의존성 설계** — QUIC 및 WebTransport와 같은 의존성을 분리하여 필요한 구성 요소만 사용 가능
- **예제 & Interop** — `examples/` 및 `cmd/interop`의 샘플 애플리케이션 및 상호 운용성 테스트 모음 (broadcast, echo, relay, native_quic, interop 서버/클라이언트)

### 함께 보기
- [moqt/](moqt/) — 핵심 패키지 (프레임, 세션, 트랙 멀티플렉싱)
- [quic/](quic/) — QUIC 래퍼 및 `examples/native_quic`
- [webtransport/](webtransport/), [webtransport/webtransportgo/](webtransport/webtransportgo/), [moq-web/](moq-web/) — WebTransport 및 클라이언트 코드
- [examples/](examples/) — 샘플 앱 (broadcast, echo, native_quic, relay)

## 구성 요소
- `moqt` — Media over QUIC (MOQ) 프로토콜의 핵심 Go 패키지
- `moq-web` — 웹 클라이언트용 TypeScript 구현
- `quic` — 핵심 및 예제에서 사용하는 QUIC 래퍼
- `webtransport` — WebTransport 서버 래퍼 (`webtransportgo` 포함)
- `cmd/interop` — 상호 운용성 서버 및 클라이언트 (Go/TypeScript)
- `examples` — 데모 애플리케이션 (broadcast, echo, native_quic, relay)

## 예제
[examples](examples) 디렉토리에는 gomoqt 사용 방법을 보여주는 샘플 애플리케이션이 포함되어 있습니다:
- **Interop 서버 및 클라이언트** (`cmd/interop/`): 다양한 MOQ 구현 간의 상호 운용성 테스트
- **브로드캐스트 예제** (`examples/broadcast/`): 브로드캐스팅 기능 시연
- **에코 예제** (`examples/echo/`): 간단한 에코 서버 및 클라이언트 구현
- **네이티브 QUIC** (`examples/native_quic/`): 직접 QUIC 연결 예제
- **릴레이** (`examples/relay/`): 미디어 스트리밍 릴레이 기능

## 문서
- [GoDoc](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
- [MOQ Lite 사양](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html)
- [구현 현황](moqt/README.md) — 상세한 구현 진행 상황

## 사양 준수
본 구현체는 MOQ Lite 사양을 목표로 합니다. 현재 구현 상태는 [moqt 패키지 README](moqt/README.md)에서 확인할 수 있으며, 사양 섹션에 따라 구현된 기능에 대한 상세한 추적 정보가 포함되어 있습니다.

## 개발
### 필수 조건
- Go 1.25.0 이상
- [Mage](https://magefile.org/) 빌드 도구 (`go install github.com/magefile/mage@latest`로 설치)

### 개발 명령어
```bash
# 코드 포맷팅
mage fmt

# 린터 실행 (golangci-lint 필요: go install github.com/golangci-lint/cmd/golangci-lint@latest)
mage lint

# 품질 검사 (fmt 및 lint)
mage check

# 모든 테스트 실행
mage test:all

# 커버리지와 함께 테스트 실행
mage test:coverage
```

#### 빌드 및 정리
```bash
# 코드 빌드
mage build

# 생성된 파일 정리
mage clean

# 사용 가능한 명령어 표시
mage help
```

## 기여하기
기여를 환영합니다! 다음과 같이 도와주실 수 있습니다:
1. 저장소를 포크합니다.
2. 기능 브랜치를 생성합니다 (`git checkout -b feature/amazing-feature`).
3. 변경 사항을 작성합니다.
4. 코드 품질을 검증합니다:
   ```bash
   mage fmt
   mage lint
   mage test
   ```
5. 변경 사항을 커밋합니다 (`git commit -m 'Add amazing feature'`).
6. 브랜치를 푸시합니다 (`git push origin feature/amazing-feature`).
7. Pull Request를 엽니다.

## 라이센스
이 프로젝트는 MIT 라이센스에 따라 라이센스가 부여됩니다. 자세한 내용은 [LICENSE](LICENSE)를 참조하세요.

## 감사의 글
- [quic-go](https://github.com/quic-go/quic-go) — Go의 QUIC 구현
- [webtransport-go](https://github.com/okdaichi/webtransport-go) — Go의 WebTransport 구현
- [MOQ Lite 사양](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html) — 본 구현이 따르는 사양
