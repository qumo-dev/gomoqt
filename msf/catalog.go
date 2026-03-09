package msf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

const (
	// PackagingLOC identifies LOC-packaged media content.
	PackagingLOC Packaging = "loc"
	// PackagingMediaTimeline identifies a media timeline track.
	PackagingMediaTimeline Packaging = "mediatimeline"
	// PackagingEventTimeline identifies an event timeline track.
	PackagingEventTimeline Packaging = "eventtimeline"
	// PackagingCMAF identifies CMAF-packaged media content.
	PackagingCMAF Packaging = "cmaf"
	// PackagingLegacy identifies legacy timestamp+payload packaging.
	PackagingLegacy Packaging = "legacy"
)

const inheritedNamespaceSentinel = "\x00catalog"

type deltaOperationKind string

const (
	deltaOperationAdd    deltaOperationKind = "addTracks"
	deltaOperationRemove deltaOperationKind = "removeTracks"
	deltaOperationClone  deltaOperationKind = "cloneTracks"
)

// ValidationError contains one or more validation problems.
type ValidationError struct {
	Problems []string
}

// Packaging identifies the JSON packaging string for an MSF track.
type Packaging string

// String returns the packaging value as it should appear in JSON.
func (p Packaging) String() string {
	return string(p)
}

// IsKnown reports whether the packaging value is one of the package constants.
func (p Packaging) IsKnown() bool {
	switch p {
	case PackagingLOC, PackagingMediaTimeline, PackagingEventTimeline, PackagingCMAF, PackagingLegacy:
		return true
	default:
		return false
	}
}

// Role identifies the optional content role string for an MSF track.
type Role string

const (
	// RoleVideo identifies a video track.
	RoleVideo Role = "video"
	// RoleAudio identifies an audio track.
	RoleAudio Role = "audio"
	// RoleAudioDescription identifies an audio-description track.
	RoleAudioDescription Role = "audiodescription"
	// RoleCaption identifies a caption track.
	RoleCaption Role = "caption"
	// RoleSubtitle identifies a subtitle track.
	RoleSubtitle Role = "subtitle"
	// RoleSignLanguage identifies a sign-language video track.
	RoleSignLanguage Role = "signlanguage"
)

// String returns the role value as it should appear in JSON.
func (r Role) String() string {
	return string(r)
}

// IsKnown reports whether the role value is one of the package constants.
func (r Role) IsKnown() bool {
	switch r {
	case RoleVideo, RoleAudio, RoleAudioDescription, RoleCaption, RoleSubtitle, RoleSignLanguage:
		return true
	default:
		return false
	}
}

// Error returns the aggregated validation problems as a single error string.
func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "msf: validation failed"
	}

	return "msf: validation failed: " + strings.Join(e.Problems, "; ")
}

// newValidationError converts a non-empty problem list into a ValidationError.
func newValidationError(problems []string) error {
	if len(problems) == 0 {
		return nil
	}

	return &ValidationError{Problems: problems}
}

// Catalog represents an independent MSF catalog object as defined in
// draft-ietf-moq-msf-00.
//
// Delta updates are modeled separately by CatalogDelta so that the type system
// can distinguish a complete catalog snapshot from a set of incremental
// catalog operations. DefaultNamespace is not serialized; it represents the
// namespace implied by the catalog track itself and is used to resolve track
// namespace inheritance.
type Catalog struct {
	DefaultNamespace string `json:"-"`

	// Version is the MSF version number for the independent catalog.
	Version int `json:"-"`
	// GeneratedAt, if non-nil, records the catalog's generatedAt timestamp.
	GeneratedAt *int64 `json:"-"`
	// IsComplete reports whether the catalog has been explicitly marked as complete.
	IsComplete bool `json:"-"`

	// Tracks holds the complete set of track descriptions in the catalog.
	Tracks []Track `json:"-"`

	// ExtraFields stores any JSON properties that don't correspond to the
	// explicit fields above; they are preserved when re-encoding the catalog.
	ExtraFields map[string]json.RawMessage `json:"-"`
}

