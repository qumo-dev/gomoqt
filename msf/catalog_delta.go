package msf

import (
	"encoding/json"
	"fmt"
	"slices"
)

// CatalogDelta represents a delta update for an existing MSF catalog
// (draft-ietf-moq-msf-02 §5.1.6, §5.3).
//
// A delta catalog never carries a complete track list. Instead it contains one
// or more add/remove/clone/update operations and the generatedAt timestamp,
// which a delta MUST carry; it MUST NOT carry any other root catalog field.
// The JSON form is a "deltaUpdate" array of operation objects, each shaped as
// {"op": "add"|"remove"|"clone"|"update", "tracks": [...]}.
//
// The in-memory model keeps one grouped slice per operation kind (AddTracks,
// RemoveTracks, CloneTracks, UpdateTracks). A decoded delta also records each
// wire operation in order, so interleaved operations (e.g. remove,add,remove
// of the same track) apply and re-encode in their declared order. Tracks a
// delta built or extended in code holds beyond the recorded operations follow
// as one operation per kind, in add, remove, clone, update order, so an update
// can target a track the same delta adds or clones.
type CatalogDelta struct {
	// DefaultNamespace, if set, replaces the base catalog namespace used for
	// resolving tracks whose namespace field is omitted. It is not serialized.
	DefaultNamespace string `json:"-"`
	// GeneratedAt records the delta's generatedAt timestamp; it is required.
	GeneratedAt *int64 `json:"-"`

	// AddTracks lists new track entries to append to the base catalog.
	AddTracks []Track `json:"-"`
	// RemoveTracks identifies tracks to remove from the base catalog.
	RemoveTracks []TrackRef `json:"-"`
	// CloneTracks lists clone operations derived from an existing parent track.
	CloneTracks []TrackClone `json:"-"`
	// UpdateTracks lists update operations that override fields of an
	// existing track.
	UpdateTracks []TrackUpdate `json:"-"`

	// ExtraFields stores unknown JSON properties for round-tripping.
	ExtraFields map[string]json.RawMessage `json:"-"`

	// ops records the decoded deltaUpdate operations in wire order; see
	// operations.
	ops []deltaOp
}

// deltaOp is one operation of a decoded deltaUpdate array: its kind and how
// many tracks it carries, taken in order from the slice for that kind.
type deltaOp struct {
	kind deltaOperationKind
	n    int
}

// deltaSpan is one operation to apply or encode: a kind and the range of the
// slice for that kind it covers.
type deltaSpan struct {
	kind   deltaOperationKind
	lo, hi int
}

// deltaKinds lists the operation kinds in the order code-built operations
// apply and encode; read-only.
var deltaKinds = [...]deltaOperationKind{deltaOperationAdd, deltaOperationRemove, deltaOperationClone, deltaOperationUpdate}

// kindIndex returns the position of kind in deltaKinds.
func kindIndex(kind deltaOperationKind) int {
	switch kind {
	case deltaOperationAdd:
		return 0
	case deltaOperationRemove:
		return 1
	case deltaOperationClone:
		return 2
	default:
		return 3
	}
}

// TrackRef identifies a track by namespace and name.
//
// It is used where only the identity of a track is required, such as the
// tracks of a "remove" operation inside a catalog delta.
type TrackRef struct {
	Namespace string `json:"-"`
	Name      string `json:"-"`

	ExtraFields map[string]json.RawMessage `json:"-"`
}

// TrackClone describes a "clone" operation in a catalog delta.
//
// The embedded Track provides the override values to apply to the cloned track,
// including its new Name. ParentName identifies the source track from which
// values are inherited and ParentNamespace (optional) identifies the namespace
// of that source track; without it the catalog namespace is assumed.
type TrackClone struct {
	Track

	ParentName      string `json:"-"`
	ParentNamespace string `json:"-"`
}

// TrackUpdate describes an "update" operation in a catalog delta
// (draft-ietf-moq-msf-02 §5.1.6).
//
// ParentName and ParentNamespace (optional) identify the existing track to
// update. The fields declared in the embedded Track override that track's
// values; fields not declared keep their values. Declaring name or namespace
// renames the track, which must not collide with another track.
type TrackUpdate struct {
	Track

	ParentName      string `json:"-"`
	ParentNamespace string `json:"-"`
}

// Clone returns a deep copy of the delta catalog.
func (d CatalogDelta) Clone() CatalogDelta {
	clone := d
	clone.GeneratedAt = cloneInt64Ptr(d.GeneratedAt)
	clone.AddTracks = cloneTracks(d.AddTracks)
	clone.RemoveTracks = cloneTrackRefs(d.RemoveTracks)
	clone.CloneTracks = cloneTrackClones(d.CloneTracks)
	clone.UpdateTracks = cloneTrackUpdates(d.UpdateTracks)
	clone.ExtraFields = cloneRawMessages(d.ExtraFields)
	clone.ops = slices.Clone(d.ops)
	return clone
}

