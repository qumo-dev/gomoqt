# gomoqt

<div align="center">
<sup align="center"><a href="README.md">English</a></sup>
</div>

Eine Go-Implementierung von Media over QUIC Transport (MOQT), die speziell die MOQ Lite-Spezifikation für effizientes Medien-Streaming über QUIC umsetzt.

[![Go](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml)
[![Lint](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml)
[![moq-web CI](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/OkutaniDaichi0106/gomoqt.svg)](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
[![codecov](https://codecov.io/gh/OkutaniDaichi0106/gomoqt/branch/main/graph/badge.svg?token=4LZCD3FEU3)](https://codecov.io/gh/OkutaniDaichi0106/gomoqt)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/okdaichi/gomoqt)

## Inhaltsverzeichnis
- [Übersicht](#übersicht)
- [Schnellstart](#schnellstart)
- [Funktionen](#funktionen)
- [Komponenten](#komponenten)
- [Beispiele](#beispiele)
- [Dokumentation](#dokumentation)
- [Spezifikationskonformität](#spezifikationskonformität)
- [Entwicklung](#entwicklung)
- [Zum Projekt beitragen](#zum-projekt-beitragen)
- [Lizenz](#lizenz)
- [Danksagungen](#danksagungen)

## Übersicht
Diese Implementierung folgt der [MOQ Lite-Spezifikation](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html) und ermöglicht den Aufbau einer Kommunikationsinfrastruktur für Echtzeit-Medien-Streaming-Anwendungen über QUIC.

## Schnellstart
```bash
# Mage installieren (Go 1.25+)
go install github.com/magefile/mage@latest

# Führen Sie den Interop-Test im Docker‑Container aus
mage interop:ts    # TypeScript-Client + Server
mage interop:go    # Go-Client + Server
```
```

## Funktionen
- **MOQ Lite-Protokoll** — Leichtgewichtige Version der MoQ-Spezifikation
  - **Niedrige Wiedergabelatenz** — Minimiert Latenz von Datenentdeckung, Senden/Empfangen bis zur Wiedergabe
  - **Unterbrechungsfreie Wiedergabe** — Robustes Design gegen Netzwerkschwankungen durch unabhängige Datenübertragung
  - **Netzwerkumgebungsoptimierung** — Ermöglicht Verhaltensoptimierung entsprechend der Netzwerkbedingungen
  - **Track-Management** — Publisher/Subscriber-Modell für Track-Datenübertragung
  - **Effiziente Multiplexed Delivery** — Effizientes Multiplexing durch Track-Ankündigungen und Subscriptions
  - **Web-Unterstützung** — Browser-Unterstützung über WebTransport
  - **Native QUIC-Unterstützung** — Native QUIC-Unterstützung über `quic`-Wrapper
- **Flexibles Dependency-Design** — Trennt Abhängigkeiten wie QUIC und WebTransport, ermöglicht Nutzung nur benötigter Komponenten
- **Beispiele & Interop** — Beispielanwendungen und Interop-Suite in `examples/` und `cmd/interop` (broadcast, echo, relay, native_quic, Interop-Server/Client)

### Siehe auch
- [moqt/](moqt/) — Kernpaket (Frames, Sessions, Track-Muxing)
- [quic/](quic/) — QUIC-Hilfen und Beispiel `examples/native_quic`
- [webtransport/](webtransport/), [webtransport/webtransportgo/](webtransport/webtransportgo/), [moq-web/](moq-web/) — WebTransport und Client-Code
- [examples/](examples/) — Beispiel-Apps (broadcast, echo, native_quic, relay)

## Komponenten
- `moqt` — zentrales Go-Paket für das Media over QUIC (MOQ)-Protokoll.
- `moq-web` — TypeScript-Implementierung für Web-Clients.
- `quic` — QUIC-Hilfsbibliothek, genutzt vom Kern und den Beispielen.
- `webtransport` — WebTransport-Server-Hüllen (inkl. `webtransportgo`).
- `cmd/interop` — Interop-Server und -Clients (Go/TypeScript).
- `examples` — Beispielanwendungen (broadcast, echo, native_quic, relay).

## Beispiele
Das Verzeichnis [examples](examples) enthält Beispielanwendungen zur Nutzung von gomoqt:
- **Interop Server und Client** (`cmd/interop/`): Interoperabilitätstests zwischen verschiedenen MOQ-Implementierungen
- **Broadcast-Beispiel** (`examples/broadcast/`): Demonstration der Broadcast-Funktionalität
- **Echo-Beispiel** (`examples/echo/`): Einfacher Echo-Server und -Client
- **Native QUIC** (`examples/native_quic/`): Direkte QUIC-Verbindungen
- **Relay** (`examples/relay/`): Weiterleitung von Medienströmen

## Dokumentation
- [GoDoc](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
- [MOQ Lite-Spezifikation](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html)
- [Implementierungsstatus](moqt/README.md) — Detaillierter Fortschritt der Umsetzung

## Spezifikationskonformität
Diese Implementierung richtet sich nach der MOQ Lite-Spezifikation. Den aktuellen Umsetzungsstand findest du in der [README des Pakets moqt](moqt/README.md), inklusive Nachverfolgung der implementierten Funktionen je Abschnitt der Spezifikation.

## Entwicklung
### Voraussetzungen
- Go 1.25.0 oder neuer
- [Mage](https://magefile.org/) Build-Tool (Installation: `go install github.com/magefile/mage@latest`)

### Entwicklungsbefehle
```bash
# Code formatieren
mage fmt

# Linter ausführen (benötigt golangci-lint: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
mage lint

# Qualitätsprüfungen (fmt und lint)
mage check

# Alle Tests
mage test:all

# Tests mit Coverage
mage test:coverage
```

#### Build & Clean
```bash
# Code bauen
mage build

# Generierte Dateien bereinigen
mage clean

# Verfügbare Befehle anzeigen
mage help
```

## Zum Projekt beitragen
Beiträge sind willkommen! So kannst du helfen:
1. Repository forken.
2. Feature-Branch erstellen (`git checkout -b feature/amazing-feature`).
3. Änderungen durchführen.
4. Codequalität prüfen:
   ```bash
   mage fmt
   mage lint
   mage test
   ```
5. Änderungen committen (`git commit -m 'Add amazing feature'`).
6. Branch pushen (`git push origin feature/amazing-feature`).
7. Pull Request eröffnen.

## Lizenz
Dieses Projekt steht unter der MIT-Lizenz; siehe [LICENSE](LICENSE) für Details.

## Danksagungen
- [quic-go](https://github.com/quic-go/quic-go) — QUIC-Implementierung in Go
- [webtransport-go](https://github.com/quic-go/webtransport-go) — WebTransport-Implementierung in Go
- [MOQ Lite-Spezifikation](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html) — Spezifikation, der diese Implementierung folgt