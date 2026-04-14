---
title: MSF
weight: 6
---

MSF (MOQT Streaming Format, [draft-ietf-moq-msf-00](https://datatracker.ietf.org/doc/html/draft-ietf-moq-msf-00)) defines how media content is organized, described, and delivered over the MoQ transport protocol.

While MoQ itself provides the transport primitives — broadcasts, tracks, groups, and frames — MSF defines the standard format for describing what media is available and how to consume it.

```go
import "github.com/okdaichi/gomoqt/msf"
```

## Catalog

A **catalog** is a JSON document that describes the complete set of tracks available in a broadcast. Subscribers read the catalog to discover available media before opening individual subscriptions.

A catalog describes:
- **Track identities** — namespace and name for each track
- **Media parameters** — codec, bitrate, resolution, sample rate, etc.
- **Packaging mode** — how media data is packed into MoQ groups and frames (LOC, CMAF, media timeline, event timeline)
- **Content roles** — video, audio, caption, subtitle, etc.
- **Dependencies** — which tracks depend on other tracks (e.g., enhancement layers)

Catalogs are delivered as a reserved track (typically named `"catalog"`) within the broadcast, so they can be subscribed to like any other track.

{{< cards >}}
    {{< card link="catalog/" title="Catalog" icon="document-text" subtitle="Parse, validate, and inspect catalog snapshots" >}}
    {{< card link="catalog_delta/" title="Catalog Delta" icon="refresh" subtitle="Incremental catalog updates" >}}
{{< /cards >}}

## Timeline

MSF defines **timeline records** that map media timestamps to MoQ group/object locations:

- **Media Timeline** — maps media presentation timestamps to `[groupID, objectID]` locations with wallclock times
- **Event Timeline** — records events indexed by wallclock time, object location, or media time, carrying arbitrary JSON payloads

{{< cards >}}
    {{< card link="timeline/" title="Timeline" icon="clock" subtitle="Location, media timeline, and event timeline records" >}}
{{< /cards >}}

## Broadcast

`msf.Broadcast` is an optional helper that integrates an MSF catalog with `moqt.TrackHandler` routing, managing the catalog snapshot and per-track handlers together.

{{< cards >}}
    {{< card link="broadcast/" title="Broadcast" icon="external-link" subtitle="Catalog-aware track handler routing" >}}
{{< /cards >}}