// Validate checks whether the delta satisfies the package's MSF draft-02
// rules: generatedAt is present, at least one operation carries a track, and
// each operation's tracks are well formed. Whether the operations are logical
// for a given catalog is checked by Catalog.ApplyDelta.
func (d CatalogDelta) Validate() error {
	var problems []string

	if d.GeneratedAt == nil {
		problems = append(problems, "delta catalog must include generatedAt")
	}
	if len(d.AddTracks) == 0 && len(d.RemoveTracks) == 0 && len(d.CloneTracks) == 0 && len(d.UpdateTracks) == 0 {
		problems = append(problems, "delta catalog must contain at least one add, remove, clone, or update track")
	}
	for i := range d.AddTracks {
		if errs := d.AddTracks[i].validate(trackListTracks); len(errs) > 0 {
			prefix := "add[" + itoa(i) + "]: "
			for _, err := range errs {
				problems = append(problems, prefix+err)
			}
		}
	}
	for i := range d.RemoveTracks {
		if errs := d.RemoveTracks[i].Validate(""); len(errs) > 0 {
			prefix := "remove[" + itoa(i) + "]: "
			for _, err := range errs {
				problems = append(problems, prefix+err)
			}
		}
	}
	for i := range d.CloneTracks {
		if errs := d.CloneTracks[i].Validate(""); len(errs) > 0 {
			prefix := "clone[" + itoa(i) + "]: "
			for _, err := range errs {
				problems = append(problems, prefix+err)
			}
		}
	}
	for i := range d.UpdateTracks {
		if errs := d.UpdateTracks[i].Validate(""); len(errs) > 0 {
			prefix := "update[" + itoa(i) + "]: "
			for _, err := range errs {
				problems = append(problems, prefix+err)
			}
		}
	}

	return newValidationError(problems)
}