// Track represents a catalog track description.  Many of the numeric and
// boolean fields use pointers so that the presence or absence of the field can
// be distinguished when decoding JSON; this is required both for correct
// `omitempty` behavior and for the delta/clone machinery which needs to know
// which values were explicitly provided by the user.
//
// Fields that are always required by the draft (namespace, name, packaging,
// etc.) are plain strings, whereas optional parameters are pointer-typed.
//
// ExtraFields and presentFields are helpers used by the custom JSON
// marshaling/unmarshaling logic for round‑tripping unknown keys and tracking
// which fields were declared.
type Track struct {
	// Namespace of the track.  If empty, the catalog's DefaultNamespace
	// (and, transitively, the namespace of the catalog track itself) is used.
	Namespace string `json:"-"`
	// Name of the track; unique within its namespace.
	Name string `json:"-"`
	// Packaging mode string such as "loc" or "mediatimeline".
	Packaging Packaging `json:"-"`
	// For eventtimeline tracks, the event type identifier.
	EventType string `json:"-"`
	// Optional content role ("video", "audio", etc.).
	Role Role `json:"-"`

	// IsLive indicates whether the track is live.  Pointer form allows knowing
	// if the field was omitted vs explicitly set to false.
	IsLive *bool `json:"-"`
	// Target latency in milliseconds; nil means unspecified.
	TargetLatency *int64 `json:"-"`
	// Human-readable label.
	Label string `json:"-"`
	// Render group number for synchronized playback.
	RenderGroup *int64 `json:"-"`
	// Alternate group for quality switching.
	AltGroup *int64 `json:"-"`
	// Base64-encoded initialization data (e.g. codec config).
	InitData string `json:"-"`
	// List of track names this track depends on.
	Depends []string
	// Temporal and spatial identifiers for H.264/AV1 layers, optional.
	TemporalID *int64 `json:"-"`
	SpatialID  *int64 `json:"-"`
	// Codec string, may be empty.
	Codec string `json:"-"`
	// MIME type for the track's data payload.
	MimeType string `json:"-"`
	// Frame rate, timescale, and bitrate all optional and pointer-typed.
	Framerate *int64 `json:"-"`
	Timescale *int64 `json:"-"`
	Bitrate   *int64 `json:"-"`
	// Spatial dimensions; pointers so zero width/height can be distinguished
	// from the field being absent.
	Width  *int64 `json:"-"`
	Height *int64 `json:"-"`
	// Audio sample rate and channel configuration.
	SampleRate    *int64 `json:"-"`
	ChannelConfig string `json:"-"`
	// Display size for crop/letterbox.
	DisplayWidth  *int64 `json:"-"`
	DisplayHeight *int64 `json:"-"`
	// Language tag, optional.
	Language string `json:"-"`
	// TrackDuration in milliseconds; must not be set when IsLive is true.
	TrackDuration *int64 `json:"-"`

	// ExtraFields stores unknown JSON key/values for round-tripping.
	ExtraFields map[string]json.RawMessage `json:"-"`

	// presentFields tracks which top-level fields were explicitly present in
	// the original JSON, used during validation and cloning operations.
	presentFields map[string]struct{}
}

// TrackID uniquely identifies a track by namespace and name.
type TrackID struct {
	Namespace string
	Name      string
}

// String returns a stable printable representation of the track identity.
func (id TrackID) String() string {
	if id.Namespace == inheritedNamespaceSentinel {
		return id.Name
	}
	if id.Namespace == "" {
		return id.Name
	}
	return id.Namespace + "/" + id.Name
}

// Clone returns a deep copy of the catalog and its track collections.
func (c Catalog) Clone() Catalog {
	clone := c
	clone.GeneratedAt = cloneInt64Ptr(c.GeneratedAt)
	clone.Tracks = cloneTracks(c.Tracks)
	clone.ExtraFields = cloneRawMessages(c.ExtraFields)
	return clone
}

