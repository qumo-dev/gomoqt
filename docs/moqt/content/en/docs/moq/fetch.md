---
title: Fetch
weight: 10
---

Fetch is a mechanism for requesting a specific group from a track without maintaining a continuous subscription. It is primarily used to retrieve groups that were dropped by the publisher (e.g., cancelled via `DropGroups` or `DropNextGroups`) but may still be available in relay or cache nodes. This allows subscribers to recover recently dropped data and maintain playback continuity.

## Fetch a Group

To fetch a specific group, use the `Session.Fetch` method with a `moqt.FetchRequest`:

```go
    req := &moqt.FetchRequest{
        BroadcastPath: "/broadcast_path",
        TrackName:     "video",
        Priority:      moqt.TrackPriority(1),
        GroupSequence: moqt.GroupSequence(42),
    }

    gr, err := sess.Fetch(req)
    if err != nil {
        // Handle error
        return
    }
    defer gr.CancelRead(moqt.ExpiredGroupErrorCode)

    // Read frames from the fetched group
    frame := moqt.NewFrame(0)
    for {
        err := gr.ReadFrame(frame)
        if err != nil {
            break
        }
        // Process frame data
    }
```

### `moqt.FetchRequest`

```go
type FetchRequest struct {
    BroadcastPath BroadcastPath
    TrackName     TrackName
    Priority      TrackPriority
    GroupSequence GroupSequence
}

func (r *FetchRequest) Context() context.Context
func (r *FetchRequest) WithContext(ctx context.Context) *FetchRequest
func (r *FetchRequest) Clone(ctx context.Context) *FetchRequest
```

| Field            | Type             | Description                                               |
|------------------|------------------|-----------------------------------------------------------|
| `BroadcastPath`  | `BroadcastPath`  | The path of the broadcast to fetch from                   |
| `TrackName`      | `TrackName`      | The name of the track within the broadcast                |
| `Priority`       | `TrackPriority`  | Delivery priority for the fetch request                   |
| `GroupSequence`  | `GroupSequence`  | The specific group sequence number to fetch               |

### Context and Cancellation

The `FetchRequest` supports context-based cancellation. When the context is cancelled, the fetch stream is automatically cancelled:

```go
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    req := (&moqt.FetchRequest{
        BroadcastPath: "/broadcast_path",
        TrackName:     "video",
        GroupSequence: moqt.GroupSequence(42),
    }).WithContext(ctx)

    gr, err := sess.Fetch(req)
    if err != nil {
        // Handle error (including timeout)
        return
    }
```

## Handle Fetch Requests (Server Side)

To serve fetch requests, implement the `FetchHandler` interface and configure it on the server or dialer:

```go
type FetchHandler interface {
    ServeFetch(w *GroupWriter, r *FetchRequest)
}
```

The handler receives a `GroupWriter` to write frames and a `FetchRequest` describing what the client is requesting.

### Using `FetchHandlerFunc`

For simple handlers, use the convenience type `FetchHandlerFunc`:

```go
    handler := moqt.FetchHandlerFunc(func(w *moqt.GroupWriter, r *moqt.FetchRequest) {
        defer w.Close()

        // Look up the requested group data
        frame := moqt.NewFrame(0)
        frame.Write([]byte("frame data"))
        w.WriteFrame(frame)
    })
```

### Configuring FetchHandler

**On a Server (native QUIC):**

```go
    server := moqt.Server{
        FetchHandler: handler,
        // ...
    }
```

**On a Dialer (WebTransport):**

```go
    dialer := moqt.Dialer{
        FetchHandler: handler,
        // ...
    }
```

**On a WebTransportHandler:**

```go
    wtHandler := &moqt.WebTransportHandler{
        FetchHandler: handler,
        // ...
    }
```

## Error Handling

Fetch operations may return `moqt.FetchError` or `moqt.SessionError`. Use type assertions for specific handling:

```go
    gr, err := sess.Fetch(req)
    if err != nil {
        var fetchErr *moqt.FetchError
        if errors.As(err, &fetchErr) {
            // Handle fetch-specific error
            fmt.Println("Fetch error code:", fetchErr.FetchErrorCode())
        }

        var sessErr *moqt.SessionError
        if errors.As(err, &sessErr) {
            // Handle session-level error
        }
    }
```

See [Built-in Error Codes](errors/#built-in-error-codes) for `FetchErrorCode` values.
