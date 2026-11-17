package msf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"strconv"
	"strings"
)

// CurrentVersion is the MSF version string of the catalogs this package
// produces and accepts (draft-ietf-moq-msf-02 §5.1.1).
const CurrentVersion = "draft-02"

const (
	// PackagingLOC identifies LOC-packaged media content.
	PackagingLOC Packaging = "loc"
	// PackagingMediaTimeline identifies a media timeline track.
	PackagingMediaTimeline Packaging = "mediatimeline"
	// PackagingEventTimeline identifies an event timeline track.
	PackagingEventTimeline Packaging = "eventtimeline"
	// PackagingMoqLog identifies a MoQ log publish track ([MOQLOG]).
	PackagingMoqLog Packaging = "moqlog"
	// PackagingMoqMetrics identifies a MoQ metrics publish track ([MOQMETRICS]).
	PackagingMoqMetrics Packaging = "moqmetrics"
	// PackagingCatalog identifies a track that is itself an MSF catalog,
	// referenced from another catalog (draft-ietf-moq-msf-02 §5).
	PackagingCatalog Packaging = "catalog"
	// PackagingCMAF identifies CMAF-packaged media content.
	// Non-spec extension retained for backward compatibility.
	PackagingCMAF Packaging = "cmaf"
	// PackagingLegacy identifies legacy timestamp+payload packaging.
	// Non-spec extension retained for backward compatibility.
	PackagingLegacy Packaging = "legacy"
)

const inheritedNamespaceSentinel = "\x00catalog"

type deltaOperationKind string

const (
	deltaOperationAdd    deltaOperationKind = "add"
	deltaOperationRemove deltaOperationKind = "remove"
	deltaOperationClone  deltaOperationKind = "clone"
	deltaOperationUpdate deltaOperationKind = "update"
)

// EncryptionSchemeMoQSecureObjects is the encryptionScheme value for MoQ
// Secure Objects, the scheme for which cipherSuite is required
// (draft-ietf-moq-msf-02 §5.2.38, §5.2.39).
const EncryptionSchemeMoQSecureObjects = "moq-secure-objects"

const (
	// InitDataTypeInline marks Initialization Data List data as base64
	// encoded initialization data.
	InitDataTypeInline = "inline"
	// InitDataTypeTrackProperty marks Initialization Data List data as the
	// hex type of the Track property that carries the initialization data.
	InitDataTypeTrackProperty = "track-property"
	// InitDataTypeObjectProperty marks Initialization Data List data as the
	// hex type of the Object property that carries the initialization data.
	InitDataTypeObjectProperty = "object-property"
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
	case PackagingLOC, PackagingMediaTimeline, PackagingEventTimeline, PackagingMoqLog, PackagingMoqMetrics, PackagingCatalog, PackagingCMAF, PackagingLegacy:
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
	// RoleMediaTimeline identifies an MSF media timeline track.
	RoleMediaTimeline Role = "mediatimeline"
	// RoleEventTimeline identifies an MSF event timeline track.
	RoleEventTimeline Role = "eventtimeline"
	// RoleLog identifies a log publishing track ([MOQLOG]).
	RoleLog Role = "log"
	// RoleMetrics identifies a metrics publishing track ([MOQMETRICS]).
	RoleMetrics Role = "metrics"
	// RoleData identifies a track carrying application-defined data
	// (draft-ietf-moq-msf-02 §5.2.7).
	RoleData Role = "data"
)

// String returns the role value as it should appear in JSON.
func (r Role) String() string {
	return string(r)
}

// IsKnown reports whether the role value is one of the package constants.
func (r Role) IsKnown() bool {
	switch r {
	case RoleVideo, RoleAudio, RoleAudioDescription, RoleCaption, RoleSubtitle, RoleSignLanguage, RoleMediaTimeline, RoleEventTimeline, RoleLog, RoleMetrics, RoleData:
		return true
	default:
		return false
	}
}

// InitDataRef represents a single entry in the catalog's Initialization Data
// List (draft-ietf-moq-msf-02 §5.1.7). Tracks reference an entry by its ID via
// the initRef track attribute. Type is InitDataTypeInline (Data is base64
// encoded init data), InitDataTypeTrackProperty or InitDataTypeObjectProperty
// (Data is the hex type of the property that carries the init data as raw
// bytes, §12.2).
type InitDataRef struct {
	// ID is the unique-within-catalog reference for this init data.
	ID string `json:"-"`
	// Type identifies how Data is interpreted.
	Type string `json:"-"`
	// Data holds the init payload as defined by Type.
	Data string `json:"-"`

	// ExtraFields stores unknown JSON properties for round-tripping.
	ExtraFields map[string]json.RawMessage `json:"-"`
}

// Buffers represents a track's target jitter-buffer configuration
// (draft-ietf-moq-msf-01 §5.2.9) in integer milliseconds. Keys are optional;
// unknown keys are preserved in ExtraFields per the spec's "MUST be ignored".
type Buffers struct {
	// Target is the target buffer in milliseconds.
	Target *int64 `json:"-"`
	// Min is the minimum buffer in milliseconds.
	Min *int64 `json:"-"`
	// Max is the maximum buffer in milliseconds.
	Max *int64 `json:"-"`

	// ExtraFields stores unknown JSON properties for round-tripping.
	ExtraFields map[string]json.RawMessage `json:"-"`
}

