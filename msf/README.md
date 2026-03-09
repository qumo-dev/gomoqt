# `msf` package

## Overview

Package `msf` implements the MOQT Streaming Format catalog and timeline data model from [draft-ietf-moq-msf-00](https://datatracker.ietf.org/doc/html/draft-ietf-moq-msf-00).

It focuses on:

- parsing independent catalogs and catalog deltas
- validating MSF catalog constraints
- applying delta updates to a base catalog
- encoding and decoding timeline record formats
- optionally exposing a small catalog-aware `moqt.TrackHandler` via `Broadcast`

Most of the package is transport-agnostic. The networking primitives themselves still live in [`../moqt`](../moqt/).

## Installation

```go
import "github.com/okdaichi/gomoqt/msf"
```

## Usage

### Parse and validate a catalog

```go
catalog, err := msf.ParseCatalog(raw)
if err != nil {
	// invalid JSON or wrong catalog shape
}
if err := catalog.Validate(); err != nil {
	// syntactically valid, but violates MSF rules
}
```

### Apply a catalog delta

```go
base, err := msf.ParseCatalog(baseJSON)
if err != nil {
	// handle error
}

delta, err := msf.ParseCatalogDelta(deltaJSON)
if err != nil {
	// handle error
}

updated, err := base.ApplyDelta(delta)
if err != nil {
	// handle validation or merge error
}
```

### Serve a catalog track with `Broadcast`

```go
broadcast, err := msf.NewBroadcast(msf.Catalog{Version: 1})
if err != nil {
	// handle error
}

err = broadcast.RegisterTrack(msf.Track{
	Name:      "video",
	Packaging: msf.PackagingLOC,
	IsLive:    new(bool),
}, myHandler)
if err != nil {
	// handle error
}
```

## Main types

- `Catalog` — independent MSF catalog snapshot
- `CatalogDelta` — incremental update for an existing catalog
- `Track` — track entry in an independent catalog or add operation
- `TrackRef` — minimal track identity used by `removeTracks`
- `TrackClone` — clone operation with `parentName` and overrides
- `MediaTimelineEntry` — media-time to object-location mapping
- `EventTimelineRecord` — event timeline record with a single index selector
- `Broadcast` — optional helper that serves the reserved catalog track and routes registered track handlers

## Notes

- Optional catalog fields use pointer types where the package needs to preserve the difference between “field omitted” and “field explicitly set to zero”.
- Unknown JSON properties are preserved in `ExtraFields` so catalogs can be round-tripped without dropping extensions.
- `Catalog` and `CatalogDelta` are intentionally separate types so independent snapshots and incremental updates cannot be confused accidentally.
- `Broadcast` routes non-catalog tracks by `Track.Name`, so it rejects catalogs that reuse the same name in multiple namespaces.

## References

- [MSF draft-ietf-moq-msf-00](https://datatracker.ietf.org/doc/html/draft-ietf-moq-msf-00)
- [CMSF draft-ietf-moq-cmsf-00](https://datatracker.ietf.org/doc/html/draft-ietf-moq-cmsf-00)
- [Core `moqt` package](../moqt/)
- [Root project README](../README.md)
