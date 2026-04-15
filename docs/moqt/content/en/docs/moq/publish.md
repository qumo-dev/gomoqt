---
title: Publish
weight: 7
---

Publishing is a mechanism for responding to subscriptions initiated by the other side (either client or server).

You register a `TrackHandler` with a `TrackMux` for each broadcast path. When a subscription request is received for a given track name, the handler is invoked and is responsible for sending the appropriate track data.

## Publish and Announce Broadcasts

To make a broadcast available for subscription, use one of the following methods on a `TrackMux`:

- `Publish`: Registers a `TrackHandler` for a specific path. Internally, it creates an active `Announcement` and delegates to `Announce`.
- `PublishFunc`: Registers a function as a handler for a specific path (convenience wrapper for `Publish`).
- `Announce`: Registers a handler and an explicit `Announcement` object. The `Announcement` must be active, otherwise the handler is not registered and no announcement is sent.

When you publish or announce a broadcast, an announcement is sent internally to notify other participants that the broadcast exists and is active. The announcement is distributed via the announcement tree and channels managed by the mux.


## Handle Track Subscriptions

When the other side (client or server) subscribes to a broadcast path, the registered `TrackHandler` is invoked to serve the subscription. The handler receives a `TrackWriter` and is responsible for sending the appropriate track data for the requested track name.

**Registering a TrackHandler with `Publish`**:

```go
mux := moqt.NewTrackMux(0)
var trackHandler moqt.TrackHandler
mux.Publish(ctx, "/broadcast_path", trackHandler)
```

**Using `PublishFunc`**:
```go
    mux := moqt.NewTrackMux(0)
    mux.PublishFunc(ctx, "/broadcast_path", func(tw *moqt.TrackWriter) {
        defer tw.Close() // Always close when done

        // Handle track subscription
    })
```

- **Context**:
  The context is used to manage the lifecycle of the publication and can be cancelled to stop the broadcast.

- **Broadcast Path**:
  The broadcast path is a unique identifier for the broadcast stream.

- **Track Handler**:
  The track handler is a function that processes incoming track data for a specific broadcast path.


## Announcements and Track Discovery

When you call `Publish`, `PublishFunc`, or `Announce`, a `moqt.Announcement` is initialized (or provided), and the announcement is sent to all registered channels in the announcement tree.

This ensures that all participants are aware of available broadcasts and their status. If the announcement ends (for example, if the context is cancelled), the mux cleans up the handler and announcement.

For more details, see the [Announce & Discover](announce_discover.md) documentation.

## Producing a Track

{{< cards >}}
	{{< card link="../track_group_frame/#produce-a-track" title="Produce a Track" icon="upload" subtitle="How to produce a track." >}}
{{< /cards >}}

## Cancel a Publication

To stop serving a broadcast, cancel the context used for publishing. This will terminate the publication, trigger the announcement's end handler, and release any associated resources.

**Cancelling the Context**:

```go
ctx, cancel := context.WithCancel(context.Background())
mux.Publish(ctx, "/broadcast_path", trackHandler)
// Later, when you want to stop serving:
cancel() // This ends the announcement and removes the handler
```