---
title: Probe
weight: 11
---

Probe is a mechanism for measuring the available bitrate between two peers.
The subscriber sends a target bitrate hint to the publisher, and the publisher
periodically reports back the bitrate it actually measured on the connection.
This enables adaptive bitrate decisions without maintaining a persistent subscription.

### `moqt.ProbeResult`

Both `Session.Probe` and `Session.ProbeTargets` deliver results via `ProbeResult`:

```go
type ProbeResult struct {
    Bitrate uint64
}
```

| Field     | Type     | Description                                                    |
|-----------|----------|----------------------------------------------------------------|
| `Bitrate` | `uint64` | The measured bitrate in bits per second. 0 means unknown.      |

> **Note:** RTT is not included in `ProbeResult`.
> Use `Session.Stats()` or the underlying transport API (e.g. `(*quic.Conn).ConnectionStats()`) to obtain RTT, bytes sent, and bytes received.

## Notify Target Bitrate

Use `Session.Probe` to send a target bitrate hint to the publisher.
The call can be repeated to update the target; the same underlying stream is
reused. The returned channel is closed when the session terminates.

```go
func (s *Session) Probe(targetBitrate uint64) (<-chan ProbeResult, error)
```

```go
    probeCh, err := sess.Probe(5_000_000) // hint: 5 Mbps
    if err != nil {
        // Handle error
        return
    }

    result, ok := <-probeCh
    if !ok {
        // Stream closed without a result.
        return
    }
    fmt.Printf("Measured bitrate: %d bps\n", result.Bitrate)
```

## Receive Target Bitrate

Use `Session.ProbeTargets` to receive target bitrate hints sent by the subscriber.
The channel has a buffer of 1 and uses latest-value semantics — if the
previous value has not been consumed it is replaced by the newer one.

```go
func (s *Session) ProbeTargets() <-chan ProbeResult
```

```go
    for result := range sess.ProbeTargets() {
        fmt.Printf("Subscriber target: %d bps\n", result.Bitrate)
        // Adjust encoding bitrate accordingly.
    }
```

The publisher automatically enforces a single active incoming probe stream;
if the subscriber opens a new stream the previous one is cancelled.

## Configuring Probe Behaviour

The probe loop timing can be tuned via `moqt.Config`:

```go
    cfg := &moqt.Config{
        ProbeInterval: 50 * time.Millisecond,
        ProbeMaxAge:   5 * time.Second,
        ProbeMaxDelta: 0.05,
    }
```

| Field           | Type            | Default | Description                                                          |
|-----------------|-----------------|---------|----------------------------------------------------------------------|
| `ProbeInterval` | `time.Duration` | 100 ms  | Ticker period for the publisher-side probe loop.                     |
| `ProbeMaxAge`   | `time.Duration` | 10 s    | Maximum interval between probe sends regardless of bitrate change.   |
| `ProbeMaxDelta` | `float64`       | 0.10    | Fractional bitrate change threshold that triggers an early send.     |

## Error Handling

Probe operations may return `moqt.ProbeError` or `moqt.SessionError`:

```go
    probeCh, err := sess.Probe(5_000_000)
    if err != nil {
        var probeErr *moqt.ProbeError
        if errors.As(err, &probeErr) {
            switch probeErr.ProbeErrorCode() {
            case moqt.ProbeErrorCodeNotSupported:
                // Remote peer does not support probing
            default:
                // Internal error
            }
        }
    }
```

See [Built-in Error Codes](errors/#built-in-error-codes) for `ProbeErrorCode` values.