// Validate checks whether the catalog satisfies the package's MSF draft-00 rules.
func (c Catalog) Validate() error {
	var problems []string

	if c.Version == 0 {
		problems = append(problems, "catalog version is required")
	}
	for i, track := range c.Tracks {
		problems = append(problems, track.validate(trackContextCatalog, fmt.Sprintf("tracks[%d]", i))...)
	}
	seen := make(map[TrackID]struct{}, len(c.Tracks))
	for i, track := range c.Tracks {
		id := track.ID(c.DefaultNamespace)
		if _, ok := seen[id]; ok {
			problems = append(problems, fmt.Sprintf("tracks[%d]: duplicate track identity %q", i, id.String()))
			continue
		}
		seen[id] = struct{}{}
	}

	return newValidationError(problems)
}

// ApplyDelta applies a catalog delta to the receiver and returns the updated catalog.
func (c Catalog) ApplyDelta(delta CatalogDelta) (Catalog, error) {
	if err := c.Validate(); err != nil {
		return Catalog{}, err
	}
	if err := delta.Validate(); err != nil {
		return Catalog{}, err
	}

	result := c.Clone()
	if delta.DefaultNamespace != "" && delta.DefaultNamespace != result.DefaultNamespace {
		if result.hasInheritedNamespaceTracks() {
			return Catalog{}, fmt.Errorf("msf: cannot change default namespace when catalog contains tracks that inherit it")
		}
		result.DefaultNamespace = delta.DefaultNamespace
	}
	if delta.GeneratedAt != nil {
		value := *delta.GeneratedAt
		result.GeneratedAt = &value
	}
	if delta.IsComplete {
		result.IsComplete = true
	}
	maps.Copy(result.ExtraFields, cloneRawMessages(delta.ExtraFields))

	order := delta.operationOrder()
	for _, op := range order {
		switch op {
		case deltaOperationAdd:
			for _, track := range delta.AddTracks {
				if err := result.addTrack(track); err != nil {
					return Catalog{}, err
				}
			}
		case deltaOperationRemove:
			for _, track := range delta.RemoveTracks {
				if err := result.removeTrack(track); err != nil {
					return Catalog{}, err
				}
			}
		case deltaOperationClone:
			for _, track := range delta.CloneTracks {
				if err := result.cloneTrack(track); err != nil {
					return Catalog{}, err
				}
			}
		}
	}
	if err := result.Validate(); err != nil {
		return Catalog{}, err
	}

	return result, nil
}

// hasInheritedNamespaceTracks reports whether any track currently relies on DefaultNamespace.
func (c Catalog) hasInheritedNamespaceTracks() bool {
	for _, track := range c.Tracks {
		if track.Namespace == "" {
			return true
		}
	}
	return false
}

