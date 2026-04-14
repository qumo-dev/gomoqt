---
title: Broadcast
weight: 4
---

`msf.Broadcast` is an optional helper that integrates an MSF catalog with `moqt.TrackHandler` routing. It manages the catalog snapshot and per-track handlers together, automatically serving the catalog track and dispatching subscriptions to the appropriate handler.

## Create a Broadcast

```go
    catalog := msf.Catalog{
        Version: 1,
        Tracks: []msf.Track{
            {Name: "video", Packaging: msf.PackagingLOC, Role: msf.RoleVideo},
            {Name: "audio", Packaging: msf.PackagingLOC, Role: msf.RoleAudio},
        },
    }

    broadcast, err := msf.NewBroadcast(catalog)
    if err != nil {
        // handle error
    }
```

The catalog is validated before the broadcast is created. You can also start with a zero-value `Broadcast` and call `SetCatalog` later.

## `msf.Broadcast`

```go
func NewBroadcast(catalog Catalog) (*Broadcast, error)
func (b *Broadcast) Catalog() Catalog
func (b *Broadcast) CatalogBytes() ([]byte, error)
func (b *Broadcast) CatalogTrackName() moqt.TrackName
func (b *Broadcast) SetCatalog(catalog Catalog) error
func (b *Broadcast) RegisterTrack(track Track, handler moqt.TrackHandler) error
func (b *Broadcast) RemoveTrack(name moqt.TrackName) bool
func (b *Broadcast) Handler(name moqt.TrackName) moqt.TrackHandler
func (b *Broadcast) ServeTrack(tw *moqt.TrackWriter)
```

## Register Tracks

```go
    err := broadcast.RegisterTrack(
        msf.Track{Name: "video", Packaging: msf.PackagingLOC, Role: msf.RoleVideo},
        videoHandler,
    )
```

`RegisterTrack` adds a track to the catalog and associates a `moqt.TrackHandler` with it. If a track with the same name already exists, it is replaced. The catalog is validated after modification.

The reserved catalog track name (`"catalog"` by default) cannot be used.

## Remove Tracks

```go
    removed := broadcast.RemoveTrack("old-video")
```

`RemoveTrack` removes the named track and its handler from the broadcast. Returns `true` if the track was present.

## Use as TrackHandler

`msf.Broadcast` implements `moqt.TrackHandler`, so it can be published directly:

```go
    mux := moqt.DefaultMux
    mux.Publish("/live", broadcast)
```

Subscriptions to the catalog track name are served automatically with the current catalog snapshot serialized as JSON. All other subscriptions are routed to the registered per-track handler.

## Update Catalog

```go
    err := broadcast.SetCatalog(newCatalog)
```

`SetCatalog` replaces the catalog snapshot atomically. Tracks present in the old catalog but absent in the new one are automatically removed along with their handlers.

## Catalog Track

The catalog is served as a special track (default name: `"catalog"`). When a subscriber requests this track, the current catalog snapshot is serialized as JSON and written as a single group with one frame.

```go
    name := broadcast.CatalogTrackName() // "catalog"
```

The default catalog track name is `msf.DefaultCatalogTrackName`.
