---
title: Announce & Discover
weight: 8
---

Announcements notify which broadcasts exist and whether they are active. They are essential for track discovery in a session. You can send announcements from a `moqt.Session` by using the `TrackMux.Publish`, `TrackMux.PublishFunc`, or `TrackMux.Announce` methods on the associated `moqt.TrackMux`.

When a track is published on a `moqt.TrackMux`, it distributes `Announcement` to all listeners.


## Announce Broadcasts

Broadcasts are announced when they are registered with the `moqt.TrackMux`. This can be done using the `TrackMux.Announce` method, or by using `TrackMux.Publish` or `TrackMux.PublishFunc`, which internally create the announcement.

```go
    var mux *moqt.TrackMux
    var sess *moqt.Session // Holds the mux

    // Register and announce a broadcast
    ann, end := NewAnnouncement(ctx, "/broadcast_path")
    mux.Announce(ann, trackHandler)
    defer end() // Cleanup when done

    // Or use Publish and PublishFunc (serves Announcement internally)
    mux.Publish(ctx, "/broadcast_path", trackHandler)
    mux.PublishFunc(ctx, "/broadcast_path", trackHandleFunc)
```

## Discover Broadcasts

Peers can discover available broadcasts by specifying the prefix for the broadcast path they are interested in and listening for announcements.

To be able to listen for announcements with a specific prefix, use the `Session.AcceptAnnounce` method. This returns a `moqt.AnnouncementReader`, which allows you to read incoming announcements.

```go
var sess *moqt.Session

ar, err := sess.AcceptAnnounce("/prefix/")
if err != nil {
    // Handle error
}

for {
    ann, err := ar.ReceiveAnnouncement(context.Background())
    if err != nil {
        // Handle error
        break
    }
    // Handle announcement
    fmt.Println("Broadcast:", ann.BroadcastPath())
    fmt.Println("HopIDs:", ann.HopIDs()) // List of relay hop IDs the announcement traversed
}
```

> [!NOTE] Note: Loop Avoidance
> When `AcceptAnnounce` is called, the `TrackMux`'s hop ID is automatically sent as `ExcludeHop` in the ANNOUNCE_INTEREST message. This prevents announcement loops in relay topologies. See [Relay — Hop ID and Loop Avoidance](../relay/#hop-id-and-loop-avoidance) for details.
