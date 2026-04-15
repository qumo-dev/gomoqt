---
linkTitle: Produce a Track
title: Produce a Track
weight: 5
---

Producing a track involves writing media data to a `moqt.TrackWriter`, which manages the creation of groups and frames for transmission.
`moqt.TrackWriter` is responsible for handling the track's lifecycle, including opening and closing groups.
`moqt.TrackWriter` is created when being subscribed to a track from the peer on the session. Created `moqt.TrackWriter` is then routed via the session's `moqt.TrackMux` and can be used to publish media data.

{{% details title="Overview" closed="true" %}}

```go
    mux := moqt.NewTrackMux(0)

    // Set the mux to a session
    // This differs from if it is client-side or server-side

    mux.PublishFunc(ctx, "/broadcast_path", func(tw *moqt.TrackWriter) {
        defer tw.Close() // Always close when done

        for {
            gw, err := tw.OpenGroup() // Create a new group
            if err != nil {
                // Handle error
                return
            }

            frame := moqt.NewFrame(1024)
            payload := []byte("sample data")
            frame.Write(payload)

            err = gw.WriteFrame(frame)
            if err != nil {
                // Handle error
                return
            }
        }
    })
```

{{% /details %}}

## Handle Track Subscriptions

When the other side (client or server) subscribes to a broadcast path, the registered `moqt.TrackHandler` is invoked to serve the subscription. The handler receives a `moqt.TrackWriter` and is responsible for sending the appropriate track data for the requested track name.

{{< cards >}}
    {{< card link="../publish/#handle-track-subscriptions" title="Handle Track Subscriptions" icon="external-link">}}
{{</cards>}}

```go
    var mux *moqt.TrackMux

    // Register the track handler with PublishFunc
    mux.PublishFunc(ctx, "/broadcast_path", func(tw *moqt.TrackWriter) {
        defer tw.Close() // Always close when done

        // Handle track subscription
    })
```

```go
    // trackHandler implements moqt.TrackHandler
    var _ moqt.TrackHandler = (*trackHandler)(nil)
    type trackHandler struct{}
    func (h *trackHandler) ServeTrack(ctx context.Context, tw *moqt.TrackWriter) {
        defer tw.Close() // Always close when done

        // Handle track subscription
    }

    var mux *moqt.TrackMux

    // Register the track handler
    mux.Publish(ctx, "/broadcast_path", trackHandler{})

    // Register the track handler using an Announcement
    var ann *moqt.Announcement
    mux.Announce(ann, trackHandler{})
```

## Create a Group

To start a new group within a track, use `TrackWriter.OpenGroup` method. This creates a new group with an automatically incremented sequence number and returns a `moqt.GroupWriter` for writing frames to that group.

For explicit sequence control, use `TrackWriter.OpenGroupAt` method with a specific `moqt.GroupSequence`.


```go
    var tw *moqt.TrackWriter
    gw, err := tw.OpenGroup() // Auto-incrementing sequence
    if err != nil {
        // Handle error
        return
    }
    defer gw.Close() // Always close when done
    // Use gw to write frames
```

```go
    // Or use OpenGroupAt for explicit sequence control
    gw, err := tw.OpenGroupAt(moqt.GroupSequence(42))
    if err != nil {
        // Handle error
        return
    }
```

> [!NOTE] Note: Group Ordering
> Groups are supposed to be produced in order, with each group having a unique increasing sequence number and then consumed in order by the subscriber.
> However, groups not in order can be created, but this may lead to complications in playback and synchronization.


> [!NOTE] Note: Sequence Number 0
> Sequence number 0 has a special meaning: it is reserved for special identifiers like "Latest Available Group" or "Final Group", and does not represent a real group.

## Write Frames

To add media data to a group, use `GroupWriter.WriteFrame` method. Each frame represents a chunk of media data (audio, video, etc.) to be sent as part of the group.

```go
    var gw *moqt.GroupWriter
    frame := moqt.NewFrame(0)
    payload := []byte("sample payload")
    frame.Write(payload) // Populate payload
    err := gw.WriteFrame(frame)
    if err != nil {
        // Handle error
    }
```

### Frame Creation

Create frames with `moqt.NewFrame(capacity)`, reuse the buffer with `Reset`, and append payload via `Write` or by manipulating `Body()` when needed.

```go
    frame := moqt.NewFrame(1024)
    frame.Write([]byte("Good Morning"))
    fmt.Println(string(frame.Body())) // "Good Morning"

    frame.Reset() // Reuse buffer for next frame
    frame.Write([]byte("Good Evening"))
    fmt.Println(string(frame.Body())) // "Good Evening"
```

Frames are reused for efficiency; clone with `frame.Clone()` when you need to retain a copy after the next write.

> Relays MUST NOT combine, split, or otherwise modify object payloads.<br>
> — <cite>MOQT WG[^1]</cite>
[^1]: [IETF Draft - The Media Over QUIC Transport (moqtransport)](https://www.ietf.org/archive/id/draft-ietf-moq-transport-13.html)

## Finalize a Group

To finalize a group and indicate that no more frames will be sent, call `GroupWriter.Close` method.

```go
    var gw *moqt.GroupWriter
    gw.Close()
```

## Cancel Group Writing

To cancel a group and stop sending frames, call `GroupWriter.CancelWrite` method with an error code.

```go
    var group *moqt.GroupWriter
    var code moqt.GroupErrorCode
    group.CancelWrite(code)
```

## Stop a Track

To stop a track and indicate that no more groups will exist, call `TrackWriter.Close` method.

```go
    var tw *moqt.TrackWriter
    tw.Close()
```

To abort a track and indicate that no more groups will exist due to an error, call `TrackWriter.CloseWithError` method with an error code.

```go
    var tw *moqt.TrackWriter
    var code moqt.SubscribeErrorCode
    tw.CloseWithError(code)
```

- **Subscribe Error Code**:
  The subscribe error code is used to indicate the reason for closing the track subscription. It helps the publisher understand why the subscription finished.

If any groups belong to the track, they will be closed automatically with an error code `moqt.PublishAbortedErrorCode` when the track is closed. This ensures that all resources are released properly and no data is lost.

