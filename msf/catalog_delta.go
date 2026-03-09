package msf

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// CatalogDelta represents a delta update for an existing MSF catalog.
//
// A delta catalog never carries a complete track list. Instead it contains one
// or more add/remove/clone operations plus optional metadata updates. The JSON
// form always includes "deltaUpdate": true.
type CatalogDelta struct {
	// DefaultNamespace, if set, replaces the base catalog namespace used for
	// resolving tracks whose namespace field is omitted.
	DefaultNamespace string `json:"-"`
	// GeneratedAt, if non-nil, records the delta's generatedAt timestamp.
	GeneratedAt *int64 `json:"-"`
	// IsComplete marks the updated catalog as complete.
	IsComplete bool `json:"-"`

	// AddTracks lists new track entries to append to the base catalog.
	AddTracks []Track `json:"-"`
	// RemoveTracks identifies tracks to remove from the base catalog.
	RemoveTracks []TrackRef `json:"-"`
	// CloneTracks lists clone operations derived from an existing parent track.
	CloneTracks []TrackClone `json:"-"`

	// ExtraFields stores unknown JSON properties for round-tripping.
	ExtraFields map[string]json.RawMessage `json:"-"`

	deltaOpOrder []deltaOperationKind
}

// TrackRef identifies a track by namespace and name.
//
// It is used where only the identity of a track is required, such as
// removeTracks entries inside a catalog delta.
type TrackRef struct {
	Namespace string `json:"-"`
	Name      string `json:"-"`

	ExtraFields map[string]json.RawMessage `json:"-"`
}

// TrackClone describes a cloneTracks entry in a catalog delta.
//
// The embedded Track provides the override values to apply to the cloned track.
// ParentName identifies the source track from which values are inherited.
type TrackClone struct {
	Track

	ParentName string `json:"-"`
}

// Clone returns a deep copy of the delta catalog.
func (d CatalogDelta) Clone() CatalogDelta {
	clone := d
	clone.AddTracks = cloneTracks(d.AddTracks)
	clone.RemoveTracks = cloneTrackRefs(d.RemoveTracks)
	clone.CloneTracks = cloneTrackClones(d.CloneTracks)
	clone.ExtraFields = cloneRawMessages(d.ExtraFields)
	clone.deltaOpOrder = slices.Clone(d.deltaOpOrder)
	return clone
}

// Validate checks whether the delta satisfies the package's MSF draft-00 rules.
func (d CatalogDelta) Validate() error {
	var problems []string

	if len(d.AddTracks) == 0 && len(d.RemoveTracks) == 0 && len(d.CloneTracks) == 0 {
		problems = append(problems, "delta catalog must contain addTracks, removeTracks, or cloneTracks")
	}
	for i, track := range d.AddTracks {
		problems = append(problems, track.validate(trackContextAdd, fmt.Sprintf("addTracks[%d]", i))...)
	}
	for i, track := range d.RemoveTracks {
		problems = append(problems, track.Validate(fmt.Sprintf("removeTracks[%d]", i))...)
	}
	for i, track := range d.CloneTracks {
		problems = append(problems, track.Validate(fmt.Sprintf("cloneTracks[%d]", i))...)
	}

	return newValidationError(problems)
}

// ParseCatalogDelta decodes an MSF catalog delta from JSON bytes.
func ParseCatalogDelta(data []byte) (CatalogDelta, error) {
	var delta CatalogDelta
	if err := json.Unmarshal(data, &delta); err != nil {
		return CatalogDelta{}, err
	}
	return delta, nil
}

// ParseCatalogDeltaString is like ParseCatalogDelta but accepts a string.
func ParseCatalogDeltaString(s string) (CatalogDelta, error) {
	return ParseCatalogDelta([]byte(s))
}