// AccessibilityDescriptor describes an embedded accessibility feature of a
// track (draft-ietf-moq-msf-01 §5.2.44), such as CEA-608/CEA-708 captions.
type AccessibilityDescriptor struct {
	// Scheme identifies the accessibility scheme (e.g. a URN).
	Scheme string `json:"-"`
	// Value specifies the accessibility channels or features available.
	Value string `json:"-"`

	// ExtraFields stores unknown JSON properties for round-tripping.
	ExtraFields map[string]json.RawMessage `json:"-"`
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
// draft-ietf-moq-msf-02.
//
// Delta updates are modeled separately by CatalogDelta so that the type system
// can distinguish a complete catalog snapshot from a set of incremental
// catalog operations. DefaultNamespace is not serialized; it represents the
// namespace implied by the catalog track itself (the Track Namespace under
// which the catalog was subscribed) and is used to resolve track namespace
// inheritance. A track that declares a namespace replaces, rather than
// extends, DefaultNamespace.
type Catalog struct {
	DefaultNamespace string `json:"-"`

	// Version is the MSF version string, CurrentVersion for catalogs this
	// package accepts (draft-ietf-moq-msf-02 §5.1.1).
	Version string `json:"-"`
	// GeneratedAt, if non-nil, records the catalog's generatedAt timestamp.
	GeneratedAt *int64 `json:"-"`
	// IsComplete reports whether the catalog has been explicitly marked as complete.
	IsComplete bool `json:"-"`

	// Tracks holds the complete set of track descriptions in the catalog.
	Tracks []Track `json:"-"`
	// PublishTracks holds tracks to which subscribers may publish data, such as
	// logs or metrics (draft-ietf-moq-msf-02 §5.1.5).
	PublishTracks []Track `json:"-"`
	// InitDataList holds referenced initialization data entries; tracks point
	// at an entry by ID via the initRef attribute (draft-ietf-moq-msf-02 §5.1.7).
	InitDataList []InitDataRef `json:"-"`

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
// String fields are plain strings, whereas optional numeric and boolean
// parameters are pointer-typed.
//
// Naming fields (Namespace, Name, Depends track names, and the parent fields
// of clone and update operations) map to MOQT bytes as the UTF-8 encoding of
// the string, with no normalization or case folding, and compare byte for
// byte (draft-ietf-moq-msf-02 §11.2).
//
// ExtraFields and presentFields are helpers used by the custom JSON
// marshaling/unmarshaling logic for round‑tripping unknown keys and tracking
// which fields were declared.
type Track struct {
	// Namespace of the track as a '/'-delimited string of 1 to 32 non-empty
	// Track Namespace Fields (see SplitNamespace). If empty, the catalog's
	// DefaultNamespace is inherited; a declared namespace replaces it.
	Namespace string `json:"-"`
	// Name of the track; non-empty, and the Full Track Name (namespace and
	// name) is unique within the catalog.
	Name string `json:"-"`
	// Packaging mode string such as "loc" or "mediatimeline".
	Packaging Packaging `json:"-"`
	// PackagingMetadata holds additional parameters for the packaging as a
	// semicolon-separated list of key=value pairs (draft-ietf-moq-msf-02
	// §5.2.5). See PackagingMetadataValue and StreamMapping.
	PackagingMetadata string `json:"-"`
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
	// InitRef points at the id of an entry in the catalog InitDataList
	// (draft-ietf-moq-msf-02 §5.2.14).
	InitRef string `json:"-"`
	// Buffers is the optional target jitter-buffer configuration.
	Buffers *Buffers `json:"-"`
	// Template is the optional media-timeline template for fixed-duration
	// segments (draft-ietf-moq-msf-02 §5.2.16 / §7.4).
	Template *Template `json:"-"`
	// Depends lists the track names and render groups this track depends on
	// (draft-ietf-moq-msf-02 §5.2.15). Every entry must resolve within the
	// catalog.
	Depends []Dependency `json:"-"`
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
	// Bitrate is the maximum bitrate in bits per second (draft-ietf-moq-msf-02 §5.2.23).
	Bitrate *int64 `json:"-"`
	// AvgBitrate is the average bitrate over the track lifetime (§5.2.24).
	AvgBitrate *int64 `json:"-"`
	// MaxGopDuration is the maximum duration between random access points, in ms (§5.2.25).
	MaxGopDuration *int64 `json:"-"`
	// MaxGroupDuration is the maximum duration of any MOQT Group, in ms (§5.2.26).
	MaxGroupDuration *int64 `json:"-"`
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
	// ConnectionURI is the MOQT endpoint URI for a publish track (§5.2.37),
	// encoded under the JSON key "connectionURI".
	ConnectionURI string `json:"-"`
	// EncryptionScheme identifies the encryption scheme protecting the track (§5.2.38).
	EncryptionScheme string `json:"-"`
	// CipherSuite identifies the AEAD cipher suite; required when
	// EncryptionScheme is EncryptionSchemeMoQSecureObjects (§5.2.39).
	CipherSuite string `json:"-"`
	// KeyID identifies the key material used for encryption (§5.2.40).
	KeyID string `json:"-"`
	// TrackBaseKey is the base64-encoded base key material for the track (§5.2.41).
	TrackBaseKey string `json:"-"`
	// AuthInfo signals that authorization is required, keyed by scheme
	// (§5.2.42). Publish tracks also use it for publish authorization.
	AuthInfo map[string]json.RawMessage `json:"-"`
	// Accessibility lists embedded accessibility features (§5.2.44).
	Accessibility []AccessibilityDescriptor `json:"-"`
	// AccessibleWindowTime is how far behind the live edge, in wallclock
	// milliseconds, the track's content remains accessible (§5.2.45).
	AccessibleWindowTime *int64 `json:"-"`
	// KeyframeSpacing is the number of objects per keyframe interval (§5.2.46).
	KeyframeSpacing *int64 `json:"-"`

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
	clone.PublishTracks = cloneTracks(c.PublishTracks)
	clone.InitDataList = cloneInitDataRefs(c.InitDataList)
	clone.ExtraFields = cloneRawMessages(c.ExtraFields)
	return clone
}

// Validate checks whether the catalog satisfies the package's MSF draft-02
// rules: the version, the per-track rules including the required fields per
// (packaging, role) of §5.2.4.1, unique Full Track Names, the Initialization
// Data List, that every depends entry resolves (§5.2.15), and that tracks of
// an alternate group share accessibleWindowTime (§5.2.45).
//
// A catalog that uses variable substitution must be validated after
// substitution (§5.4.1, §13.5); Catalog.ResolveVariables does so.
func (c Catalog) Validate() error {
	var problems []string

	switch c.Version {
	case CurrentVersion:
	case "":
		problems = append(problems, "catalog version is required")
	default:
		problems = append(problems, fmt.Sprintf("unsupported catalog version %q; want %q", c.Version, CurrentVersion))
	}
	for i := range c.Tracks {
		if errs := c.Tracks[i].validate(trackListTracks); len(errs) > 0 {
			prefix := "tracks[" + itoa(i) + "]: "
			for _, err := range errs {
				problems = append(problems, prefix+err)
			}
		}
	}
	for i := range c.PublishTracks {
		if errs := c.PublishTracks[i].validate(trackListPublishTracks); len(errs) > 0 {
			prefix := "publishTracks[" + itoa(i) + "]: "
			for _, err := range errs {
				problems = append(problems, prefix+err)
			}
		}
	}
	// Validate unique track identities.
	// For small catalogs (N<=16), O(N^2) loop is faster and avoids map allocations on the happy path.
	// For larger catalogs, we fallback to a map to avoid O(N^2) performance degradation.
	if len(c.Tracks) > 16 {
		seen := make(map[TrackID]struct{}, len(c.Tracks))
		for i := range c.Tracks {
			id := c.Tracks[i].ID(c.DefaultNamespace)
			if _, ok := seen[id]; ok {
				problems = append(problems, fmt.Sprintf("tracks[%d]: duplicate track identity %q", i, id.String()))
				continue
			}
			seen[id] = struct{}{}
		}
	} else if len(c.Tracks) > 1 {
		var seen [16]TrackID
		seen[0] = c.Tracks[0].ID(c.DefaultNamespace)
		numSeen := 1
		for i := 1; i < len(c.Tracks); i++ {
			id := c.Tracks[i].ID(c.DefaultNamespace)
			isDuplicate := false
			for j := 0; j < numSeen; j++ {
				if seen[j] == id {
					problems = append(problems, fmt.Sprintf("tracks[%d]: duplicate track identity %q", i, id.String()))
					isDuplicate = true
					break
				}
			}
			if !isDuplicate {
				seen[numSeen] = id
				numSeen++
			}
		}
	}

	// Validate the Initialization Data List: unique ids, known type, and that
	// every track initRef resolves to a declared entry (draft-ietf-moq-msf-02 §5.1.7/§5.2.14).
	initIDs := make(map[string]struct{}, len(c.InitDataList))
	for i, ref := range c.InitDataList {
		if err := ref.validate(); err != nil {
			problems = append(problems, "initDataList["+itoa(i)+"]: "+err.Error())
		}
		if ref.ID == "" {
			problems = append(problems, "initDataList["+itoa(i)+"]: id is required")
			continue
		}
		if _, ok := initIDs[ref.ID]; ok {
			problems = append(problems, fmt.Sprintf("initDataList[%d]: duplicate init id %q", i, ref.ID))
			continue
		}
		initIDs[ref.ID] = struct{}{}
	}
	if len(initIDs) > 0 {
		checkRef := func(prefix, ref string) {
			if ref == "" {
				return
			}
			if _, ok := initIDs[ref]; !ok {
				problems = append(problems, fmt.Sprintf("%s: initRef %q does not match any initDataList id", prefix, ref))
			}
		}
		for i := range c.Tracks {
			checkRef("tracks["+itoa(i)+"]", c.Tracks[i].InitRef)
		}
		for i := range c.PublishTracks {
			checkRef("publishTracks["+itoa(i)+"]", c.PublishTracks[i].InitRef)
		}
	}

	problems = c.appendDependencyProblems(problems)
	problems = c.appendAltGroupProblems(problems)

	return newValidationError(problems)
}

// appendDependencyProblems checks that every depends entry resolves
// (draft-ietf-moq-msf-02 §5.2.15): a track name must match the Track Name of
// exactly one other track, and a renderGroup must match the renderGroup of
// some track. Both tracks and publishTracks are searched, since event
// timeline publish tracks depend on media tracks.
func (c Catalog) appendDependencyProblems(problems []string) []string {
	if !hasDependencies(c.Tracks) && !hasDependencies(c.PublishTracks) {
		return problems
	}

	names := make(map[string]int, len(c.Tracks)+len(c.PublishTracks))
	groups := make(map[int64]struct{})
	for _, list := range [2][]Track{c.Tracks, c.PublishTracks} {
		for i := range list {
			names[list[i].Name]++
			if list[i].RenderGroup != nil {
				groups[*list[i].RenderGroup] = struct{}{}
			}
		}
	}

	check := func(field string, list []Track) {
		for i := range list {
			track := &list[i]
			for j, dep := range track.Depends {
				if dep.validate() != nil {
					continue // reported by Track.validate
				}
				if dep.RenderGroup != nil {
					if _, ok := groups[*dep.RenderGroup]; !ok {
						problems = append(problems, fmt.Sprintf("%s[%d]: depends[%d]: renderGroup %d does not match any track", field, i, j, *dep.RenderGroup))
					}
					continue
				}
				others := names[dep.TrackName]
				if dep.TrackName == track.Name {
					others--
				}
				switch {
				case others == 0:
					problems = append(problems, fmt.Sprintf("%s[%d]: depends[%d]: track name %q does not match any other track", field, i, j, dep.TrackName))
				case others > 1:
					problems = append(problems, fmt.Sprintf("%s[%d]: depends[%d]: track name %q matches %d tracks; it must match exactly one", field, i, j, dep.TrackName, others))
				}
			}
		}
	}
	check("tracks", c.Tracks)
	check("publishTracks", c.PublishTracks)
	return problems
}

// hasDependencies reports whether any track declares depends entries.
func hasDependencies(tracks []Track) bool {
	for i := range tracks {
		if len(tracks[i].Depends) > 0 {
			return true
		}
	}
	return false
}

// appendAltGroupProblems checks that tracks of the same alternate group carry
// the same accessibleWindowTime (draft-ietf-moq-msf-02 §5.2.45).
func (c Catalog) appendAltGroupProblems(problems []string) []string {
	type window struct {
		set   bool
		value int64
	}
	var windows map[int64]window
	for i := range c.Tracks {
		track := &c.Tracks[i]
		if track.AltGroup == nil {
			continue
		}
		if windows == nil {
			windows = make(map[int64]window)
		}
		current := window{set: track.AccessibleWindowTime != nil}
		if current.set {
			current.value = *track.AccessibleWindowTime
		}
		first, seen := windows[*track.AltGroup]
		if !seen {
			windows[*track.AltGroup] = current
			continue
		}
		if first != current {
			problems = append(problems, fmt.Sprintf("tracks[%d]: accessibleWindowTime must match the other tracks of altGroup %d", i, *track.AltGroup))
		}
	}
	return problems
}

// ApplyDelta applies a catalog delta to the receiver and returns the updated
// catalog (draft-ietf-moq-msf-02 §5.3). Operations apply in order, each to the
// result of the previous one. An illogical operation (adding or cloning to an
// existing Full Track Name, removing or updating an unknown track) rejects the
// whole delta, as does a result that fails Validate.
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
	maps.Copy(result.ExtraFields, cloneRawMessages(delta.ExtraFields))

	for _, op := range delta.operations() {
		switch op.kind {
		case deltaOperationAdd:
			for _, track := range delta.AddTracks[op.lo:op.hi] {
				if err := result.addTrack(track); err != nil {
					return Catalog{}, err
				}
			}
		case deltaOperationRemove:
			for _, track := range delta.RemoveTracks[op.lo:op.hi] {
				if err := result.removeTrack(track); err != nil {
					return Catalog{}, err
				}
			}
		case deltaOperationClone:
			for _, track := range delta.CloneTracks[op.lo:op.hi] {
				if err := result.cloneTrack(track); err != nil {
					return Catalog{}, err
				}
			}
		case deltaOperationUpdate:
			for _, track := range delta.UpdateTracks[op.lo:op.hi] {
				if err := result.updateTrack(track); err != nil {
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
// the delta-only deltaUpdate field, and documents that begin with a byte order
// mark, are not UTF-8, or contain an unpaired surrogate escape
// (draft-ietf-moq-msf-02 §11.2.1). Validation must be performed separately if
// desired.
func ParseCatalog(data []byte) (Catalog, error) {
	// The JSON decoder rejects a leading BOM before UnmarshalJSON runs, with
	// a less helpful message.
	if err := checkNoBOM(data); err != nil {
		return Catalog{}, err
	}
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
	parentID := TrackID{Namespace: parentEffectiveNamespace(track.ParentNamespace, c.DefaultNamespace), Name: track.ParentName}
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

// updateTrack overrides the declared fields of an existing track
// (draft-ietf-moq-msf-02 §5.1.6 "update"). An update that renames the track
// must not collide with another track, and isLive must not go from false back
// to true (§5.2.8).
func (c *Catalog) updateTrack(update TrackUpdate) error {
	targetID := TrackID{Namespace: parentEffectiveNamespace(update.ParentNamespace, c.DefaultNamespace), Name: update.ParentName}
	target, idx, ok := c.findTrack(targetID)
	if !ok {
		return fmt.Errorf("msf: cannot update unknown track %q", targetID.String())
	}
	if target.IsLive != nil && !*target.IsLive && update.hasField("isLive") && update.IsLive != nil && *update.IsLive {
		return fmt.Errorf("msf: cannot update track %q: isLive must not change from false to true", targetID.String())
	}

	updated := target.Clone()
	updated.applyOverrides(update.Track)

	if newID := updated.ID(c.DefaultNamespace); newID != targetID {
		if _, other, exists := c.findTrack(newID); exists {
			return fmt.Errorf("msf: cannot update track %q into duplicate track %q at index %d", targetID.String(), newID.String(), other)
		}
	}

	c.Tracks[idx] = updated
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

// MarshalJSON encodes the independent catalog in the draft-02 JSON shape.
// initDataList is emitted last, after tracks, as §5.1.7 recommends.
func (c Catalog) MarshalJSON() ([]byte, error) {
	obj := make(map[string]any, len(c.ExtraFields)+2)
	for key, raw := range c.ExtraFields {
		obj[key] = cloneRawMessage(raw)
	}
	if c.Version != "" {
		obj["version"] = c.Version
	}
	if c.GeneratedAt != nil {
		obj["generatedAt"] = *c.GeneratedAt
	}
	if c.IsComplete {
		obj["isComplete"] = true
	}
	// tracks is a required field (§5.1.4), so an empty list encodes as [].
	if c.Tracks != nil {
		obj["tracks"] = c.Tracks
	} else {
		obj["tracks"] = []Track{}
	}
	if len(c.PublishTracks) > 0 {
		obj["publishTracks"] = c.PublishTracks
	}
	delete(obj, "initDataList")
	encoded, err := json.Marshal(obj)
	if err != nil || len(c.InitDataList) == 0 {
		return encoded, err
	}
	initData, err := json.Marshal(c.InitDataList)
	if err != nil {
		return nil, err
	}
	// Splice initDataList in as the last member of the object.
	encoded = append(encoded[:len(encoded)-1], `,"initDataList":`...)
	encoded = append(encoded, initData...)
	return append(encoded, '}'), nil
}

// UnmarshalJSON decodes the independent catalog shape and rejects delta-only fields.
func (c *Catalog) UnmarshalJSON(data []byte) error {
	*c = Catalog{}
	c.ExtraFields = make(map[string]json.RawMessage)

	if err := checkCatalogText(data); err != nil {
		return err
	}
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
		case "publishTracks":
			if err := json.Unmarshal(entry.Value, &c.PublishTracks); err != nil {
				return err
			}
		case "initDataList":
			if err := json.Unmarshal(entry.Value, &c.InitDataList); err != nil {
				return err
			}
		case "deltaUpdate":
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
	clone.Depends = cloneDependencies(t.Depends)
	clone.ExtraFields = cloneRawMessages(t.ExtraFields)
	clone.presentFields = maps.Clone(t.presentFields)
	if t.IsLive != nil {
		value := *t.IsLive
		clone.IsLive = &value
	}
	clone.TargetLatency = cloneInt64Ptr(t.TargetLatency)
	clone.RenderGroup = cloneInt64Ptr(t.RenderGroup)
	clone.AltGroup = cloneInt64Ptr(t.AltGroup)
	clone.Buffers = t.Buffers.Clone()
	clone.Template = t.Template.Clone()
	clone.TemporalID = cloneInt64Ptr(t.TemporalID)
	clone.SpatialID = cloneInt64Ptr(t.SpatialID)
	clone.Framerate = cloneInt64Ptr(t.Framerate)
	clone.Timescale = cloneInt64Ptr(t.Timescale)
	clone.Bitrate = cloneInt64Ptr(t.Bitrate)
	clone.AvgBitrate = cloneInt64Ptr(t.AvgBitrate)
	clone.MaxGopDuration = cloneInt64Ptr(t.MaxGopDuration)
	clone.MaxGroupDuration = cloneInt64Ptr(t.MaxGroupDuration)
	clone.Width = cloneInt64Ptr(t.Width)
	clone.Height = cloneInt64Ptr(t.Height)
	clone.SampleRate = cloneInt64Ptr(t.SampleRate)
	clone.DisplayWidth = cloneInt64Ptr(t.DisplayWidth)
	clone.DisplayHeight = cloneInt64Ptr(t.DisplayHeight)
	clone.TrackDuration = cloneInt64Ptr(t.TrackDuration)
	clone.AccessibleWindowTime = cloneInt64Ptr(t.AccessibleWindowTime)
	clone.KeyframeSpacing = cloneInt64Ptr(t.KeyframeSpacing)
	if t.AuthInfo != nil {
		clone.AuthInfo = cloneRawMessages(t.AuthInfo)
	}
	clone.Accessibility = cloneAccessibility(t.Accessibility)
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

// trackList identifies the catalog array a track is declared in, since some
// packaging values are only valid in one of them.
type trackList uint8

const (
	// trackListTracks is the tracks array, also used for delta add and clone
	// results.
	trackListTracks trackList = iota
	// trackListPublishTracks is the publishTracks array.
	trackListPublishTracks
)

// requiredLOCFields returns the fields a loc track with role must also carry
// (draft-ietf-moq-msf-02 §5.2.4.1, Table 6). Roles not listed have no
// requirement beyond the fields every track carries.
func requiredLOCFields(role Role) []string {
	switch role {
	case RoleAudio:
		return requiredLOCAudioFields[:]
	case RoleVideo:
		return requiredLOCVideoFields[:]
	case RoleAudioDescription:
		return requiredLOCAudioDescriptionFields[:]
	case RoleSignLanguage:
		return requiredLOCSignLanguageFields[:]
	default:
		return nil
	}
}

// The rows of Table 6 for loc packaging; read-only.
var (
	requiredLOCAudioFields            = [...]string{"codec", "bitrate", "samplerate", "channelConfig"}
	requiredLOCVideoFields            = [...]string{"codec", "bitrate", "framerate"}
	requiredLOCAudioDescriptionFields = [...]string{"codec", "samplerate", "channelConfig", "lang"}
	requiredLOCSignLanguageFields     = [...]string{"codec", "framerate"}
)

// validate checks track-level constraints of draft-ietf-moq-msf-02 for a
// track declared in list. Cross-track constraints, such as unique Full Track
// Names and depends resolution, are checked by Catalog.Validate.
func (t Track) validate(list trackList) []string {
	var problems []string
	if t.Name == "" {
		problems = append(problems, "name is required and must not be empty")
	} else if err := validateNameText(t.Name); err != nil {
		problems = append(problems, "name: "+err.Error())
	}
	if t.Namespace != "" || t.hasPresentField("namespace") {
		if err := validateNamespace(t.Namespace); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if t.Packaging == "" {
		problems = append(problems, "packaging is required")
	}
	if t.IsLive == nil {
		problems = append(problems, "isLive is required")
	}
	if t.TrackDuration != nil && t.IsLive != nil && *t.IsLive {
		problems = append(problems, "trackDuration must not be present when isLive is true")
	}
	if t.TargetLatency != nil && t.Buffers != nil {
		problems = append(problems, "targetLatency and buffers must not both be present")
	}
	if t.PackagingMetadata != "" || t.hasPresentField("packagingMetadata") {
		if err := validatePackagingMetadata(t.PackagingMetadata, t.Packaging); err != nil {
			problems = append(problems, err.Error())
		}
	}
	for i, dep := range t.Depends {
		if err := dep.validate(); err != nil {
			problems = append(problems, "depends["+itoa(i)+"]: "+err.Error())
		}
	}
	if _, ok := t.ExtraFields["parentName"]; ok {
		problems = append(problems, "parentName must only appear in a clone or update operation")
	}
	if _, ok := t.ExtraFields["parentNamespace"]; ok {
		problems = append(problems, "parentNamespace must only appear in a clone or update operation")
	}

	// eventType is required for, and only allowed on, eventtimeline tracks (§5.2.6).
	if t.Packaging == PackagingEventTimeline {
		if t.EventType == "" {
			problems = append(problems, "eventType is required for eventtimeline tracks")
		}
	} else if t.EventType != "" {
		problems = append(problems, "eventType must only be set for eventtimeline tracks")
	}

	switch t.Packaging {
	case PackagingLOC:
		for _, field := range requiredLOCFields(t.Role) {
			if !t.hasField(field) {
				problems = append(problems, field+" is required for loc tracks with role "+string(t.Role))
			}
		}
	case PackagingMediaTimeline:
		// §7.2: depends lists the tracks and render groups the timeline
		// applies to. §7.1: a media timeline track uses either the explicit
		// format (its payload) or the template format, never both.
		if len(t.Depends) == 0 {
			problems = append(problems, "mediatimeline tracks must declare depends")
		}
		if t.Template != nil {
			problems = append(problems, "mediatimeline tracks must not also carry a template")
		}
	case PackagingMoqLog:
		problems = t.appendPublishOnlyProblems(problems, list, RoleLog)
	case PackagingMoqMetrics:
		problems = t.appendPublishOnlyProblems(problems, list, RoleMetrics)
	}

	// cipherSuite is required only for moq-secure-objects (§5.2.39).
	if t.EncryptionScheme == EncryptionSchemeMoQSecureObjects && t.CipherSuite == "" {
		problems = append(problems, "cipherSuite is required when encryptionScheme is "+EncryptionSchemeMoQSecureObjects)
	}

	return problems
}

// appendPublishOnlyProblems checks the catalog requirements shared by log and
// metrics tracks (draft-ietf-moq-msf-02 §9.2, §9.4, §10.2, §10.4): declared in
// publishTracks, with the matching role, and named by the ASCII decimal
// priority or granularity level "0" to "7".
func (t Track) appendPublishOnlyProblems(problems []string, list trackList, role Role) []string {
	if list != trackListPublishTracks {
		problems = append(problems, string(t.Packaging)+" tracks must be declared in publishTracks")
	}
	if t.Role != role {
		problems = append(problems, string(t.Packaging)+" tracks must have role "+string(role))
	}
	if len(t.Name) != 1 || t.Name[0] < '0' || t.Name[0] > '7' {
		problems = append(problems, string(t.Packaging)+" track name must be a level from \"0\" to \"7\"")
	}
	return problems
}

// hasPresentField reports whether field was explicitly present in the decoded
// JSON, even with an empty value.
func (t Track) hasPresentField(field string) bool {
	_, ok := t.presentFields[field]
	return ok
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
	if override.hasField("packagingMetadata") {
		t.PackagingMetadata = override.PackagingMetadata
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
	if override.hasField("initRef") {
		t.InitRef = override.InitRef
	}
	if override.hasField("buffers") {
		t.Buffers = override.Buffers.Clone()
	}
	if override.hasField("template") {
		t.Template = override.Template.Clone()
	}
	if override.hasField("depends") {
		t.Depends = cloneDependencies(override.Depends)
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
	if override.hasField("avgBitrate") {
		t.AvgBitrate = cloneInt64Ptr(override.AvgBitrate)
	}
	if override.hasField("maxGopDuration") {
		t.MaxGopDuration = cloneInt64Ptr(override.MaxGopDuration)
	}
	if override.hasField("maxGroupDuration") {
		t.MaxGroupDuration = cloneInt64Ptr(override.MaxGroupDuration)
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
	if override.hasField("connectionURI") {
		t.ConnectionURI = override.ConnectionURI
	}
	if override.hasField("encryptionScheme") {
		t.EncryptionScheme = override.EncryptionScheme
	}
	if override.hasField("cipherSuite") {
		t.CipherSuite = override.CipherSuite
	}
	if override.hasField("keyId") {
		t.KeyID = override.KeyID
	}
	if override.hasField("trackBaseKey") {
		t.TrackBaseKey = override.TrackBaseKey
	}
	if override.hasField("authInfo") {
		t.AuthInfo = cloneRawMessages(override.AuthInfo)
	}
	if override.hasField("accessibility") {
		t.Accessibility = cloneAccessibility(override.Accessibility)
	}
	if override.hasField("accessibleWindowTime") {
		t.AccessibleWindowTime = cloneInt64Ptr(override.AccessibleWindowTime)
	}
	if override.hasField("keyframeSpacing") {
		t.KeyframeSpacing = cloneInt64Ptr(override.KeyframeSpacing)
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
	if t.PackagingMetadata != "" {
		obj["packagingMetadata"] = t.PackagingMetadata
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
	if t.InitRef != "" {
		obj["initRef"] = t.InitRef
	}
	if t.Buffers != nil {
		obj["buffers"] = t.Buffers
	}
	if t.Template != nil {
		obj["template"] = t.Template
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
	if t.AvgBitrate != nil {
		obj["avgBitrate"] = *t.AvgBitrate
	}
	if t.MaxGopDuration != nil {
		obj["maxGopDuration"] = *t.MaxGopDuration
	}
	if t.MaxGroupDuration != nil {
		obj["maxGroupDuration"] = *t.MaxGroupDuration
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
	if t.ConnectionURI != "" {
		obj["connectionURI"] = t.ConnectionURI
	}
	if t.EncryptionScheme != "" {
		obj["encryptionScheme"] = t.EncryptionScheme
	}
	if t.CipherSuite != "" {
		obj["cipherSuite"] = t.CipherSuite
	}
	if t.KeyID != "" {
		obj["keyId"] = t.KeyID
	}
	if t.TrackBaseKey != "" {
		obj["trackBaseKey"] = t.TrackBaseKey
	}
	if t.AuthInfo != nil {
		obj["authInfo"] = cloneRawMessages(t.AuthInfo)
	}
	if t.Accessibility != nil {
		obj["accessibility"] = t.Accessibility
	}
	if t.AccessibleWindowTime != nil {
		obj["accessibleWindowTime"] = *t.AccessibleWindowTime
	}
	if t.KeyframeSpacing != nil {
		obj["keyframeSpacing"] = *t.KeyframeSpacing
	}
	return obj
}

// MarshalJSON encodes the track using the draft-02 object form.
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
		case "packagingMetadata":
			if err := json.Unmarshal(value, &t.PackagingMetadata); err != nil {
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
		case "initRef":
			if err := json.Unmarshal(value, &t.InitRef); err != nil {
				return err
			}
		case "buffers":
			var v Buffers
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			t.Buffers = &v
		case "template":
			var v Template
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			t.Template = &v
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
		case "avgBitrate":
			if err := json.Unmarshal(value, &t.AvgBitrate); err != nil {
				return err
			}
		case "maxGopDuration":
			if err := json.Unmarshal(value, &t.MaxGopDuration); err != nil {
				return err
			}
		case "maxGroupDuration":
			if err := json.Unmarshal(value, &t.MaxGroupDuration); err != nil {
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
		case "connectionURI":
			if err := json.Unmarshal(value, &t.ConnectionURI); err != nil {
				return err
			}
		case "encryptionScheme":
			if err := json.Unmarshal(value, &t.EncryptionScheme); err != nil {
				return err
			}
		case "cipherSuite":
			if err := json.Unmarshal(value, &t.CipherSuite); err != nil {
				return err
			}
		case "keyId":
			if err := json.Unmarshal(value, &t.KeyID); err != nil {
				return err
			}
		case "trackBaseKey":
			if err := json.Unmarshal(value, &t.TrackBaseKey); err != nil {
				return err
			}
		case "authInfo":
			t.AuthInfo = make(map[string]json.RawMessage)
			if err := json.Unmarshal(value, &t.AuthInfo); err != nil {
				return err
			}
		case "accessibility":
			if err := json.Unmarshal(value, &t.Accessibility); err != nil {
				return err
			}
		case "accessibleWindowTime":
			if err := json.Unmarshal(value, &t.AccessibleWindowTime); err != nil {
				return err
			}
		case "keyframeSpacing":
			if err := json.Unmarshal(value, &t.KeyframeSpacing); err != nil {
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
	case "packagingMetadata":
		return t.PackagingMetadata != ""
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
	case "initRef":
		return t.InitRef != ""
	case "buffers":
		return t.Buffers != nil
	case "template":
		return t.Template != nil
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
	case "avgBitrate":
		return t.AvgBitrate != nil
	case "maxGopDuration":
		return t.MaxGopDuration != nil
	case "maxGroupDuration":
		return t.MaxGroupDuration != nil
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
	case "connectionURI":
		return t.ConnectionURI != ""
	case "encryptionScheme":
		return t.EncryptionScheme != ""
	case "cipherSuite":
		return t.CipherSuite != ""
	case "keyId":
		return t.KeyID != ""
	case "trackBaseKey":
		return t.TrackBaseKey != ""
	case "authInfo":
		return t.AuthInfo != nil
	case "accessibility":
		return t.Accessibility != nil
	case "accessibleWindowTime":
		return t.AccessibleWindowTime != nil
	case "keyframeSpacing":
		return t.KeyframeSpacing != nil
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

// itoa is a simple allocation-free integer to string conversion for small integers.
func itoa(i int) string {
	if i >= 0 && i < 10 {
		return string(byte('0' + i))
	}
	// Fallback for larger numbers
	return fmt.Sprint(i)
}

// --- InitDataRef -----------------------------------------------------------

// Clone returns a deep copy of the init-data reference.
func (r InitDataRef) Clone() InitDataRef {
	clone := r
	clone.ExtraFields = cloneRawMessages(r.ExtraFields)
	return clone
}

// PropertyType returns the property type named by Data for the
// InitDataTypeTrackProperty and InitDataTypeObjectProperty types. Data is
// hex notation, with or without a "0x" prefix.
func (r InitDataRef) PropertyType() (uint64, error) {
	if r.Type != InitDataTypeTrackProperty && r.Type != InitDataTypeObjectProperty {
		return 0, fmt.Errorf("msf: init data type %q does not name a property", r.Type)
	}
	digits := strings.TrimPrefix(strings.TrimPrefix(r.Data, "0x"), "0X")
	if digits == "" {
		return 0, fmt.Errorf("msf: init data property type %q is not hex notation", r.Data)
	}
	value, err := strconv.ParseUint(digits, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("msf: init data property type %q is not hex notation: %w", r.Data, err)
	}
	return value, nil
}

// validate checks the entry's type, and for the property types that Data is
// a hex property type. An empty type is accepted as before draft-02.
func (r InitDataRef) validate() error {
	switch r.Type {
	case "", InitDataTypeInline:
		return nil
	case InitDataTypeTrackProperty, InitDataTypeObjectProperty:
		if _, err := r.PropertyType(); err != nil {
			return fmt.Errorf("data %q must be a property type in hex notation", r.Data)
		}
		return nil
	default:
		return fmt.Errorf("type %q must be %q, %q or %q", r.Type, InitDataTypeInline, InitDataTypeTrackProperty, InitDataTypeObjectProperty)
	}
}

// MarshalJSON encodes the init-data reference as the draft-02 object form.
func (r InitDataRef) MarshalJSON() ([]byte, error) {
	obj := make(map[string]any, len(r.ExtraFields)+3)
	for key, raw := range r.ExtraFields {
		obj[key] = cloneRawMessage(raw)
	}
	if r.ID != "" {
		obj["id"] = r.ID
	}
	if r.Type != "" {
		obj["type"] = r.Type
	}
	if r.Data != "" {
		obj["data"] = r.Data
	}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes the init-data reference, preserving unknown keys.
func (r *InitDataRef) UnmarshalJSON(data []byte) error {
	*r = InitDataRef{ExtraFields: make(map[string]json.RawMessage)}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key, value := range raw {
		switch key {
		case "id":
			if err := json.Unmarshal(value, &r.ID); err != nil {
				return err
			}
		case "type":
			if err := json.Unmarshal(value, &r.Type); err != nil {
				return err
			}
		case "data":
			if err := json.Unmarshal(value, &r.Data); err != nil {
				return err
			}
		default:
			r.ExtraFields[key] = cloneRawMessage(value)
		}
	}
	return nil
}

// cloneInitDataRefs returns a deep copy of an InitDataRef slice.
func cloneInitDataRefs(in []InitDataRef) []InitDataRef {
	if in == nil {
		return nil
	}
	out := make([]InitDataRef, len(in))
	for i, ref := range in {
		out[i] = ref.Clone()
	}
	return out
}

// --- Buffers ---------------------------------------------------------------

// Clone returns a deep copy of the buffers configuration. Safe on a nil receiver.
func (b *Buffers) Clone() *Buffers {
	if b == nil {
		return nil
	}
	clone := *b
	clone.Target = cloneInt64Ptr(b.Target)
	clone.Min = cloneInt64Ptr(b.Min)
	clone.Max = cloneInt64Ptr(b.Max)
	clone.ExtraFields = cloneRawMessages(b.ExtraFields)
	return &clone
}

// MarshalJSON encodes the buffers object, preserving unknown keys.
func (b Buffers) MarshalJSON() ([]byte, error) {
	obj := make(map[string]any, len(b.ExtraFields)+3)
	for key, raw := range b.ExtraFields {
		obj[key] = cloneRawMessage(raw)
	}
	if b.Target != nil {
		obj["target"] = *b.Target
	}
	if b.Min != nil {
		obj["min"] = *b.Min
	}
	if b.Max != nil {
		obj["max"] = *b.Max
	}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes the buffers object, preserving unknown keys.
func (b *Buffers) UnmarshalJSON(data []byte) error {
	*b = Buffers{ExtraFields: make(map[string]json.RawMessage)}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key, value := range raw {
		switch key {
		case "target":
			var v int64
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			b.Target = &v
		case "min":
			var v int64
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			b.Min = &v
		case "max":
			var v int64
			if err := json.Unmarshal(value, &v); err != nil {
				return err
			}
			b.Max = &v
		default:
			b.ExtraFields[key] = cloneRawMessage(value)
		}
	}
	return nil
}

// --- AccessibilityDescriptor -----------------------------------------------

// Clone returns a deep copy of the accessibility descriptor.
func (a AccessibilityDescriptor) Clone() AccessibilityDescriptor {
	clone := a
	clone.ExtraFields = cloneRawMessages(a.ExtraFields)
	return clone
}

// MarshalJSON encodes the accessibility descriptor, preserving unknown keys.
func (a AccessibilityDescriptor) MarshalJSON() ([]byte, error) {
	obj := make(map[string]any, len(a.ExtraFields)+2)
	for key, raw := range a.ExtraFields {
		obj[key] = cloneRawMessage(raw)
	}
	if a.Scheme != "" {
		obj["scheme"] = a.Scheme
	}
	if a.Value != "" {
		obj["value"] = a.Value
	}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes the accessibility descriptor, preserving unknown keys.
func (a *AccessibilityDescriptor) UnmarshalJSON(data []byte) error {
	*a = AccessibilityDescriptor{ExtraFields: make(map[string]json.RawMessage)}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key, value := range raw {
		switch key {
		case "scheme":
			if err := json.Unmarshal(value, &a.Scheme); err != nil {
				return err
			}
		case "value":
			if err := json.Unmarshal(value, &a.Value); err != nil {
				return err
			}
		default:
			a.ExtraFields[key] = cloneRawMessage(value)
		}
	}
	return nil
}

// cloneAccessibility returns a deep copy of an AccessibilityDescriptor slice.
func cloneAccessibility(in []AccessibilityDescriptor) []AccessibilityDescriptor {
	if in == nil {
		return nil
	}
	out := make([]AccessibilityDescriptor, len(in))
	for i, desc := range in {
		out[i] = desc.Clone()
	}
	return out
}

var (
	_ json.Marshaler   = InitDataRef{}
	_ json.Unmarshaler = (*InitDataRef)(nil)
	_ json.Marshaler   = Buffers{}
	_ json.Unmarshaler = (*Buffers)(nil)
	_ json.Marshaler   = AccessibilityDescriptor{}
	_ json.Unmarshaler = (*AccessibilityDescriptor)(nil)
)