// ParseCatalog decodes an independent MSF catalog from JSON bytes. It rejects
// delta-only fields such as deltaUpdate and addTracks. Validation must be
// performed separately if desired.
func ParseCatalog(data []byte) (Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

// ParseCatalogString is like ParseCatalog but accepts a string input.  It is
// provided for convenience when the catalog is already available as a string.
func ParseCatalogString(s string) (Catalog, error) {
	return ParseCatalog([]byte(s))
}

// addTrack appends a new track after checking that its resolved identity is unique.
func (c *Catalog) addTrack(track Track) error {
	id := track.ID(c.DefaultNamespace)
	if _, idx, ok := c.findTrack(id); ok {
		return fmt.Errorf("msf: cannot add duplicate track %q at index %d", id.String(), idx)
	}

	c.Tracks = append(c.Tracks, track.Clone())
	return nil
}

// removeTrack removes the track identified by track from the catalog.
func (c *Catalog) removeTrack(track TrackRef) error {
	id := track.ID(c.DefaultNamespace)
	_, idx, ok := c.findTrack(id)
	if !ok {
		return fmt.Errorf("msf: cannot remove unknown track %q", id.String())
	}

	c.Tracks = append(c.Tracks[:idx], c.Tracks[idx+1:]...)
	return nil
}

// cloneTrack creates a new track by cloning an existing parent and applying overrides.
func (c *Catalog) cloneTrack(track TrackClone) error {
	parentID := TrackID{Namespace: track.effectiveNamespace(c.DefaultNamespace), Name: track.ParentName}
	parent, _, ok := c.findTrack(parentID)
	if !ok {
		return fmt.Errorf("msf: cannot clone unknown parent track %q", parentID.String())
	}

	cloned := parent.Clone()
	cloned.applyOverrides(track.Track)

	if cloned.Name == "" {
		return fmt.Errorf("msf: cloned track derived from %q is missing name", parentID.String())
	}

	newID := cloned.ID(c.DefaultNamespace)
	if _, idx, exists := c.findTrack(newID); exists {
		return fmt.Errorf("msf: cannot clone into duplicate track %q at index %d", newID.String(), idx)
	}

	c.Tracks = append(c.Tracks, cloned)
	return nil
}

// findTrack looks up a track by resolved identity and returns the track and its index.
func (c Catalog) findTrack(id TrackID) (Track, int, bool) {
	for i, track := range c.Tracks {
		if track.ID(c.DefaultNamespace) == id {
			return track, i, true
		}
	}
	return Track{}, -1, false
}

var (
	_ json.Marshaler   = Catalog{}
	_ json.Unmarshaler = (*Catalog)(nil)
)

// MarshalJSON encodes the independent catalog in the draft-00 JSON shape.
func (c Catalog) MarshalJSON() ([]byte, error) {
	obj := make(map[string]any, len(c.ExtraFields)+2)
	for key, raw := range c.ExtraFields {
		obj[key] = cloneRawMessage(raw)
	}
	if c.Version != 0 {
		obj["version"] = c.Version
	}
	if c.GeneratedAt != nil {
		obj["generatedAt"] = *c.GeneratedAt
	}
	if c.IsComplete {
		obj["isComplete"] = true
	}
	if len(c.Tracks) > 0 {
		obj["tracks"] = c.Tracks
	}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes the independent catalog shape and rejects delta-only fields.
func (c *Catalog) UnmarshalJSON(data []byte) error {
	*c = Catalog{}
	c.ExtraFields = make(map[string]json.RawMessage)

	ordered, err := decodeOrderedObject(data)
	if err != nil {
		return err
	}

	for _, entry := range ordered {
		switch entry.Key {
		case "version":
			if err := json.Unmarshal(entry.Value, &c.Version); err != nil {
				return err
			}
		case "generatedAt":
			var value int64
			if err := json.Unmarshal(entry.Value, &value); err != nil {
				return err
			}
			c.GeneratedAt = &value
		case "isComplete":
			if err := json.Unmarshal(entry.Value, &c.IsComplete); err != nil {
				return err
			}
		case "tracks":
			if err := json.Unmarshal(entry.Value, &c.Tracks); err != nil {
				return err
			}
		case "deltaUpdate", "addTracks", "removeTracks", "cloneTracks":
			return fmt.Errorf("msf: delta catalog fields are not allowed in an independent catalog")
		default:
			c.ExtraFields[entry.Key] = cloneRawMessage(entry.Value)
		}
	}

	return nil
}

// Clone returns a deep copy of the track and its JSON extension fields.
func (t Track) Clone() Track {
	clone := t
	clone.Depends = slices.Clone(t.Depends)
	clone.ExtraFields = cloneRawMessages(t.ExtraFields)
	clone.presentFields = maps.Clone(t.presentFields)
	if t.IsLive != nil {
		value := *t.IsLive
		clone.IsLive = &value
	}
	clone.TargetLatency = cloneInt64Ptr(t.TargetLatency)
	clone.RenderGroup = cloneInt64Ptr(t.RenderGroup)
	clone.AltGroup = cloneInt64Ptr(t.AltGroup)
	clone.TemporalID = cloneInt64Ptr(t.TemporalID)
	clone.SpatialID = cloneInt64Ptr(t.SpatialID)
	clone.Framerate = cloneInt64Ptr(t.Framerate)
	clone.Timescale = cloneInt64Ptr(t.Timescale)
	clone.Bitrate = cloneInt64Ptr(t.Bitrate)
	clone.Width = cloneInt64Ptr(t.Width)
	clone.Height = cloneInt64Ptr(t.Height)
	clone.SampleRate = cloneInt64Ptr(t.SampleRate)
	clone.DisplayWidth = cloneInt64Ptr(t.DisplayWidth)
	clone.DisplayHeight = cloneInt64Ptr(t.DisplayHeight)
	clone.TrackDuration = cloneInt64Ptr(t.TrackDuration)
	return clone
}

// ID returns the resolved identity of the track.
//
// If Namespace is omitted, defaultNamespace is used to model catalog namespace
// inheritance from the MSF specification.
func (t Track) ID(defaultNamespace string) TrackID {
	return TrackID{
		Namespace: t.effectiveNamespace(defaultNamespace),
		Name:      t.Name,
	}
}

// effectiveNamespace resolves Namespace against the catalog default namespace.
func (t Track) effectiveNamespace(defaultNamespace string) string {
	if t.Namespace != "" {
		return t.Namespace
	}
	if defaultNamespace != "" {
		return defaultNamespace
	}
	return inheritedNamespaceSentinel
}

type trackContext string

const (
	trackContextCatalog trackContext = "catalog"
	trackContextAdd     trackContext = "add"
)

// validate checks track-level constraints for either an independent catalog or addTracks entry.
func (t Track) validate(ctx trackContext, path string) []string {
	var problems []string
	if t.Name == "" {
		problems = append(problems, path+": name is required")
	}

	if t.Packaging == "" {
		problems = append(problems, path+": packaging is required")
	}
	if t.TrackDuration != nil && t.IsLive != nil && *t.IsLive {
		problems = append(problems, path+": trackDuration must not be present when isLive is true")
	}

	switch t.Packaging {
	case PackagingEventTimeline:
		if t.EventType == "" {
			problems = append(problems, path+": eventType is required for eventtimeline tracks")
		}
		if t.MimeType != "application/json" {
			problems = append(problems, path+": eventtimeline tracks must use mimeType application/json")
		}
		if len(t.Depends) == 0 {
			problems = append(problems, path+": eventtimeline tracks must declare depends")
		}
	case PackagingMediaTimeline:
		if t.EventType != "" {
			problems = append(problems, path+": eventType must not be set for mediatimeline tracks")
		}
		if t.MimeType != "application/json" {
			problems = append(problems, path+": mediatimeline tracks must use mimeType application/json")
		}
		if len(t.Depends) == 0 {
			problems = append(problems, path+": mediatimeline tracks must declare depends")
		}
	case PackagingLOC:
		if t.EventType != "" {
			problems = append(problems, path+": eventType must not be set for loc tracks")
		}
		if t.IsLive == nil {
			problems = append(problems, path+": isLive is required for loc tracks")
		}
	default:
		if t.EventType != "" {
			problems = append(problems, path+": eventType must only be set for eventtimeline tracks")
		}
	}

	return problems
}

// applyOverrides copies explicitly declared fields from override onto the receiver.
func (t *Track) applyOverrides(override Track) {
	if override.hasField("namespace") {
		t.Namespace = override.Namespace
	}
	if override.hasField("name") {
		t.Name = override.Name
	}
	if override.hasField("packaging") {
		t.Packaging = override.Packaging
	}
	if override.hasField("eventType") {
		t.EventType = override.EventType
	}
	if override.hasField("role") {
		t.Role = override.Role
	}
	if override.hasField("isLive") {
		t.IsLive = cloneBoolPtr(override.IsLive)
	}
	if override.hasField("targetLatency") {
		t.TargetLatency = cloneInt64Ptr(override.TargetLatency)
	}
	if override.hasField("label") {
		t.Label = override.Label
	}
	if override.hasField("renderGroup") {
		t.RenderGroup = cloneInt64Ptr(override.RenderGroup)
	}
	if override.hasField("altGroup") {
		t.AltGroup = cloneInt64Ptr(override.AltGroup)
	}
	if override.hasField("initData") {
		t.InitData = override.InitData
	}
	if override.hasField("depends") {
		t.Depends = slices.Clone(override.Depends)
	}
	if override.hasField("temporalId") {
		t.TemporalID = cloneInt64Ptr(override.TemporalID)
	}
	if override.hasField("spatialId") {
		t.SpatialID = cloneInt64Ptr(override.SpatialID)
	}
	if override.hasField("codec") {
		t.Codec = override.Codec
	}
	if override.hasField("mimeType") {
		t.MimeType = override.MimeType
	}
	if override.hasField("framerate") {
		t.Framerate = cloneInt64Ptr(override.Framerate)
	}
	if override.hasField("timescale") {
		t.Timescale = cloneInt64Ptr(override.Timescale)
	}
	if override.hasField("bitrate") {
		t.Bitrate = cloneInt64Ptr(override.Bitrate)
	}
	if override.hasField("width") {
		t.Width = cloneInt64Ptr(override.Width)
	}
	if override.hasField("height") {
		t.Height = cloneInt64Ptr(override.Height)
	}
	if override.hasField("samplerate") {
		t.SampleRate = cloneInt64Ptr(override.SampleRate)
	}
	if override.hasField("channelConfig") {
		t.ChannelConfig = override.ChannelConfig
	}
	if override.hasField("displayWidth") {
		t.DisplayWidth = cloneInt64Ptr(override.DisplayWidth)
	}
	if override.hasField("displayHeight") {
		t.DisplayHeight = cloneInt64Ptr(override.DisplayHeight)
	}
	if override.hasField("lang") {
		t.Language = override.Language
	}
	if override.hasField("trackDuration") {
		t.TrackDuration = cloneInt64Ptr(override.TrackDuration)
	}
	if t.presentFields == nil {
		t.presentFields = make(map[string]struct{})
	}
	for field := range override.presentFields {
		t.presentFields[field] = struct{}{}
	}
	if t.ExtraFields == nil {
		t.ExtraFields = make(map[string]json.RawMessage)
	}
	for key, raw := range override.ExtraFields {
		t.ExtraFields[key] = cloneRawMessage(raw)
	}
}

var (
	_ json.Marshaler   = Track{}
	_ json.Unmarshaler = (*Track)(nil)
)

// marshalObject builds the JSON object form used by Track marshaling and clone entries.
func (t Track) marshalObject() map[string]any {
	obj := make(map[string]any, len(t.ExtraFields)+24)
	for key, raw := range t.ExtraFields {
		obj[key] = cloneRawMessage(raw)
	}
	if t.Namespace != "" {
		obj["namespace"] = t.Namespace
	}
	if t.Name != "" {
		obj["name"] = t.Name
	}
	if t.Packaging != "" {
		obj["packaging"] = t.Packaging
	}
	if t.EventType != "" {
		obj["eventType"] = t.EventType
	}
	if t.Role != "" {
		obj["role"] = t.Role
	}
	if t.IsLive != nil {
		obj["isLive"] = *t.IsLive
	}
	if t.TargetLatency != nil {
		obj["targetLatency"] = *t.TargetLatency
	}
	if t.Label != "" {
		obj["label"] = t.Label
	}
	if t.RenderGroup != nil {
		obj["renderGroup"] = *t.RenderGroup
	}
	if t.AltGroup != nil {
		obj["altGroup"] = *t.AltGroup
	}
	if t.InitData != "" {
		obj["initData"] = t.InitData
	}
	if t.Depends != nil {
		obj["depends"] = t.Depends
	}
	if t.TemporalID != nil {
		obj["temporalId"] = *t.TemporalID
	}
	if t.SpatialID != nil {
		obj["spatialId"] = *t.SpatialID
	}
	if t.Codec != "" {
		obj["codec"] = t.Codec
	}
	if t.MimeType != "" {
		obj["mimeType"] = t.MimeType
	}
	if t.Framerate != nil {
		obj["framerate"] = *t.Framerate
	}
	if t.Timescale != nil {
		obj["timescale"] = *t.Timescale
	}
	if t.Bitrate != nil {
		obj["bitrate"] = *t.Bitrate
	}
	if t.Width != nil {
		obj["width"] = *t.Width
	}
	if t.Height != nil {
		obj["height"] = *t.Height
	}
	if t.SampleRate != nil {
		obj["samplerate"] = *t.SampleRate
	}
	if t.ChannelConfig != "" {
		obj["channelConfig"] = t.ChannelConfig
	}
	if t.DisplayWidth != nil {
		obj["displayWidth"] = *t.DisplayWidth
	}
	if t.DisplayHeight != nil {
		obj["displayHeight"] = *t.DisplayHeight
	}
	if t.Language != "" {
		obj["lang"] = t.Language
	}
	if t.TrackDuration != nil {
		obj["trackDuration"] = *t.TrackDuration
	}
	return obj
}

// MarshalJSON encodes the track using the draft-00 object form.
func (t Track) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.marshalObject())
}

