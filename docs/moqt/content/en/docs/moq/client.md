---
title: Client
weight: 1
---

`moqt.Client` manages client-side operations for the MoQ protocol. It establishes and maintains sessions, handling the lifecycle.

{{% details title="Overview" closed="true" %}}

```go
func main() {
    // Create a new client
	client := moqt.Client{
		// Set client options here
	}

	// Dial and establish a session with the server
	sess, err := client.Dial(context.Background(), "https://[addr]:[port]/[path]", nil)
	if err != nil {
		log.Fatalf("Failed to dial: %v", err)
	}
	defer sess.CloseWithError(moqt.NoErrorCode, "Client shutting down")

	// Use the session (e.g., subscribe to tracks, receive announcements)

	// Gracefully shut down the client when done
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Shutdown(ctx); err != nil {
		log.Printf("Client shutdown error: %v", err)
	}
}
```
{{% /details %}}

## Initialize a Client

There is no dedicated function (such as a constructor) for initializing a client.
Users define the struct directly and assign values to its fields as needed.

```go
    client := moqt.Client{
		// Set client options here
	}
```

### Configuration

The following table describes the public fields of the `moqt.Client` struct:

| Field                  | Type                        | Description                                 |
|------------------------|-----------------------------|---------------------------------------------|
| `TLSConfig`            | [`*tls.Config`](https://pkg.go.dev/crypto/tls#Config) | TLS configuration for secure connections    |
| `QUICConfig`           | [`*quic.Config`](https://pkg.go.dev/github.com/okdaichi/gomoqt/quic#Config)              | QUIC protocol configuration                 |
| `Config`               | [`*moqt.Config`](https://pkg.go.dev/github.com/okdaichi/gomoqt/moqt#Config)                   | MOQ protocol configuration                  |
| `DialQUICFunc`         | [`quic.DialAddrFunc`](https://pkg.go.dev/github.com/okdaichi/gomoqt/quic#DialAddrFunc)         | Function to dial QUIC connection            |
| `DialWebTransportFunc` | [`webtransport.DialAddrFunc`](https://pkg.go.dev/github.com/okdaichi/gomoqt/webtransport#DialAddrFunc) | Function to dial WebTransport connection    |
| `Logger`               | [`*slog.Logger`](https://pkg.go.dev/log/slog#Logger)              | Logger for client events and errors         |

{{< tabs items="Using Default QUIC, Using Custom QUIC" >}}
{{< tab >}}

[`quic-go/quic-go`](https://github.com/quic-go/quic-go) is used internally as the default QUIC implementation when relevant fields which is set for customization are not set or `nil`.

{{<github-readme-stats user="quic-go" repo="quic-go" >}}

{{< /tab >}}
{{< tab >}}

To use a custom QUIC implementation, you need to provide your own implementation of the `quic.DialAddrFunc`. `(moqt.Client).DialQUICFunc` field is set, it is used to dial QUIC connections instead of the default implementation.

```go {filename="gomoqt/moqt/client.go",base_url="https://github.com/okdaichi/gomoqt/tree/main/moqt/client.go"}
type Client struct {
    // ...
	DialQUICFunc quic.DialAddrFunc
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

To use a custom WebTransport implementation, you need to provide your own implementation of the `webtransport.DialAddrFunc`. `(moqt.Client).DialWebTransportFunc` field is set, it is used to dial WebTransport connections instead of the default implementation.

```go {filename="gomoqt/moqt/client.go",base_url="https://github.com/okdaichi/gomoqt/tree/main/moqt/client.go"}
type Client struct {
    // ...
	DialWebTransportFunc webtransport.DialAddrFunc
    // ...
}
```
{{< /tab >}}

{{< /tabs >}}

## Dial and Establish Session

Clients can initiate a QUIC connection and establish a MOQ session with a server using the `(*moqt.Client).Dial` method.

```go
	var mux *TrackMux
	sess, err := client.Dial(ctx, "https://[addr]:[port]/[path]", mux)
	if err != nil {
		// Handle error
		return
	}

	// Handle session
```

> [!NOTE] Note: Nil TrackMux
> When `mux` is set to `nil`, the `moqt.DefaultTrackMux` will be used by default.
> Ensure that the `mux` is properly configured for your use case to avoid unexpected behavior.

## Shutting Down a Client

Clients can shut down in two ways: immediate or graceful.

### Immediate Shutdown

`(moqt.Client).Close` method forcefully terminates all sessions and releases resources. Use cautiously as it may interrupt operations.

```go
    client.Close() // Immediate shutdown
```

### Graceful Shutdown

`(moqt.Client).Shutdown` method waits for sessions to close naturally, or forces termination on timeout.

```go
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    err := client.Shutdown(ctx)
    if err != nil {
        // Handle forced termination
    }
```

> [!NOTE] Note: GOAWAY message
> The current implementation does not send a GOAWAY message during shutdown. Immediate session closure occurs when the context is canceled. This will be updated once the GOAWAY message specification is finalized.

## 📝 Future Work

- Implement GOAWAY message: (#XXX)