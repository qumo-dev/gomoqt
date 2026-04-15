---
title: Relay
weight: 12
---

## Relay a Track

By relaying media data from a source track to one or more destination tracks by servers, contents are delivered to a wider audience or users.
To forward media data, a server subscribes to a source track as a subscriber to upstream and handles one or more downstream subscriptions as a publisher.

{{% details title="Overview" closed="true" %}}

```go
    var src *moqt.TrackReader
    var dests []*moqt.TrackWriter

    for {
        gr, err := src.AcceptGroup(context.Background())
        if err != nil {
            break
        }

        go func(gr *moqt.GroupReader) {
            defer gr.Close()
            seq := gr.GroupSequence()

            writers := make([]*moqt.GroupWriter, 0, len(dests))
            for _, dest := range dests {
                gw, err := dest.OpenGroupAt(seq)
                if err != nil {
                    break
                }

                writers = append(writers, gw)
            }

            frame := moqt.NewFrame(0)
            for {
                err := gr.ReadFrame(frame)
                if err != nil {
                    if err == io.EOF {
                        for _, gw := range writers {
                            gw.Close()
                        }
                    } else {
                        // Handle error
                        for _, gw := range writers {
                            gw.CancelWrite(moqt.InternalGroupErrorCode)
                        }
                    }
                    break
                }

                for _, gw := range writers {
                    err = gw.WriteFrame(frame)
                    if err != nil {
                        break
                    }
                }
            }
        }(gr)
    }
```
{{% /details %}}

> [!TIP] Tip: The First Subscription
> Making the first subscription before downstream clients have subscribed can reduce latency, but may increase resource usage. This trade-off should be considered when designing your relay logic.
> When the track is a high-frequency track (e.g., video), it is recommended to make the first subscription after downstream clients subscribe to avoid unnecessary resource consumption.

## Relay Broadcasts

`TrackMux` acts as a hub for relaying broadcasts.

> [!NOTE] Note: Relay Implementation
> `gomoqt` does not provide built-in implementation for relaying broadcasts and tracks because there are many scenarios on relaying and many different implementations. The relay implementation is left to the user.

## Hop ID and Loop Avoidance

Relay nodes MUST use a unique non-zero hop ID to enable loop avoidance. When creating a `TrackMux` for a relay, use `moqt.NewHopID()` to generate a unique identifier:

```go
    mux := moqt.NewTrackMux(moqt.NewHopID())
    fmt.Printf("Relay HopID: %d\n", mux.HopID())
```

When the mux accepts an announce interest (`AcceptAnnounce`), it automatically sends an `ExcludeHop` with the mux's hop ID to prevent announcement loops. Each `Announcement` carries a list of hop IDs it has traversed, accessible via `Announcement.HopIDs`.

```go
    ann, err := ar.ReceiveAnnouncement(ctx)
    if err != nil {
        // Handle error
    }
    fmt.Printf("Announcement traversed hops: %v\n", ann.HopIDs())
```

For edge nodes (origin publishers or pure subscribers), pass `0` as the hop ID:

```go
    mux := moqt.NewTrackMux(0) // Edge node, no hop tracking
```

## Caching

To enhance UX, consider implementing caching strategies for frequently accessed data or long-lived objects. This can help reduce latency and improve overall performance. Some common caching techniques include:

1. **In-Memory Caching**: Store frequently accessed data in memory (RAM) for quick retrieval.
2. **Distributed Caching**: Use a distributed cache system to share cached data across multiple instances.
3. **Cache Invalidation**: Implement strategies to invalidate stale cache entries to ensure data consistency.

By leveraging caching, you can significantly improve the responsiveness of your application and provide a smoother user experience.

## 📝 Future Work

- Per-track Caching Management: (#XXX)