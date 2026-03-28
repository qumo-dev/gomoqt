---
title: Server
weight: 2
---

`moqt.Server` manages server-side operations for the MoQ protocol. It listens for incoming QUIC connections, establishes MoQ sessions, relays data, and manages their lifecycle.

{{% details title="Overview" closed="true" %}}

```go
func main() {
    server := moqt.Server{
        Addr: "moqt.example.com:9000",
        TLSConfig: &tls.Config{
            NextProtos:         []string{"h3", "moq-lite-03"},
            Certificates:       []tls.Certificate{loadCert()},
            InsecureSkipVerify: false,
        },
        QUICConfig: &quic.Config{
            Allow0RTT:       true,
            EnableDatagrams: true,
        },
        CheckHTTPOrigin: func(r *http.Request) bool {
            return r.Header.Get("Origin") == "https://trusted.example.com"
        },
        Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
            Level: slog.LevelInfo,
        })),
    }

    path := "/relay"

    // Handle WebTransport connections
    http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
        err := server.HandleWebTransport(w, r)
        if err != nil {
            slog.Error("Failed to serve MoQ over WebTransport", "error", err)
        }
    })

    // Set up MoQ handler
    moqt.HandleFunc(path, func(w moqt.SetupResponseWriter, r *moqt.SetupRequest) {
        sess, err := moqt.Accept(w, r, nil)
        if err != nil {
            slog.Error("Failed to accept session", "error", err)
            return
        }

        slog.Info("New session established")

        // Handle announcements and tracks...
    })

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
| `QUICConfig`           | [`*quic.Config`](https://pkg.go.dev/github.com/okdaichi/gomoqt/quic#Config)              | QUIC protocol configuration                 |
| `Config`               | [`*moqt.Config`](https://pkg.go.dev/github.com/okdaichi/gomoqt/moqt#Config)                   | MOQ protocol configuration                  |
| `CheckHTTPOrigin`      | `func(*http.Request) bool`     | Validates the HTTP Origin header for WebTransport connections. If nil, all origins are accepted. |
| `Handler`              | [`moqt.Handler`](https://pkg.go.dev/github.com/okdaichi/gomoqt/moqt#Handler)                 | Set-up Request handler for routing                 |
| `ListenFunc`           | [`quic.ListenAddrFunc`](https://pkg.go.dev/github.com/okdaichi/gomoqt/quic#ListenAddrFunc)   | Function to listen for QUIC connections     |
| `NewWebtransportServerFunc` | `func(checkOrigin func(*http.Request) bool) webtransport.Server` | Function to create a new WebTransport server |
| `Logger`               | [`*slog.Logger`](https://pkg.go.dev/log/slog#Logger)              | Logger for server events and errors         |


{{< tabs items="Using Default QUIC, Using Custom QUIC" >}}
{{< tab >}}

[`quic-go/quic-go`](https://github.com/quic-go/quic-go) is used internally as the default QUIC implementation when relevant fields which is set for customization are not set or `nil`.

{{<github-readme-stats user="quic-go" repo="quic-go" >}}

{{< /tab >}}
{{< tab >}}

To use a custom QUIC implementation, you need to provide your own implementation of the `gomoqt/quic` interfaces and `quic.ListenAddrFunc`. `(moqt.Server).ListenFunc` field is set, it is used to listen for incoming QUIC connections instead of the default implementation.

```go {filename="gomoqt/moqt/server.go",base_url="https://github.com/okdaichi/gomoqt/tree/main/moqt/server.go"}
type Server struct {
    // ...
	ListenFunc quic.ListenAddrFunc
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

To use a custom WebTransport implementation, you need to provide your own implementation of the `webtransport.Server` interface and a function to create it. `(moqt.Server).NewWebtransportServerFunc` field is set, it is used to create a new WebTransport server instead of the default implementation.

```go {filename="gomoqt/moqt/server.go",base_url="https://github.com/okdaichi/gomoqt/tree/main/moqt/server.go"}
type Server struct {
    // ...
    NewWebtransportServerFunc func(checkOrigin func(*http.Request) bool) webtransport.Server
    // ...
}
```
{{< /tab >}}

{{< /tabs >}}

## Accept and Set-Up Sessions

### Route Set-Up Requests

Before establishing sessions, servers have to handle incoming set-up requests for a specific path and route them to appropriate handlers.
`(Server).SetupHandler` field is used for this purpose.

```go  {filename="gomoqt/moqt/server.go",base_url="https://github.com/okdaichi/gomoqt/tree/main/moqt/server.go"}
type Server struct {
    // ...
    SetupHandler SetupHandler
    // ...
}
```

```go {filename="gomoqt/moqt/router.go",base_url="https://github.com/okdaichi/gomoqt/tree/main/moqt/router.go"}
type SetupHandler interface {
    ServeMOQ(SetupResponseWriter, *SetupRequest)
}

type SetupHandlerFunc func(SetupResponseWriter, *SetupRequest)
```
{{< tabs items="Using Default Router, Using Custom Router" >}}
{{< tab >}}

When `(Server).SetupHandler` is not set and is nil, `moqt.DefaultRouter` is the default router used by the server.

You can register your handlers to the default router as follows:

```go
    server = &moqt.Server{
        SetupHandler: nil,
        // Other server fields...
    }

    // Register handlers to the default router
    moqt.DefaultRouter.HandleFunc("/path", func(w moqt.SetupResponseWriter, r *moqt.SetupRequest){/* ... */})
    moqt.DefaultRouter.Handle("/path", handler)

    // You can also use global functions
    moqt.HandleFunc("/path", handlerFunc)
    moqt.Handle("/path", handler)
```

{{< /tab >}}
{{< tab >}}

If you need more control over routing, you can create a custom router and set it as the server's handler:

```go
    var router moqt.SetupHandler

    server = &moqt.Server{
        SetupHandler: router,
        // Other server fields...
    }
```

{{< /tab >}}
{{< /tabs >}}

### Accept Sessions

After a set-up request is routed to a specific handler and is accepted, a session is established.

```go
    var server *moqt.Server
    var mux *moqt.TrackMux

    moqt.HandleFunc("/path", func(w moqt.SetupResponseWriter, r *moqt.SetupRequest) {
        sess, err := moqt.Accept(w, r, mux)
        if err != nil {
            // Handle error
            return
        }

        // Handle the established session
    })
```

The `moqt.Accept` function establishes a new MoQ session by accepting the setup request. It takes:
- `w moqt.SetupResponseWriter`: Writer to send the server response
- `r *moqt.SetupRequest`: The client's setup request
- `mux *moqt.TrackMux`: Multiplexer for track management (can be nil for default handling)

On success, it returns a `*moqt.Session` for managing the established connection.

## Handle WebTransport Connections

For WebTransport-based MoQ sessions, integrate the server with an HTTP server using `(moqt.Server).HandleWebTransport` method.

**Using with net/http:**

```go
http.HandleFunc("/moq", func(w http.ResponseWriter, r *http.Request) {
    err := server.HandleWebTransport(w, r)
    if err != nil {
        // Handle error
    }

    // Fallback to another protocol if not WebTransport
})
```

The `(moqt.Server).HandleWebTransport` method upgrades the HTTP/3 connection to WebTransport, accepts the session stream, and routes the setup request to the configured `(moqt.Server).SetupHandler`.

## Run the Server

`(moqt.Server).ListenAndServe()` starts the server listening for incoming connections and setting up sessions.

```go
    server.ListenAndServe()
```

For more advanced use cases:
- `ListenAndServeTLS(certFile, keyFile string)`: Starts the server with TLS certificates loaded from files.
- `ServeQUICListener(ln quic.Listener)`: Serves on an existing QUIC listener.
- `ServeQUICConn(conn quic.Connection)`: Handles a single QUIC connection directly.

## Shutting Down a Server

Servers also support immediate and graceful shutdowns.

### Immediate Shutdown

`(moqt.Server).Close` method terminates all sessions and closes listeners forcefully.

```go
    server.Close() // Immediate shutdown
```

### Graceful Shutdown

`(moqt.Server).Shutdown` method allows sessions to close gracefully before forcing termination.

```go
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    err := server.Shutdown(ctx)
    if err != nil {
        // Handle forced termination
    }
```

> [!NOTE] Note: GOAWAY message
> The current implementation does not send a GOAWAY message during shutdown. Immediate session closure occurs when the context is canceled. This will be updated once the GOAWAY message specification is finalized.

## 📝 Future Work

- Implement GOAWAY message: (#XXX)