// operationOrder returns the declared delta operation order, preserving JSON order when known.
func (d CatalogDelta) operationOrder() []deltaOperationKind {
	if len(d.deltaOpOrder) > 0 {
		return slices.Clone(d.deltaOpOrder)
	}

	order := make([]deltaOperationKind, 0, 3)
	if len(d.AddTracks) > 0 {
		order = append(order, deltaOperationAdd)
	}
	if len(d.RemoveTracks) > 0 {
		order = append(order, deltaOperationRemove)
	}
	if len(d.CloneTracks) > 0 {
		order = append(order, deltaOperationClone)
	}
	return order
}

var (
	_ json.Marshaler   = CatalogDelta{}
	_ json.Unmarshaler = (*CatalogDelta)(nil)
	_ json.Marshaler   = TrackRef{}
	_ json.Unmarshaler = (*TrackRef)(nil)
	_ json.Marshaler   = TrackClone{}
	_ json.Unmarshaler = (*TrackClone)(nil)
)

// MarshalJSON encodes the delta in the draft-00 JSON form.
func (d CatalogDelta) MarshalJSON() ([]byte, error) {
	obj := make(map[string]any, len(d.ExtraFields)+6)
	for key, raw := range d.ExtraFields {
		obj[key] = cloneRawMessage(raw)
	}
	obj["deltaUpdate"] = true
	if d.GeneratedAt != nil {
		obj["generatedAt"] = *d.GeneratedAt
	}
	if d.IsComplete {
		obj["isComplete"] = true
	}
	if len(d.AddTracks) > 0 {
		obj["addTracks"] = d.AddTracks
	}
	if len(d.RemoveTracks) > 0 {
		obj["removeTracks"] = d.RemoveTracks
	}
	if len(d.CloneTracks) > 0 {
		obj["cloneTracks"] = d.CloneTracks
	}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes a delta catalog and rejects independent-catalog fields.
func (d *CatalogDelta) UnmarshalJSON(data []byte) error {
	*d = CatalogDelta{}
	d.ExtraFields = make(map[string]json.RawMessage)

	ordered, err := decodeOrderedObject(data)
	if err != nil {
		return err
	}

	sawDeltaUpdate := false
	for _, entry := range ordered {
		switch entry.Key {
		case "deltaUpdate":
			var value bool
			if err := json.Unmarshal(entry.Value, &value); err != nil {
				return err
			}
			if !value {
				return fmt.Errorf("msf: delta catalog must include deltaUpdate=true")
			}
			sawDeltaUpdate = true
		case "version", "tracks":
			return fmt.Errorf("msf: independent catalog fields are not allowed in a delta catalog")
		case "generatedAt":
			var value int64
			if err := json.Unmarshal(entry.Value, &value); err != nil {
				return err
			}
			d.GeneratedAt = &value
		case "isComplete":
			if err := json.Unmarshal(entry.Value, &d.IsComplete); err != nil {
				return err
			}
		case "addTracks":
			if err := json.Unmarshal(entry.Value, &d.AddTracks); err != nil {
				return err
			}
			d.deltaOpOrder = append(d.deltaOpOrder, deltaOperationAdd)
		case "removeTracks":
			if err := json.Unmarshal(entry.Value, &d.RemoveTracks); err != nil {
				return err
			}
			d.deltaOpOrder = append(d.deltaOpOrder, deltaOperationRemove)
		case "cloneTracks":
			if err := json.Unmarshal(entry.Value, &d.CloneTracks); err != nil {
				return err
			}
			d.deltaOpOrder = append(d.deltaOpOrder, deltaOperationClone)
		default:
			d.ExtraFields[entry.Key] = cloneRawMessage(entry.Value)
		}
	}
	if !sawDeltaUpdate {
		return fmt.Errorf("msf: delta catalog must include deltaUpdate=true")
	}

	return nil
}

// Clone returns a deep copy of the reference.
func (r TrackRef) Clone() TrackRef {
	clone := r
	clone.ExtraFields = cloneRawMessages(r.ExtraFields)
	return clone
}

// ID returns the resolved identity of the referenced track.
func (r TrackRef) ID(defaultNamespace string) TrackID {
	return TrackID{
		Namespace: r.effectiveNamespace(defaultNamespace),
		Name:      r.Name,
	}
}

// effectiveNamespace resolves Namespace against the delta or catalog default namespace.
func (r TrackRef) effectiveNamespace(defaultNamespace string) string {
	if r.Namespace != "" {
		return r.Namespace
	}
	if defaultNamespace != "" {
		return defaultNamespace
	}
	return inheritedNamespaceSentinel
}

// Validate checks whether the reference is valid for a removeTracks entry.
func (r TrackRef) Validate(path string) []string {
	var problems []string
	if r.Name == "" {
		problems = append(problems, path+": name is required")
	}
	if len(r.ExtraFields) > 0 {
		problems = append(problems, path+": remove track entries may contain only name and optional namespace")
	}
	return problems
}

// MarshalJSON encodes the reference as a JSON object.
func (r TrackRef) MarshalJSON() ([]byte, error) {
	obj := make(map[string]any, len(r.ExtraFields)+2)
	for key, raw := range r.ExtraFields {
		obj[key] = cloneRawMessage(raw)
	}
	if r.Namespace != "" {
		obj["namespace"] = r.Namespace
	}
	if r.Name != "" {
		obj["name"] = r.Name
	}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes the removeTracks JSON shape.
func (r *TrackRef) UnmarshalJSON(data []byte) error {
	*r = TrackRef{}
	r.ExtraFields = make(map[string]json.RawMessage)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key, value := range raw {
		switch key {
		case "namespace":
			if err := json.Unmarshal(value, &r.Namespace); err != nil {
				return err
			}
		case "name":
			if err := json.Unmarshal(value, &r.Name); err != nil {
				return err
			}
		default:
			r.ExtraFields[key] = cloneRawMessage(value)
		}
	}
	return nil
}

// Clone returns a deep copy of the clone operation.
func (c TrackClone) Clone() TrackClone {
	return TrackClone{
		Track:      c.Track.Clone(),
		ParentName: c.ParentName,
	}
}

// Validate checks whether the cloneTracks entry is valid.
func (c TrackClone) Validate(path string) []string {
	var problems []string
	if c.Name == "" {
		problems = append(problems, path+": name is required")
	}
	if c.ParentName == "" {
		problems = append(problems, path+": parentName is required for clone tracks")
	}
	return problems
}

// effectiveNamespace resolves the clone entry namespace used for parent lookup.
func (c TrackClone) effectiveNamespace(defaultNamespace string) string {
	return c.Track.effectiveNamespace(defaultNamespace)
}

// MarshalJSON encodes the clone entry as a JSON object with parentName.
func (c TrackClone) MarshalJSON() ([]byte, error) {
	obj := c.Track.marshalObject()
	if c.ParentName != "" {
		obj["parentName"] = c.ParentName
	}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes a cloneTracks entry.
func (c *TrackClone) UnmarshalJSON(data []byte) error {
	*c = TrackClone{}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	trackRaw := make(map[string]json.RawMessage, len(raw))
	for key, value := range raw {
		if key == "parentName" {
			if err := json.Unmarshal(value, &c.ParentName); err != nil {
				return err
			}
			continue
		}
		trackRaw[key] = value
	}
	return c.Track.unmarshalObject(trackRaw)
}

// cloneTrackRefs returns a deep copy of a TrackRef slice.
func cloneTrackRefs(in []TrackRef) []TrackRef {
	if in == nil {
		return nil
	}
	out := make([]TrackRef, len(in))
	for i, track := range in {
		out[i] = track.Clone()
	}
	return out
}

// cloneTrackClones returns a deep copy of a TrackClone slice.
func cloneTrackClones(in []TrackClone) []TrackClone {
	if in == nil {
		return nil
	}
	out := make([]TrackClone, len(in))
	for i, track := range in {
		out[i] = track.Clone()
	}
	return out
}

// cloneDeltaExtraFields copies extension fields into dst for delta metadata merging.
func cloneDeltaExtraFields(dst map[string]json.RawMessage, src map[string]json.RawMessage) {
	maps.Copy(dst, cloneRawMessages(src))
}
