---
title: Go
weight: 1
---

## Building Go Applications

### Prerequisites

- Go 1.26 or later (recommended)
- Git (required for building from source)
- (Optional, recommended) Local certificate tool mkcert (for TLS usage)

{{% steps %}}

### Initialize Go module

If you haven't already, initialize a Go module in your project directory.

```bash
go mod init [module_name]
```

### Get gomoqt

Download the package in your Go environment.

```bash
go get github.com/qumo-dev/gomoqt
```

### Importing packages

gomoqt provides several packages that can be imported into your Go application. The main package is `moqt`, which contains the core logic for Media over QUIC. In addition to `moqt`, the following packages are provided:

| Import Path                              | Description                                                                 |
|:-----------------------------------------|:----------------------------------------------------------------------------|
| `github.com/qumo-dev/gomoqt/moqt`        | Main package implementing the core logic for Media over QUIC.               |
| `github.com/qumo-dev/gomoqt/msf`         | MOQT Streaming Format — catalogs, catalog deltas, and timeline records.     |
| `github.com/qumo-dev/gomoqt/transport`   | Abstraction and interface definitions for the QUIC/WebTransport layer used by `moqt`.<br>Wraps `quic-go/quic-go` and `okdaichi/webtransport-go`, which `moqt` uses by default. |

**Example of importing the `moqt` package**:

```go
import (
	"github.com/qumo-dev/gomoqt/moqt"
)
```
{{% /steps %}}
