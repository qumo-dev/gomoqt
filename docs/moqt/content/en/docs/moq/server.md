---
title: Server
weight: 2
---

`moqt.Server` manages server-side operations for the MoQ protocol. It listens for incoming QUIC connections, dispatches them based on ALPN negotiation, and manages their lifecycle.

The server uses ALPN (Application-Layer Protocol Negotiation) to determine the transport:
- `moq-lite-04` (`moqt.NextProtoMOQ`) — Native QUIC, dispatched to `Server.Handler`
- `h3` (`moqt.NextProtoH3`) — WebTransport via HTTP/3, dispatched to `Server.WebTransportServer`

{{% details title="Overview" closed="true" %}}

```go
func main() {
    mux := moqt.NewTrackMux(0)

    server := moqt.Server{
        Addr: ":9000",
        TLSConfig: &tls.Config{
            NextProtos:   []string{moqt.NextProtoH3, moqt.NextProtoMOQ},
            Certificates: []tls.Certificate{loadCert()},
        },
        QUICConfig: &quic.Config{
            Allow0RTT:       true,
            EnableDatagrams: true,
        },
        TrackMux: mux,
        Handler: moqt.HandleFunc(func(sess *moqt.Session) {
            slog.Info("Native QUIC session established")
            <-sess.Context().Done()
        }),
        WebTransportServer: &webtransportgo.Server{},
        Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
            Level: slog.LevelInfo,
        })),
    }

    err := server.ListenAndServe()
    if err != nil {
        slog.Error("Failed to start server", "error", err)
    }
}
```

{{% /details %}}

## Initialize a Server

There is no dedicated function (such as a constructor) for initializing a server.
Users define the struct directly and assign values to its fields as needed.

```go
    server := moqt.Server{
        // Set server options here
    }
```

### Configuration

The following table describes the public fields of the `Server` struct:

