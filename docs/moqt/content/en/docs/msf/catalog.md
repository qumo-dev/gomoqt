---
title: Catalog
weight: 1
---

A `Catalog` represents an independent MSF catalog snapshot — the complete description of all tracks available in a broadcast.

## Parse and Validate

```go
    catalog, err := msf.ParseCatalog(rawJSON)
    if err != nil {
        // invalid JSON or wrong catalog shape
    }
    if err := catalog.Validate(); err != nil {
        // syntactically valid, but violates MSF rules
    }
```

`ParseCatalog` decodes an independent MSF catalog from JSON. `Validate` checks that the catalog satisfies draft-00 constraints (required fields, unique track identities, etc.).

## `msf.Catalog`

```go
type Catalog struct {
    DefaultNamespace string
    Version          int
    GeneratedAt      *int64
    IsComplete       bool
    Tracks           []Track
}

func ParseCatalog(data []byte) (Catalog, error)
func ParseCatalogString(s string) (Catalog, error)
func (c Catalog) Validate() error
func (c Catalog) Clone() Catalog
func (c Catalog) ApplyDelta(delta CatalogDelta) (Catalog, error)
```

| Field              | Type       | Description                                                    |
|--------------------|------------|----------------------------------------------------------------|
| `DefaultNamespace` | `string`   | Namespace implied by the catalog track (not serialized in JSON)|
| `Version`          | `int`      | MSF version number                                             |
| `GeneratedAt`      | `*int64`   | Catalog generation timestamp (optional)                        |
| `IsComplete`       | `bool`     | Whether the catalog is marked as complete                      |
| `Tracks`           | `[]Track`  | Complete set of track descriptions                             |

## Track Description

Each track in the catalog is described by an `msf.Track`:

| Field           | Type        | Description                                        |
|-----------------|-------------|----------------------------------------------------|
| `Namespace`     | `string`    | Track namespace (inherited from catalog if empty)  |
| `Name`          | `string`    | Track name, unique within its namespace            |
| `Packaging`     | `Packaging` | Packaging mode (see below)                         |
| `Role`          | `Role`      | Content role: `video`, `audio`, `caption`, etc.    |
| `Codec`         | `string`    | Codec string (e.g. `"avc1.64001f"`)               |
| `MimeType`      | `string`    | MIME type for the track's data payload             |
| `Bitrate`       | `*int64`    | Bitrate in bps (optional)                          |
| `Width`         | `*int64`    | Video width (optional)                             |
| `Height`        | `*int64`    | Video height (optional)                            |
| `Framerate`     | `*int64`    | Frame rate (optional)                              |
| `SampleRate`    | `*int64`    | Audio sample rate (optional)                       |
| `ChannelConfig` | `string`    | Audio channel configuration (optional)             |
| `Label`         | `string`    | Human-readable label (optional)                    |
| `Language`      | `string`    | Language tag (optional)                            |
| `Depends`       | `[]string`  | Track names this track depends on (optional)       |
| `IsLive`        | `*bool`     | Whether the track is live (optional)               |
| `TargetLatency` | `*int64`    | Target latency in milliseconds (optional)          |
| `RenderGroup`   | `*int64`    | Render group for synchronized playback (optional)  |
| `AltGroup`      | `*int64`    | Alternate group for quality switching (optional)   |
| `InitData`      | `string`    | Base64-encoded initialization data (optional)      |
| `TrackDuration` | `*int64`    | Duration in milliseconds (optional, not for live)  |

Many optional fields use pointer types (`*int64`, `*bool`) to distinguish "absent" from "zero value".

### Packaging Modes

| Constant                      | Value              | Description                        |
|-------------------------------|--------------------|------------------------------------|
| `msf.PackagingLOC`           | `"loc"`            | LOC-packaged media content         |
| `msf.PackagingMediaTimeline` | `"mediatimeline"`  | Media timeline track               |
| `msf.PackagingEventTimeline` | `"eventtimeline"`  | Event timeline track               |
| `msf.PackagingCMAF`          | `"cmaf"`           | CMAF-packaged media content        |
| `msf.PackagingLegacy`        | `"legacy"`         | Legacy timestamp+payload packaging |

### Roles

| Constant                      | Value                | Description                 |
|-------------------------------|----------------------|-----------------------------|
| `msf.RoleVideo`              | `"video"`            | Video track                 |
| `msf.RoleAudio`              | `"audio"`            | Audio track                 |
| `msf.RoleAudioDescription`   | `"audiodescription"` | Audio description track     |
| `msf.RoleCaption`            | `"caption"`          | Caption track               |
| `msf.RoleSubtitle`           | `"subtitle"`         | Subtitle track              |
| `msf.RoleSignLanguage`       | `"signlanguage"`     | Sign language video track   |

## Validation

`Validate()` returns `*msf.ValidationError` containing a list of problems:

```go
    if err := catalog.Validate(); err != nil {
        var ve *msf.ValidationError
        if errors.As(err, &ve) {
            for _, problem := range ve.Problems {
                fmt.Println(problem)
            }
        }
    }
```
