---
title: Client
weight: 1
---

`moqt.Dialer` manages client-side operations for the MoQ protocol. It establishes sessions with MOQ servers over both WebTransport and native QUIC connections.

{{% details title="Overview" closed="true" %}}

```go
func main() {
    // Create a new dialer
	dialer := moqt.Dialer{
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	// Dial and establish a session with the server
	sess, err := dialer.Dial(context.Background(), "https://[addr]:[port]/[path]", nil)
	if err != nil {
		log.Fatalf("Failed to dial: %v", err)
	}
	defer sess.CloseWithError(moqt.NoError, "done")

	// Use the session (e.g., subscribe to tracks, receive announcements)
}
```
{{% /details %}}

## Initialize a Dialer

There is no dedicated function (such as a constructor) for initializing a dialer.
Users define the struct directly and assign values to its fields as needed.

```go
    dialer := moqt.Dialer{
		// Set dialer options here
	}
```

### Configuration

The following table describes the public fields of the `moqt.Dialer` struct:

| Field                  | Type                        | Description                                 |
|------------------------|-----------------------------|---------------------------------------------|
| `TLSConfig`            | [`*tls.Config`](https://pkg.go.dev/crypto/tls#Config) | TLS configuration for secure connections    |
| `QUICConfig`           | [`*quic.Config`](https://pkg.go.dev/github.com/quic-go/quic-go#Config)              | QUIC configuration for raw QUIC connections                 |
| `Config`               | [`*moqt.Config`](https://pkg.go.dev/github.com/okdaichi/gomoqt/moqt#Config)                   | MOQ protocol configuration                  |
| `DialQUICFunc`         | `func(ctx, addr, tlsConfig, quicConfig) (StreamConn, error)` | Custom QUIC dial function. If nil, the default dialer is used. |
| `DialWebTransportFunc` | `func(ctx, addr, header, tlsConfig) (*http.Response, WebTransportSession, error)` | Custom WebTransport dial function. If nil, the default dialer is used. |
| `FetchHandler`         | [`moqt.FetchHandler`](https://pkg.go.dev/github.com/okdaichi/gomoqt/moqt#FetchHandler) | Handles incoming fetch requests on WebTransport sessions. If nil, fetch requests are not handled. |
| `OnGoaway`             | `func(newSessionURI string)` | Called when the server requests session migration. The `newSessionURI` parameter contains the redirect URI, which may be empty. |
| `Logger`               | [`*slog.Logger`](https://pkg.go.dev/log/slog#Logger)              | Logger for connection and session events. If nil, logging is disabled.         |

{{< tabs items="Using Default QUIC, Using Custom QUIC" >}}
{{< tab >}}

[`quic-go/quic-go`](https://github.com/quic-go/quic-go) is used internally as the default QUIC implementation when relevant fields which is set for customization are not set or `nil`.

{{<github-readme-stats user="quic-go" repo="quic-go" >}}

{{< /tab >}}
{{< tab >}}

To use a custom QUIC implementation, you need to provide your own dial function. When `Dialer.DialQUICFunc` is set, it is used to dial QUIC connections instead of the default implementation.

```go {filename="gomoqt/moqt/dialer.go",base_url="https://github.com/okdaichi/gomoqt/tree/main/moqt/dialer.go"}
type Dialer struct {
    // ...
	DialQUICFunc func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error)
    // ...
}
```
{{< /tab >}}

{{< /tabs >}}

{{< tabs items="Using Default WebTransport, Using Custom WebTransport" >}}
{{< tab >}}

[`quic-go/webtransport-go`](https://github.com/okdaichi/webtransport-go) is used internally as the default WebTransport implementation when relevant fields which is set for customization are not set or `nil`.

{{<github-readme-stats user="quic-go" repo="webtransport-go" >}}

{{< /tab >}}
{{< tab >}}

To use a custom WebTransport implementation, you need to provide your own dial function. When `Dialer.DialWebTransportFunc` is set, it is used to dial WebTransport connections instead of the default implementation.

```go {filename="gomoqt/moqt/dialer.go",base_url="https://github.com/okdaichi/gomoqt/tree/main/moqt/dialer.go"}
type Dialer struct {
    // ...
	DialWebTransportFunc func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error)
    // ...
}
```
{{< /tab >}}

{{< /tabs >}}

## Dial and Establish Session

A `Dialer` can connect to servers using the `Dial` method, which automatically selects the transport based on the URL scheme:
- `https://` — WebTransport (HTTP/3)
- `moqt://` — Native QUIC

```go
	var mux *moqt.TrackMux
	sess, err := dialer.Dial(ctx, "https://[addr]:[port]/[path]", mux)
	if err != nil {
		// Handle error
		return
	}

	// Handle session
```

You can also use transport-specific methods directly:

```go
	// WebTransport
	sess, err := dialer.DialWebTransport(ctx, "host:port", "/path", mux)

	// Native QUIC
	sess, err := dialer.DialQUIC(ctx, "host:port", mux)
```

> [!NOTE] Note: Nil TrackMux
> When `mux` is set to `nil`, the `moqt.DefaultMux` will be used by default.
> Ensure that the `mux` is properly configured for your use case to avoid unexpected behavior.

> [!NOTE] Note: ALPN Negotiation
> For native QUIC connections, the dialer automatically sets the ALPN token to `moq-lite-04` (`moqt.NextProtoMOQ`) if `TLSConfig.NextProtos` is not configured.