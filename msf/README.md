# `msf` package

## Overview

Package `msf` implements the MOQT Streaming Format catalog and timeline data model from [draft-ietf-moq-msf-02](https://datatracker.ietf.org/doc/html/draft-ietf-moq-msf-02).

It focuses on:

- parsing independent catalogs and catalog deltas
- validating MSF catalog constraints
- applying delta updates to a base catalog
- encoding and decoding timeline record formats (including media-timeline templates)
- modeling draft-02 catalog features: publish tracks, initialization data list (inline, track-property and object-property), packaging metadata and LOC stream mapping, encryption metadata, authorization info, accessibility descriptors, accessible window and keyframe spacing, catalog references, and variable substitution
- the draft-02 required fields per (packaging, role), namespace and name rules (§5.2.2, §11.2), and depends resolution
- optionally exposing a small catalog-aware `moqt.TrackHandler` via `Broadcast`

Most of the package is transport-agnostic. The networking primitives themselves still live in [`../moqt`](../moqt/).

## Installation

```go
import "github.com/qumo-dev/gomoqt/msf"
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
broadcast, err := msf.NewBroadcast(msf.Catalog{Version: msf.CurrentVersion})
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
- `TrackRef` — minimal track identity used by a `remove` operation
- `TrackClone` — `clone` operation with `parentName`/`parentNamespace` and overrides
- `TrackUpdate` — `update` operation: `parentName`/`parentNamespace` of an existing track and the fields to override
- `Dependency` — `depends` entry: a track name or a render group
- `InitDataRef` — entry in the catalog Initialization Data List, referenced by track `initRef`
- `Template` — media-timeline template for fixed-duration segments
- `MediaTimelineEntry` — media-time to object-location mapping
- `EventTimelineRecord` — event timeline record with a single index selector
- `Broadcast` — optional helper that serves the reserved catalog track and routes registered track handlers

## Notes

- Optional catalog fields use pointer types where the package needs to preserve the difference between “field omitted” and “field explicitly set to zero”.
- Unknown JSON properties are preserved in `ExtraFields` so catalogs can be round-tripped without dropping extensions.
- `Catalog` and `CatalogDelta` are intentionally separate types so independent snapshots and incremental updates cannot be confused accidentally.
- A delta must carry `generatedAt`. Every catalog operation is checked against the base: adding or cloning to an existing track, and removing or updating an unknown one, rejects the delta.
- Variable substitution is opt-in: `ParseFragmentVariables` reads the variables of the catalog URI fragment, and `Catalog.ResolveVariables` substitutes them and validates the result, as draft-02 requires. It does not substitute into `connectionURI`.
- `WalkCatalogRefs` follows catalog references with cycle detection and `CatalogWalkLimits` on depth, catalog count and references per catalog.
- A `Broadcast` keeps its catalog valid: `RegisterTrack` and `RemoveTrack` refuse a change that would break a `depends` reference, so tracks register in dependency order and dependents are removed first.
- `Broadcast` routes non-catalog tracks by `Track.Name`, so it rejects catalogs that reuse the same name in multiple namespaces, even though MSF only requires Full Track Names to be unique.
- `PackagingCMAF` and `PackagingLegacy` are non-spec packaging values kept for compatibility.
- A `Broadcast` catalog subscription stays open: it gets the current catalog as its first group, then a new group with the whole catalog each time `SetCatalog`, `RegisterTrack` or `RemoveTrack` changes it. `Close` ends the catalog subscriptions and closes the registered tracks' writers.

## References

- [MSF draft-ietf-moq-msf-02](https://datatracker.ietf.org/doc/html/draft-ietf-moq-msf-02)
- [CMSF draft-ietf-moq-cmsf-01](https://datatracker.ietf.org/doc/html/draft-ietf-moq-cmsf-01) (non-spec extension; not part of MSF-02)
- [Core `moqt` package](../moqt/)
- [Root project README](../README.md)
