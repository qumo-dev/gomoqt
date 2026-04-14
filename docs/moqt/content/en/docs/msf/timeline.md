---
title: Timeline
weight: 3
---

MSF defines timeline records that map media timestamps to MoQ group/object locations.

## Location

```go
type Location struct {
    GroupID  uint64
    ObjectID uint64
}
```

A `Location` identifies a specific object within a broadcast by group and object ID. In JSON, it is encoded as a two-element array `[groupID, objectID]`.

## Media Timeline Entry

A media timeline track (`packaging: "mediatimeline"`) contains records that map media presentation timestamps to object locations and wallclock times.

```go
type MediaTimelineEntry struct {
    MediaTime int64
    Location  Location
    Wallclock int64
}
```

| Field       | Type       | Description                                          |
|-------------|------------|------------------------------------------------------|
| `MediaTime` | `int64`    | Media presentation timestamp                         |
| `Location`  | `Location` | The group and object where this timestamp's data lives |
| `Wallclock` | `int64`    | Wallclock time when the data was produced            |

JSON encoding: `[mediaTime, [groupID, objectID], wallclock]`.

## Event Timeline Record

An event timeline track (`packaging: "eventtimeline"`) contains records for application-defined events. Each record is indexed by exactly one of three selectors and carries a JSON data payload.

```go
type EventTimelineRecord struct {
    Wallclock *int64
    Location  *Location
    MediaTime *int64
    Data      json.RawMessage
}

func (r EventTimelineRecord) Validate() error
```

| Field       | Type              | Description                                    |
|-------------|-------------------|------------------------------------------------|
| `Wallclock` | `*int64`          | Wallclock time index (`"t"` in JSON)           |
| `Location`  | `*Location`       | Object location index (`"l"` in JSON)          |
| `MediaTime` | `*int64`          | Media time index (`"m"` in JSON)               |
| `Data`      | `json.RawMessage` | Event payload (`"data"` in JSON)               |

Exactly one of `Wallclock`, `Location`, or `MediaTime` must be set. `Validate()` checks this constraint.

### Example

```go
    // Event indexed by wallclock time
    record := msf.EventTimelineRecord{
        Wallclock: ptr(int64(1700000000000)),
        Data:      json.RawMessage(`{"type":"ad","duration":30}`),
    }

    if err := record.Validate(); err != nil {
        // handle error
    }

    data, _ := json.Marshal(record)
    // {"t":1700000000000,"data":{"type":"ad","duration":30}}
```
