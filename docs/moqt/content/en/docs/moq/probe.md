---
title: Probe
weight: 11
---

Probe is a mechanism for measuring the available bitrate and round-trip time (RTT) between two peers. One peer sends a probe request with a local bitrate estimate, and the remote peer responds with its own bitrate and RTT information. This enables adaptive bitrate decisions without maintaining a persistent subscription.

## Probe Bitrate

To probe the remote peer's bitrate and RTT, use the `Session.Probe` method:

```go
    localBitrate := uint64(5_000_000) // 5 Mbps

    result, err := sess.Probe(localBitrate)
    if err != nil {
        // Handle error
        return
    }

    fmt.Printf("Remote bitrate: %d bps\n", result.Bitrate)
    fmt.Printf("RTT: %d ms\n", result.RTT)
```

The method opens a bidirectional stream, sends a `PROBE` message with the local bitrate, and reads the remote peer's response containing the remote bitrate and RTT.

### Parameters

| Parameter      | Type     | Description                              |
|----------------|----------|------------------------------------------|
| `bitrate`      | `uint64` | The local bitrate estimate in bits per second |

### Return Values

`Probe` returns a `*moqt.ProbeResult`:

| Field      | Type     | Description                                    |
|------------|----------|------------------------------------------------|
| `Bitrate`  | `uint64` | The remote peer's bitrate in bits per second. 0 means unknown. |
| `RTT`      | `uint64` | The smoothed round-trip time in milliseconds. 0 means unknown. |

## Error Handling

Probe operations may return `moqt.ProbeError` or `moqt.SessionError`:

```go
    result, err := sess.Probe(localBitrate)
    if err != nil {
        var probeErr *moqt.ProbeError
        if errors.As(err, &probeErr) {
            switch probeErr.ProbeErrorCode() {
            case moqt.ProbeErrorCodeNotSupported:
                // Remote peer does not support probing
            case moqt.ProbeErrorCodeTimeout:
                // Probe timed out
            default:
                // Internal error
            }
        }
    }
```

See [Built-in Error Codes](errors/#built-in-error-codes) for `ProbeErrorCode` values.