| Field                  | Type                        | Description                                 |
|------------------------|-----------------------------|---------------------------------------------|
| `Addr`                 | `string`                    | Server address and port                     |
| `TLSConfig`            | [`*tls.Config`](https://pkg.go.dev/crypto/tls#Config) | TLS configuration for secure connections    |
| `QUICConfig`           | [`*quic.Config`](https://pkg.go.dev/github.com/quic-go/quic-go#Config)              | QUIC protocol configuration                 |
| `Config`               | [`*moqt.Config`](https://pkg.go.dev/github.com/okdaichi/gomoqt/moqt#Config)                   | MOQ protocol configuration                  |
| `TrackMux`             | `*moqt.TrackMux`              | Multiplexer for routing announcements and track subscriptions. If nil, a global default mux is used. |
| `Handler`              | [`moqt.Handler`](https://pkg.go.dev/github.com/okdaichi/gomoqt/moqt#Handler)                 | Handler for accepted native QUIC sessions. If nil, native QUIC connections are not handled. |
| `FetchHandler`         | [`moqt.FetchHandler`](https://pkg.go.dev/github.com/okdaichi/gomoqt/moqt#FetchHandler)       | Handles incoming FETCH requests on native QUIC sessions. If nil, FETCH requests are rejected. |
| `WebTransportServer`   | `moqt.WebTransportServer`     | WebTransport server for handling WebTransport sessions. If nil, a default implementation is used. |
| `ListenFunc`           | `func(addr, tlsConfig, quicConfig) (QUICListener, error)` | Custom QUIC listener function. If nil, the default implementation is used. |
| `ConnContext`          | `func(ctx context.Context, conn StreamConn) context.Context` | Modifies the context used for a new connection. Optional. |
| `NextSessionURI`       | `string`                    | The URI sent to clients during `Shutdown`, allowing them to reconnect to a different server. If empty, no redirect URI is provided. |
| `Logger`               | [`*slog.Logger`](https://pkg.go.dev/log/slog#Logger)              | Logger for server events and errors. If nil, logging is disabled. |

{{< tabs items="Using Default QUIC, Using Custom QUIC" >}}
{{< tab >}}

[`quic-go/quic-go`](https://github.com/quic-go/quic-go) is used internally as the default QUIC implementation when relevant fields which is set for customization are not set or `nil`.

{{<github-readme-stats user="quic-go" repo="quic-go" >}}

{{< /tab >}}
{{< tab >}}

To use a custom QUIC implementation, you need to provide your own `ListenFunc`. When `Server.ListenFunc` is set, it is used to listen for incoming QUIC connections instead of the default implementation.

```go {filename="gomoqt/moqt/server.go",base_url="https://github.com/okdaichi/gomoqt/tree/main/moqt/server.go"}
type Server struct {
    // ...
	ListenFunc func(addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (QUICListener, error)
    // ...
}
```
{{< /tab >}}

{{< /tabs >}}

{{< tabs items="Using Default WebTransport, Using Custom WebTransport" >}}
{{< tab >}}

[`quic-go/webtransport-go`](https://github.com/okdaichi/webtransport-go) is used internally as the default WebTransport implementation when `WebTransportServer` is nil.

{{<github-readme-stats user="quic-go" repo="webtransport-go" >}}

{{< /tab >}}
{{< tab >}}

To use a custom WebTransport implementation, provide your own implementation of the `WebTransportServer` interface:

```go {filename="gomoqt/moqt/server.go",base_url="https://github.com/okdaichi/gomoqt/tree/main/moqt/server.go"}
type WebTransportServer interface {
    ServeQUICConn(conn StreamConn) error
    Close() error
}
```
{{< /tab >}}

{{< /tabs >}}

## Handle Sessions

### ALPN-Based Dispatch

`ServeQUICConn` detects the negotiated ALPN protocol and dispatches the connection accordingly:

```go
    // Native QUIC (moq-lite-04) → Server.Handler.ServeMOQ(sess)
    // HTTP/3 (h3)               → Server.WebTransportServer.ServeQUICConn(conn)
```

### Native QUIC Handler

For native QUIC connections (ALPN `moq-lite-04`), the `Handler` field receives a `*moqt.Session` directly:

```go
type Handler interface {
    ServeMOQ(sess *Session)
}
```

```go
    server := moqt.Server{
        Handler: moqt.HandleFunc(func(sess *moqt.Session) {
            slog.Info("Session established",
                "remote", sess.RemoteAddr(),
                "version", sess.ConnectionState().Version,
            )
            <-sess.Context().Done()
        }),
        // ...
    }
```

### WebTransport Handler

For WebTransport connections, use `moqt.WebTransportHandler` to handle HTTP upgrade and session setup:

```go
    wtHandler := &moqt.WebTransportHandler{
        TrackMux: mux,
        Handler: moqt.HandleFunc(func(sess *moqt.Session) {
            slog.Info("WebTransport session established")
            <-sess.Context().Done()
        }),
        CheckOrigin: func(r *http.Request) bool {
            return r.Header.Get("Origin") == "https://trusted.example.com"
        },
        FetchHandler: moqt.FetchHandlerFunc(func(w *moqt.GroupWriter, r *moqt.FetchRequest) {
            // Handle fetch requests
        }),
        Logger: logger,
    }

    http.Handle("/moq", wtHandler)
```

`WebTransportHandler` implements `http.Handler` and can be used with any HTTP server. It upgrades the HTTP/3 connection to WebTransport, creates a session, and invokes the configured `Handler`.

## Run the Server

`Server.ListenAndServe` starts the server listening for incoming connections.

```go
    server.ListenAndServe()
```

For more advanced use cases:
- `ListenAndServeTLS(certFile, keyFile string)`: Starts the server with TLS certificates loaded from files.
- `ServeQUICListener(ln QUICListener)`: Serves on an existing QUIC listener.
- `ServeQUICConn(conn StreamConn)`: Handles a single QUIC connection directly.

## Shutting Down a Server

Servers also support immediate and graceful shutdowns.

### Immediate Shutdown

`Server.Close` method terminates all sessions and closes listeners forcefully.

```go
    server.Close() // Immediate shutdown
```

### Graceful Shutdown

`Server.Shutdown` method notifies all active sessions with `NextSessionURI` as the redirect and waits for them to close gracefully before forcing termination.

```go
    server := moqt.Server{
        NextSessionURI: "https://backup.example.com/moq",
        // ...
    }

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    err := server.Shutdown(ctx)
    if err != nil {
        // Handle forced termination
    }
```

If the context expires before all sessions close, remaining connections are closed with a `GoAwayTimeoutErrorCode`.
