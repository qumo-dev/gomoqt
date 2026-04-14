---
title: Catalog Delta
weight: 2
---

A `CatalogDelta` represents an incremental update to an existing catalog. Instead of resending the full track list, a delta contains add, remove, and clone operations.

## Parse and Apply

```go
    delta, err := msf.ParseCatalogDelta(deltaJSON)
    if err != nil {
        // handle error
    }

    updated, err := baseCatalog.ApplyDelta(delta)
    if err != nil {
        // handle error
    }
```

## `msf.CatalogDelta`

```go
type CatalogDelta struct {
    DefaultNamespace string
    GeneratedAt      *int64
    IsComplete       bool
    AddTracks        []Track
    RemoveTracks     []TrackRef
    CloneTracks      []TrackClone
}

func ParseCatalogDelta(data []byte) (CatalogDelta, error)
func ParseCatalogDeltaString(s string) (CatalogDelta, error)
func (d CatalogDelta) Validate() error
func (d CatalogDelta) Clone() CatalogDelta
```

### Delta Operations

| Field          | Type           | Description                                        |
|----------------|----------------|----------------------------------------------------|
| `AddTracks`    | `[]Track`      | New tracks to append to the catalog                |
| `RemoveTracks` | `[]TrackRef`   | Tracks to remove (by namespace + name)             |
| `CloneTracks`  | `[]TrackClone` | New tracks derived from an existing parent track   |

The operation order declared in the original JSON is preserved during application.

### `TrackRef`

Identifies a track by namespace and name, used in `RemoveTracks`:

```go
type TrackRef struct {
    Namespace string
    Name      string
}
```

### `TrackClone`

Describes a clone operation — a new track derived from an existing parent with optional overrides:

```go
type TrackClone struct {
    Track               // override values
    ParentName string   // source track to clone from
}
```

## Example

```go
    base, _ := msf.ParseCatalog(baseJSON)

    delta, _ := msf.ParseCatalogDelta([]byte(`{
        "deltaUpdate": true,
        "addTracks": [
            {"namespace": "ns", "name": "subtitle", "packaging": "loc"}
        ],
        "removeTracks": [
            {"namespace": "ns", "name": "old-audio"}
        ]
    }`))

    updated, err := base.ApplyDelta(delta)
    if err != nil {
        // handle error
    }
```