// ParseCatalogDelta decodes an MSF catalog delta from JSON bytes. Like
// ParseCatalog it rejects documents that begin with a byte order mark, are not
// UTF-8, or contain an unpaired surrogate escape.
func ParseCatalogDelta(data []byte) (CatalogDelta, error) {
	if err := checkNoBOM(data); err != nil {
		return CatalogDelta{}, err
	}
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

// operations returns the delta's operations in the order to apply and encode
// them. Decoded operations keep their wire order; tracks beyond what they
// cover follow as one operation per kind, in deltaKinds order.
func (d CatalogDelta) operations() []deltaSpan {
	lens := [len(deltaKinds)]int{len(d.AddTracks), len(d.RemoveTracks), len(d.CloneTracks), len(d.UpdateTracks)}
	var next [len(deltaKinds)]int

	spans := make([]deltaSpan, 0, len(d.ops)+len(deltaKinds))
	for _, op := range d.ops {
		k := kindIndex(op.kind)
		lo := next[k]
		hi := min(lo+op.n, lens[k])
		// An empty operation is kept so the wire array round-trips.
		if hi > lo || op.n == 0 {
			spans = append(spans, deltaSpan{kind: op.kind, lo: lo, hi: hi})
		}
		next[k] = hi
	}
	for k, kind := range deltaKinds {
		if next[k] < lens[k] {
			spans = append(spans, deltaSpan{kind: kind, lo: next[k], hi: lens[k]})
		}
	}
	return spans
}

var (
	_ json.Marshaler   = CatalogDelta{}
	_ json.Unmarshaler = (*CatalogDelta)(nil)
	_ json.Marshaler   = TrackRef{}
	_ json.Unmarshaler = (*TrackRef)(nil)
	_ json.Marshaler   = TrackClone{}
	_ json.Unmarshaler = (*TrackClone)(nil)
	_ json.Marshaler   = TrackUpdate{}
	_ json.Unmarshaler = (*TrackUpdate)(nil)
)

// MarshalJSON encodes the delta in the draft-02 JSON form: a "deltaUpdate"
// array of {"op", "tracks"} objects in declared operation order, plus
// generatedAt.
func (d CatalogDelta) MarshalJSON() ([]byte, error) {
	obj := make(map[string]any, len(d.ExtraFields)+2)
	for key, raw := range d.ExtraFields {
		obj[key] = cloneRawMessage(raw)
	}
	spans := d.operations()
	ops := make([]map[string]any, 0, len(spans))
	for _, op := range spans {
		entry := map[string]any{"op": string(op.kind)}
		if op.lo == op.hi {
			// tracks is required; an empty operation still encodes it as [],
			// not as the null a nil slice of that kind would produce.
			entry["tracks"] = []struct{}{}
			ops = append(ops, entry)
			continue
		}
		switch op.kind {
		case deltaOperationAdd:
			entry["tracks"] = d.AddTracks[op.lo:op.hi]
		case deltaOperationRemove:
			entry["tracks"] = d.RemoveTracks[op.lo:op.hi]
		case deltaOperationClone:
			entry["tracks"] = d.CloneTracks[op.lo:op.hi]
		case deltaOperationUpdate:
			entry["tracks"] = d.UpdateTracks[op.lo:op.hi]
		}
		ops = append(ops, entry)
	}
	obj["deltaUpdate"] = ops
	if d.GeneratedAt != nil {
		obj["generatedAt"] = *d.GeneratedAt
	}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes a delta catalog and rejects the root catalog fields a
// delta must not carry (version, isComplete, tracks, publishTracks,
// initDataList). A missing generatedAt is reported by Validate.
func (d *CatalogDelta) UnmarshalJSON(data []byte) error {
	*d = CatalogDelta{}
	d.ExtraFields = make(map[string]json.RawMessage)

	if err := checkCatalogText(data); err != nil {
		return err
	}
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
		case "version", "isComplete", "tracks", "publishTracks", "initDataList":
			return fmt.Errorf("msf: root catalog field %q is not allowed in a delta catalog", entry.Key)
		case "generatedAt":
			// Into the pointer, so that null leaves the field absent.
			if err := json.Unmarshal(entry.Value, &d.GeneratedAt); err != nil {
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

// decodeDeltaOps decodes the deltaUpdate array of {op, tracks} objects into
// the grouped slices, recording each operation in order. It replaces any
// operations decoded before, so with a repeated deltaUpdate key the last one
// wins, as encoding/json does for repeated keys.
func (d *CatalogDelta) decodeDeltaOps(data []byte) error {
	d.AddTracks, d.RemoveTracks, d.CloneTracks, d.UpdateTracks, d.ops = nil, nil, nil, nil, nil
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
		// Each kind decodes into a fresh slice that is then appended:
		// encoding/json resets the destination slice length to zero before
		// appending, so unmarshaling directly into the grouped slice would
		// discard the tracks of a previous operation of the same kind.
		var n int
		switch deltaOperationKind(op) {
		case deltaOperationAdd:
			var add []Track
			if err := json.Unmarshal(tracksRaw, &add); err != nil {
				return err
			}
			d.AddTracks = append(d.AddTracks, add...)
			n = len(add)
		case deltaOperationRemove:
			var remove []TrackRef
			if err := json.Unmarshal(tracksRaw, &remove); err != nil {
				return err
			}
			d.RemoveTracks = append(d.RemoveTracks, remove...)
			n = len(remove)
		case deltaOperationClone:
			var clone []TrackClone
			if err := json.Unmarshal(tracksRaw, &clone); err != nil {
				return err
			}
			d.CloneTracks = append(d.CloneTracks, clone...)
			n = len(clone)
		case deltaOperationUpdate:
			var update []TrackUpdate
			if err := json.Unmarshal(tracksRaw, &update); err != nil {
				return err
			}
			d.UpdateTracks = append(d.UpdateTracks, update...)
			n = len(update)
		default:
			return fmt.Errorf("msf: unknown delta update op %q", op)
		}
		d.ops = append(d.ops, deltaOp{kind: deltaOperationKind(op), n: n})
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
	return parentEffectiveNamespace(r.Namespace, defaultNamespace)
}

// Validate checks whether the reference is valid for a "remove" operation:
// a non-empty name, an optional well-formed namespace, and no other field.
func (r TrackRef) Validate(path string) []string {
	var problems []string
	prefix := ""
	if path != "" {
		prefix = path + ": "
	}
	if r.Name == "" {
		problems = append(problems, prefix+"name is required")
	} else if err := validateNameText(r.Name); err != nil {
		problems = append(problems, prefix+"name: "+err.Error())
	}
	if r.Namespace != "" {
		if err := validateNamespace(r.Namespace); err != nil {
			problems = append(problems, prefix+err.Error())
		}
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

// UnmarshalJSON decodes the "remove" track JSON shape.
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
			if isJSONNull(value) {
				continue
			}
			if err := json.Unmarshal(value, &r.Namespace); err != nil {
				return err
			}
			if r.Namespace == "" {
				return errEmptyNamespace
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

// Validate checks whether the "clone" track entry is valid: it needs the new
// track name and the parent name.
func (c TrackClone) Validate(path string) []string {
	prefix := ""
	if path != "" {
		prefix = path + ": "
	}
	var problems []string
	if c.Name == "" {
		problems = append(problems, prefix+"name is required")
	}
	return appendParentProblems(problems, prefix, "clone", c.ParentName, c.ParentNamespace)
}

// MarshalJSON encodes the clone entry as a JSON object with parentName and
// optional parentNamespace.
func (c TrackClone) MarshalJSON() ([]byte, error) {
	return marshalParentEntry(c.Track, c.ParentName, c.ParentNamespace)
}

// UnmarshalJSON decodes a "clone" track entry.
func (c *TrackClone) UnmarshalJSON(data []byte) error {
	*c = TrackClone{}
	return unmarshalParentEntry(data, &c.Track, &c.ParentName, &c.ParentNamespace)
}

// Clone returns a deep copy of the update operation.
func (u TrackUpdate) Clone() TrackUpdate {
	return TrackUpdate{
		Track:           u.Track.Clone(),
		ParentName:      u.ParentName,
		ParentNamespace: u.ParentNamespace,
	}
}

// Validate checks whether the "update" track entry is valid: it needs the
// name of the track to update.
func (u TrackUpdate) Validate(path string) []string {
	prefix := ""
	if path != "" {
		prefix = path + ": "
	}
	return appendParentProblems(nil, prefix, "update", u.ParentName, u.ParentNamespace)
}

// MarshalJSON encodes the update entry as a JSON object with parentName and
// optional parentNamespace.
func (u TrackUpdate) MarshalJSON() ([]byte, error) {
	return marshalParentEntry(u.Track, u.ParentName, u.ParentNamespace)
}

// UnmarshalJSON decodes an "update" track entry.
func (u *TrackUpdate) UnmarshalJSON(data []byte) error {
	*u = TrackUpdate{}
	return unmarshalParentEntry(data, &u.Track, &u.ParentName, &u.ParentNamespace)
}

// appendParentProblems checks the parentName and parentNamespace of a clone
// or update entry (draft-ietf-moq-msf-02 §5.2.34, §5.2.35).
func appendParentProblems(problems []string, prefix, op, parentName, parentNamespace string) []string {
	if parentName == "" {
		problems = append(problems, prefix+"parentName is required for "+op+" tracks")
	} else if err := validateNameText(parentName); err != nil {
		problems = append(problems, prefix+"parentName: "+err.Error())
	}
	if parentNamespace != "" {
		if err := validateNamespace(parentNamespace); err != nil {
			problems = append(problems, prefix+"parentNamespace: "+err.Error())
		}
	}
	return problems
}

// parentEffectiveNamespace resolves an optional namespace reference:
// namespace when set, otherwise the catalog default namespace, and finally the
// inherited sentinel.
func parentEffectiveNamespace(namespace, defaultNamespace string) string {
	if namespace != "" {
		return namespace
	}
	if defaultNamespace != "" {
		return defaultNamespace
	}
	return inheritedNamespaceSentinel
}

// marshalParentEntry encodes a clone or update entry: the track overrides plus
// parentName and optional parentNamespace.
func marshalParentEntry(track Track, parentName, parentNamespace string) ([]byte, error) {
	obj := track.marshalObject()
	if parentName != "" {
		obj["parentName"] = parentName
	}
	if parentNamespace != "" {
		obj["parentNamespace"] = parentNamespace
	}
	return json.Marshal(obj)
}

// unmarshalParentEntry decodes a clone or update entry, splitting parentName
// and parentNamespace from the track overrides.
func unmarshalParentEntry(data []byte, track *Track, parentName, parentNamespace *string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	trackRaw := make(map[string]json.RawMessage, len(raw))
	for key, value := range raw {
		switch key {
		case "parentName":
			if err := json.Unmarshal(value, parentName); err != nil {
				return err
			}
		case "parentNamespace":
			if isJSONNull(value) {
				continue
			}
			if err := json.Unmarshal(value, parentNamespace); err != nil {
				return err
			}
			if *parentNamespace == "" {
				return fmt.Errorf("msf: parentNamespace must not be empty; omit it to use the catalog namespace")
			}
		default:
			trackRaw[key] = value
		}
	}
	return track.unmarshalObject(trackRaw)
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

// cloneTrackUpdates returns a deep copy of a TrackUpdate slice.
func cloneTrackUpdates(in []TrackUpdate) []TrackUpdate {
	if in == nil {
		return nil
	}
	out := make([]TrackUpdate, len(in))
	for i, track := range in {
		out[i] = track.Clone()
	}
	return out
}