// unmarshalObject decodes a track from a raw JSON object map.
func (t *Track) unmarshalObject(raw map[string]json.RawMessage) error {
	*t = Track{}
	t.ExtraFields = make(map[string]json.RawMessage)
	t.presentFields = make(map[string]struct{})

	for key, value := range raw {
		t.presentFields[key] = struct{}{}
		switch key {
		case "namespace":
			if err := json.Unmarshal(value, &t.Namespace); err != nil {
				return err
			}
		case "name":
			if err := json.Unmarshal(value, &t.Name); err != nil {
				return err
			}
		case "packaging":
			if err := json.Unmarshal(value, &t.Packaging); err != nil {
				return err
			}
		case "eventType":
			if err := json.Unmarshal(value, &t.EventType); err != nil {
				return err
			}
		case "role":
			if err := json.Unmarshal(value, &t.Role); err != nil {
				return err
			}
		case "isLive":
			var v bool
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			t.IsLive = &v
		case "targetLatency":
			var v int64
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			t.TargetLatency = &v
		case "label":
			if err := json.Unmarshal(value, &t.Label); err != nil {
				return err
			}
		case "renderGroup":
			if err := json.Unmarshal(value, &t.RenderGroup); err != nil {
				return err
			}
		case "altGroup":
			if err := json.Unmarshal(value, &t.AltGroup); err != nil {
				return err
			}
		case "initData":
			if err := json.Unmarshal(value, &t.InitData); err != nil {
				return err
			}
		case "depends":
			if err := json.Unmarshal(value, &t.Depends); err != nil {
				return err
			}
		case "temporalId":
			if err := json.Unmarshal(value, &t.TemporalID); err != nil {
				return err
			}
		case "spatialId":
			if err := json.Unmarshal(value, &t.SpatialID); err != nil {
				return err
			}
		case "codec":
			if err := json.Unmarshal(value, &t.Codec); err != nil {
				return err
			}
		case "mimeType":
			if err := json.Unmarshal(value, &t.MimeType); err != nil {
				return err
			}
		case "framerate":
			if err := json.Unmarshal(value, &t.Framerate); err != nil {
				return err
			}
		case "timescale":
			if err := json.Unmarshal(value, &t.Timescale); err != nil {
				return err
			}
		case "bitrate":
			if err := json.Unmarshal(value, &t.Bitrate); err != nil {
				return err
			}
		case "width":
			if err := json.Unmarshal(value, &t.Width); err != nil {
				return err
			}
		case "height":
			if err := json.Unmarshal(value, &t.Height); err != nil {
				return err
			}
		case "samplerate":
			if err := json.Unmarshal(value, &t.SampleRate); err != nil {
				return err
			}
		case "channelConfig":
			if err := json.Unmarshal(value, &t.ChannelConfig); err != nil {
				return err
			}
		case "displayWidth":
			if err := json.Unmarshal(value, &t.DisplayWidth); err != nil {
				return err
			}
		case "displayHeight":
			if err := json.Unmarshal(value, &t.DisplayHeight); err != nil {
				return err
			}
		case "lang":
			if err := json.Unmarshal(value, &t.Language); err != nil {
				return err
			}
		case "trackDuration":
			if err := json.Unmarshal(value, &t.TrackDuration); err != nil {
				return err
			}
		default:
			t.ExtraFields[key] = cloneRawMessage(value)
		}
	}

	return nil
}

