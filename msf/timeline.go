package msf

import (
	"encoding/json"
	"fmt"
)

// Location identifies the precise position of an object within an MSF
// broadcast.  It consists of the group sequence number and the object ID
// inside that group.  The JSON representation is a two-element array
// [groupID, objectID].
type Location struct {
	GroupID  uint64
	ObjectID uint64
}

// MarshalJSON encodes the location as [groupID, objectID].  It is used by
// the default json package when a Location is serialized.  The reverse
// operation is implemented by UnmarshalJSON.
func (l Location) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]uint64{l.GroupID, l.ObjectID})
}

// UnmarshalJSON decodes the location from [groupID, objectID].  This
// method ensures that the compact array format in the MSF specification is
// accepted when reading JSON.
func (l *Location) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) != 2 {
		return fmt.Errorf("msf: location must contain exactly 2 items")
	}
	if err := json.Unmarshal(raw[0], &l.GroupID); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[1], &l.ObjectID); err != nil {
		return err
	}
	return nil
}

// MediaTimelineEntry represents a single record in a media timeline track.
// It maps a media timestamp to an object location and the corresponding
// wallclock time.  When encoded in JSON the structure is a three-element
// array [mediaTime, [groupID, objectID], wallclock].
type MediaTimelineEntry struct {
	MediaTime int64
	Location  Location
	Wallclock int64
}

// MarshalJSON encodes the media timeline entry as
// [mediaTime, [groupID, objectID], wallclock].
func (e MediaTimelineEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{e.MediaTime, e.Location, e.Wallclock})
}

// UnmarshalJSON decodes the media timeline entry from
// [mediaTime, [groupID, objectID], wallclock].
func (e *MediaTimelineEntry) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) != 3 {
		return fmt.Errorf("msf: media timeline entry must contain exactly 3 items")
	}
	if err := json.Unmarshal(raw[0], &e.MediaTime); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[1], &e.Location); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[2], &e.Wallclock); err != nil {
		return err
	}
	return nil
}

// EventTimelineRecord represents a single event in an event-timeline track.
// Exactly one of Wallclock, Location or MediaTime must be non‑nil, indicating
// which index is used for the record.  The Data field contains the event
// payload as raw JSON.  Any additional properties are preserved in
// ExtraFields.  The JSON encoding is an object with keys "t" (wallclock),
// "l" (location), "m" (mediaTime), "data" and optionally others.
type EventTimelineRecord struct {
	// one of the following three pointers is set to select the index type
	Wallclock *int64    `json:"-"`
	Location  *Location `json:"-"`
	MediaTime *int64    `json:"-"`
	// Data is the event payload; must be non-empty
	Data json.RawMessage `json:"-"`

	// ExtraFields holds unknown JSON properties for round-tripping.
	ExtraFields map[string]json.RawMessage `json:"-"`
}

// Validate checks whether the event timeline record contains exactly one index
// selector (wallclock, location, or mediaTime) and a non-empty data payload.
// It is used by callers that need to ensure a record meets the MSF draft’s
// integrity constraints before serializing or applying it.
func (r EventTimelineRecord) Validate() error {
	count := 0
	if r.Wallclock != nil {
		count++
	}
	if r.Location != nil {
		count++
	}
	if r.MediaTime != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("msf: event timeline record must contain exactly one of t, l, or m")
	}
	if len(r.Data) == 0 {
		return fmt.Errorf("msf: event timeline record must contain data")
	}
	return nil
}

// MarshalJSON encodes the event timeline record using the draft-00 JSON shape.
func (r EventTimelineRecord) MarshalJSON() ([]byte, error) {
	obj := make(map[string]any, len(r.ExtraFields)+2)
	for key, raw := range r.ExtraFields {
		obj[key] = append(json.RawMessage(nil), raw...)
	}
	if r.Wallclock != nil {
		obj["t"] = *r.Wallclock
	}
	if r.Location != nil {
		obj["l"] = *r.Location
	}
	if r.MediaTime != nil {
		obj["m"] = *r.MediaTime
	}
	if len(r.Data) > 0 {
		obj["data"] = append(json.RawMessage(nil), r.Data...)
	}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes the event timeline record using the draft-00 JSON shape.
func (r *EventTimelineRecord) UnmarshalJSON(data []byte) error {
	*r = EventTimelineRecord{ExtraFields: make(map[string]json.RawMessage)}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key, value := range raw {
		switch key {
		case "t":
			var v int64
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			r.Wallclock = &v
		case "l":
			var v Location
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			r.Location = &v
		case "m":
			var v int64
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			r.MediaTime = &v
		case "data":
			r.Data = append(json.RawMessage(nil), value...)
		default:
			r.ExtraFields[key] = append(json.RawMessage(nil), value...)
		}
	}

	return nil
}
