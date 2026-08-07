package msf

import (
	"encoding/json"
	"fmt"
	"slices"
)

// CatalogDelta represents a delta update for an existing MSF catalog.
//
// A delta catalog never carries a complete track list. Instead it contains one
// or more add/remove/clone operations plus optional metadata updates. The JSON
// form (draft-ietf-moq-msf-01) is a "deltaUpdate" array of operation objects,
// each shaped as {"op": "add"|"remove"|"clone", "tracks": [...]}.
//
// The in-memory model keeps three grouped slices (AddTracks/RemoveTracks/
// CloneTracks) and records the first-seen block order of each op kind so the
// wire array round-trips. Interleaved same-type ops (e.g. add,remove,add) are
// merged into a single group; the resulting catalog is identical in practice.
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

// TrackClone describes a clone operation in a catalog delta.
//
// The embedded Track provides the override values to apply to the cloned track.
// ParentName identifies the source track from which values are inherited and
// ParentNamespace (optional) identifies the namespace of that source track.
type TrackClone struct {
	Track

	ParentName      string `json:"-"`
	ParentNamespace string `json:"-"`
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

// Validate checks whether the delta satisfies the package's MSF draft-01 rules.
func (d CatalogDelta) Validate() error {
	var problems []string

	if len(d.AddTracks) == 0 && len(d.RemoveTracks) == 0 && len(d.CloneTracks) == 0 {
		problems = append(problems, "delta catalog must contain addTracks, removeTracks, or cloneTracks")
	}
	for i := range d.AddTracks {
		if errs := d.AddTracks[i].validate(""); len(errs) > 0 {
			prefix := "addTracks[" + itoa(i) + "]: "
			for _, err := range errs {
				problems = append(problems, prefix+err)
			}
		}
	}
	for i := range d.RemoveTracks {
		if errs := d.RemoveTracks[i].Validate(""); len(errs) > 0 {
			prefix := "removeTracks[" + itoa(i) + "]: "
			for _, err := range errs {
				problems = append(problems, prefix+err)
			}
		}
	}
	for i := range d.CloneTracks {
		if errs := d.CloneTracks[i].Validate(""); len(errs) > 0 {
			prefix := "cloneTracks[" + itoa(i) + "]: "
			for _, err := range errs {
				problems = append(problems, prefix+err)
			}
		}
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

// MarshalJSON encodes the delta in the draft-01 JSON form: a "deltaUpdate"
// array of {"op", "tracks"} objects in declared operation order.
func (d CatalogDelta) MarshalJSON() ([]byte, error) {
	obj := make(map[string]any, len(d.ExtraFields)+2)
	for key, raw := range d.ExtraFields {
		obj[key] = cloneRawMessage(raw)
	}
	ops := make([]map[string]any, 0, 3)
	for _, op := range d.operationOrder() {
		entry := map[string]any{"op": string(op)}
		switch op {
		case deltaOperationAdd:
			entry["tracks"] = d.AddTracks
		case deltaOperationRemove:
			entry["tracks"] = d.RemoveTracks
		case deltaOperationClone:
			entry["tracks"] = d.CloneTracks
		}
		ops = append(ops, entry)
	}
	obj["deltaUpdate"] = ops
	if d.GeneratedAt != nil {
		obj["generatedAt"] = *d.GeneratedAt
	}
	if d.IsComplete {
		obj["isComplete"] = true
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
			sawDeltaUpdate = true
			if err := d.decodeDeltaOps(entry.Value); err != nil {
				return err
			}
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
		default:
			d.ExtraFields[entry.Key] = cloneRawMessage(entry.Value)
		}
	}
	if !sawDeltaUpdate {
		return fmt.Errorf("msf: delta catalog must include a deltaUpdate array")
	}

	return nil
}

// decodeDeltaOps decodes the draft-01 deltaUpdate array of {op, tracks} objects
// into the grouped slices, recording first-seen block order.
func (d *CatalogDelta) decodeDeltaOps(data []byte) error {
	var ops []map[string]json.RawMessage
	if err := json.Unmarshal(data, &ops); err != nil {
		return fmt.Errorf("msf: deltaUpdate must be an array of operation objects: %w", err)
	}
	for _, opObj := range ops {
		opRaw, ok := opObj["op"]
		if !ok {
			return fmt.Errorf("msf: delta update operation must contain an op field")
		}
		var op string
		if err := json.Unmarshal(opRaw, &op); err != nil {
			return err
		}
		tracksRaw, ok := opObj["tracks"]
		if !ok {
			return fmt.Errorf("msf: delta update operation must contain a tracks field")
		}
		switch deltaOperationKind(op) {
		case deltaOperationAdd:
			if err := json.Unmarshal(tracksRaw, &d.AddTracks); err != nil {
				return err
			}
			d.recordOp(deltaOperationAdd)
		case deltaOperationRemove:
			if err := json.Unmarshal(tracksRaw, &d.RemoveTracks); err != nil {
				return err
			}
			d.recordOp(deltaOperationRemove)
		case deltaOperationClone:
			if err := json.Unmarshal(tracksRaw, &d.CloneTracks); err != nil {
				return err
			}
			d.recordOp(deltaOperationClone)
		default:
			return fmt.Errorf("msf: unknown delta update op %q", op)
		}
	}
	return nil
}

// recordOp appends kind to the declared operation order, ignoring repeats so
// only first-seen block order is preserved.
func (d *CatalogDelta) recordOp(kind deltaOperationKind) {
	if slices.Contains(d.deltaOpOrder, kind) {
		return
	}
	d.deltaOpOrder = append(d.deltaOpOrder, kind)
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
	prefix := ""
	if path != "" {
		prefix = path + ": "
	}
	if r.Name == "" {
		problems = append(problems, prefix+"name is required")
	}
	if len(r.ExtraFields) > 0 {
		problems = append(problems, prefix+"remove track entries may contain only name and optional namespace")
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
		Track:           c.Track.Clone(),
		ParentName:      c.ParentName,
		ParentNamespace: c.ParentNamespace,
	}
}

// Validate checks whether the cloneTracks entry is valid.
func (c TrackClone) Validate(path string) []string {
	var problems []string
	prefix := ""
	if path != "" {
		prefix = path + ": "
	}
	if c.Name == "" {
		problems = append(problems, prefix+"name is required")
	}
	if c.ParentName == "" {
		problems = append(problems, prefix+"parentName is required for clone tracks")
	}
	return problems
}

// parentEffectiveNamespace resolves the namespace of the parent track being
// cloned. ParentNamespace takes precedence when set; otherwise the catalog
// default namespace (and finally the inherited sentinel) is used.
func (c TrackClone) parentEffectiveNamespace(defaultNamespace string) string {
	if c.ParentNamespace != "" {
		return c.ParentNamespace
	}
	if defaultNamespace != "" {
		return defaultNamespace
	}
	return inheritedNamespaceSentinel
}

// MarshalJSON encodes the clone entry as a JSON object with parentName and
// optional parentNamespace.
func (c TrackClone) MarshalJSON() ([]byte, error) {
	obj := c.Track.marshalObject()
	if c.ParentName != "" {
		obj["parentName"] = c.ParentName
	}
	if c.ParentNamespace != "" {
		obj["parentNamespace"] = c.ParentNamespace
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
		switch key {
		case "parentName":
			if err := json.Unmarshal(value, &c.ParentName); err != nil {
				return err
			}
		case "parentNamespace":
			if err := json.Unmarshal(value, &c.ParentNamespace); err != nil {
				return err
			}
		default:
			trackRaw[key] = value
		}
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