// UnmarshalJSON decodes the track JSON object and preserves unknown fields.
func (t *Track) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	return t.unmarshalObject(raw)
}

// hasField reports whether field was explicitly present or is materially set on the track.
func (t Track) hasField(field string) bool {
	if _, ok := t.presentFields[field]; ok {
		return true
	}
	switch field {
	case "namespace":
		return t.Namespace != ""
	case "name":
		return t.Name != ""
	case "packaging":
		return t.Packaging != ""
	case "eventType":
		return t.EventType != ""
	case "role":
		return t.Role != ""
	case "isLive":
		return t.IsLive != nil
	case "targetLatency":
		return t.TargetLatency != nil
	case "label":
		return t.Label != ""
	case "renderGroup":
		return t.RenderGroup != nil
	case "altGroup":
		return t.AltGroup != nil
	case "initData":
		return t.InitData != ""
	case "depends":
		return t.Depends != nil
	case "temporalId":
		return t.TemporalID != nil
	case "spatialId":
		return t.SpatialID != nil
	case "codec":
		return t.Codec != ""
	case "mimeType":
		return t.MimeType != ""
	case "framerate":
		return t.Framerate != nil
	case "timescale":
		return t.Timescale != nil
	case "bitrate":
		return t.Bitrate != nil
	case "width":
		return t.Width != nil
	case "height":
		return t.Height != nil
	case "samplerate":
		return t.SampleRate != nil
	case "channelConfig":
		return t.ChannelConfig != ""
	case "displayWidth":
		return t.DisplayWidth != nil
	case "displayHeight":
		return t.DisplayHeight != nil
	case "lang":
		return t.Language != ""
	case "trackDuration":
		return t.TrackDuration != nil
	default:
		return false
	}
}

