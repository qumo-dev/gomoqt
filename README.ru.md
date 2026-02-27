# gomoqt

<div align="center">
<sup align="center"><a href="README.md">English</a></sup>
</div>

Реализация Media over QUIC Transport (MOQT) на Go, использующая спецификацию MOQ Lite для эффективной потоковой передачи мультимедиа по QUIC.

[![Go](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/go.yml)
[![Lint](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/lint.yml)
[![moq-web CI](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml/badge.svg)](https://github.com/OkutaniDaichi0106/gomoqt/actions/workflows/moq-web-ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/OkutaniDaichi0106/gomoqt.svg)](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
[![codecov](https://codecov.io/gh/OkutaniDaichi0106/gomoqt/branch/main/graph/badge.svg?token=4LZCD3FEU3)](https://codecov.io/gh/OkutaniDaichi0106/gomoqt)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/okdaichi/gomoqt)

## Содержание

- [Обзор](#обзор)
- [Быстрый старт](#быстрый-старт)
- [Возможности](#возможности)
- [Компоненты](#компоненты)
- [Примеры](#примеры)
- [Документация](#документация)
- [Соответствие спецификации](#соответствие-спецификации)
- [Разработка](#разработка)
- [Вклад в проект](#вклад-в-проект)
- [Лицензия](#лицензия)
- [Благодарности](#благодарности)

## Обзор

Эта реализация следует [спецификации MOQ Lite](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html) и позволяет создавать коммуникационную инфраструктуру для приложений потоковой передачи мультимедиа в реальном времени с использованием транспорта QUIC.

## Быстрый старт
```bash
# установить Mage (Go 1.25+)
go install github.com/magefile/mage@latest

# Запустить interop-тест в Docker-контейнере (TypeScript клиент + сервер)
mage interop:ts
# или запустить Go-клиент
mage interop:go
```

## Возможности
- **Протокол MOQ Lite** — облегчённая версия спецификации MoQ
  - **Воспроизведение с низкой задержкой** — минимизация задержки от обнаружения данных, передачи/приёма до воспроизведения
  - **Непрерывное воспроизведение** — устойчивая к колебаниям сети архитектура благодаря независимой передаче/приёму данных
  - **Оптимизация под сетевую среду** — возможность оптимизации поведения в соответствии с условиями сети
  - **Управление треками** — модель Publisher/Subscriber для передачи/приёма данных треков
  - **Эффективная мультиплексная доставка** — эффективное мультиплексирование через объявления треков и подписки
  - **Поддержка Web** — поддержка браузеров с использованием WebTransport
  - **Нативная поддержка QUIC** — нативная поддержка QUIC через обёртки `quic`
- **Гибкий дизайн зависимостей** — разделяет зависимости, такие как QUIC и WebTransport, позволяя использовать только необходимые компоненты
- **Примеры и interop** — примеры приложений и набор interop в `examples/` и `cmd/interop` (broadcast, echo, relay, native_quic, interop сервер/клиент)

### Также см.
- [moqt/](moqt/) — основной пакет (фреймы, сессии, мультиплексирование треков)
- [quic/](quic/) — обёртки над QUIC и пример `examples/native_quic`
- [webtransport/](webtransport/), [webtransport/webtransportgo/](webtransport/webtransportgo/), [moq-web/](moq-web/) — WebTransport и клиентская часть
- [examples/](examples/) — образцы (broadcast, echo, native_quic, relay)

## Компоненты
- `moqt` — основной Go-пакет для протокола Media over QUIC (MOQ).
- `moq-web` — реализация для веб-клиента на TypeScript.
- `quic` — вспомогательные обёртки QUIC, используемые ядром и примерами.
- `webtransport` — серверные обёртки WebTransport (включая `webtransportgo`).
- `cmd/interop` — сервер и клиенты для тестирования совместимости (Go/TypeScript).
- `examples` — демонстрационные приложения (broadcast, echo, native_quic, relay).

## Примеры
Каталог [examples](examples) содержит примеры приложений, демонстрирующих использование gomoqt:
- **Interop Server и Client** (`cmd/interop/`): тестирование совместимости между различными реализациями MOQ
- **Пример трансляции** (`examples/broadcast/`): демонстрация функциональности вещания
- **Пример эхо-сервера** (`examples/echo/`): простой эхо-сервер и клиент
- **Нативный QUIC** (`examples/native_quic/`): примеры прямых QUIC-подключений
- **Релей** (`examples/relay/`): ретрансляция медиапотоков

## Документация
- [GoDoc](https://pkg.go.dev/github.com/OkutaniDaichi0106/gomoqt)
- [Спецификация MOQ Lite](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html)
- [Статус реализации](moqt/README.md) — подробное отслеживание прогресса

## Соответствие спецификации
Реализация ориентирована на спецификацию MOQ Lite. Актуальный статус реализации и соответствия разделам спецификации приведён в [README пакета moqt](moqt/README.md).

## Разработка
### Необходимые инструменты
- Go 1.25.0 или новее
- Система сборки [Mage](https://magefile.org/) (установка: `go install github.com/magefile/mage@latest`)

### Команды разработки
```bash
# Форматирование кода
mage fmt

# Запуск линтера (требуется golangci-lint: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
mage lint

# Проверки качества (fmt и lint)
mage check

# Все тесты
mage test:all

# Тесты с покрытием
mage test:coverage
```

#### Сборка и очистка
```bash
# Сборка кода
mage build

# Очистка сгенерированных файлов
mage clean

# Показ доступных команд
mage help
```

## Вклад в проект
Мы приветствуем вклад в развитие проекта! Вот как вы можете помочь:
1. Сделайте форк репозитория.
2. Создайте ветку для новой функции (`git checkout -b feature/amazing-feature`).
3. Внесите изменения.
4. Проверьте качество кода:
   ```bash
   mage fmt
   mage lint
   mage test
   ```
5. Зафиксируйте изменения (`git commit -m 'Add amazing feature'`).
6. Отправьте ветку (`git push origin feature/amazing-feature`).
7. Откройте Pull Request.

## Лицензия
Проект распространяется по лицензии MIT. См. [LICENSE](LICENSE) для деталей.

## Благодарности
- [quic-go](https://github.com/quic-go/quic-go) — реализация QUIC на Go
- [webtransport-go](https://github.com/quic-go/webtransport-go) — реализация WebTransport на Go
- [Спецификация MOQ Lite](https://kixelated.github.io/moq-drafts/draft-lcurley-moq-transfork.html) — спецификация, которой следует данная реализация