type orderedField struct {
	Key   string
	Value json.RawMessage
}

// decodeOrderedObject decodes a JSON object while preserving the original field order.
func decodeOrderedObject(data []byte) ([]orderedField, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("msf: expected JSON object")
	}

	fields := make([]orderedField, 0, 8)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("msf: expected object key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		fields = append(fields, orderedField{Key: key, Value: cloneRawMessage(raw)})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("msf: unexpected trailing JSON data")
		}
		return nil, err
	}
	return fields, nil
}

// cloneTracks returns a deep copy of a track slice.
func cloneTracks(in []Track) []Track {
	if in == nil {
		return nil
	}
	out := make([]Track, len(in))
	for i, track := range in {
		out[i] = track.Clone()
	}
	return out
}

// cloneRawMessages returns a deep copy of a raw-message map.
func cloneRawMessages(in map[string]json.RawMessage) map[string]json.RawMessage {
	if in == nil {
		return make(map[string]json.RawMessage)
	}
	out := make(map[string]json.RawMessage, len(in))
	for key, value := range in {
		out[key] = cloneRawMessage(value)
	}
	return out
}

// cloneRawMessage returns a copied RawMessage buffer.
func cloneRawMessage(in json.RawMessage) json.RawMessage {
	if in == nil {
		return nil
	}
	return append(json.RawMessage(nil), in...)
}

// cloneInt64Ptr returns a copied int64 pointer.
func cloneInt64Ptr(in *int64) *int64 {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

// cloneBoolPtr returns a copied bool pointer.
func cloneBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}
