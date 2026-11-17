package msf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Dependency is one entry of a track's depends array (draft-ietf-moq-msf-02
// §5.2.15). An entry references either another track by its Track Name (a
// JSON String) or a render group by its renderGroup number (a JSON Number).
// Exactly one of TrackName and RenderGroup is set.
type Dependency struct {
	// TrackName is the Track Name of the track depended upon, for a JSON
	// String entry.
	TrackName string
	// RenderGroup is the renderGroup depended upon, for a JSON Number entry.
	RenderGroup *int64
}

// DependsOnTrack returns a dependency on the track named name.
func DependsOnTrack(name string) Dependency {
	return Dependency{TrackName: name}
}

// DependsOnRenderGroup returns a dependency on the render group group.
func DependsOnRenderGroup(group int64) Dependency {
	return Dependency{RenderGroup: &group}
}

// IsRenderGroup reports whether the dependency references a render group
// rather than a track name.
func (d Dependency) IsRenderGroup() bool {
	return d.RenderGroup != nil
}

// String returns the track name, or "renderGroup N" for a render-group
// dependency.
func (d Dependency) String() string {
	if d.RenderGroup != nil {
		return "renderGroup " + fmt.Sprint(*d.RenderGroup)
	}
	return d.TrackName
}

// validate checks that the dependency is well formed on its own; whether it
// resolves is checked against the whole catalog by Catalog.Validate.
func (d Dependency) validate() error {
	if d.RenderGroup != nil {
		if d.TrackName != "" {
			return errors.New("depends entry must reference either a track name or a renderGroup, not both")
		}
		return nil
	}
	if d.TrackName == "" {
		return errors.New("depends entry must not be an empty track name")
	}
	return validateNameText(d.TrackName)
}

var (
	_ json.Marshaler   = Dependency{}
	_ json.Unmarshaler = (*Dependency)(nil)
)

// MarshalJSON encodes a track-name dependency as a JSON String and a
// render-group dependency as a JSON Number.
func (d Dependency) MarshalJSON() ([]byte, error) {
	if d.RenderGroup != nil {
		return json.Marshal(*d.RenderGroup)
	}
	return json.Marshal(d.TrackName)
}

// UnmarshalJSON decodes a depends entry: a JSON String is a track name and a
// JSON Number is a renderGroup. Any other JSON type is rejected.
func (d *Dependency) UnmarshalJSON(data []byte) error {
	*d = Dependency{}
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &d.TrackName)
	}
	if bytes.Equal(data, []byte("null")) {
		return errors.New("msf: depends entry must be a track name string or a renderGroup integer, not null")
	}
	var group int64
	if err := json.Unmarshal(data, &group); err != nil {
		return fmt.Errorf("msf: depends entry must be a track name string or a renderGroup integer: %w", err)
	}
	d.RenderGroup = &group
	return nil
}

// cloneDependencies returns a deep copy of deps, keeping nil as nil.
func cloneDependencies(deps []Dependency) []Dependency {
	if deps == nil {
		return nil
	}
	out := make([]Dependency, len(deps))
	for i, d := range deps {
		out[i] = Dependency{TrackName: d.TrackName, RenderGroup: cloneInt64Ptr(d.RenderGroup)}
	}
	return out
}

// StreamMapping is the LOC to MOQT Stream mapping mode signaled by the
// streamMapping key of a loc track's packagingMetadata
// (draft-ietf-moq-msf-02 §4.1.1).
type StreamMapping string

const (
	// StreamMappingGroup sends all Objects of a Group in a single Subgroup,
	// in Object ID order. It is the default.
	StreamMappingGroup StreamMapping = "group"
	// StreamMappingObject sends each Object in its own Subgroup.
	StreamMappingObject StreamMapping = "object"
)

// packagingMetadataStreamMapping is the packagingMetadata key that carries the
// LOC stream mapping mode.
const packagingMetadataStreamMapping = "streamMapping"

// PackagingMetadataValue returns the value of key in the track's
// packagingMetadata (draft-ietf-moq-msf-02 §5.2.5), a semicolon-separated
// list of key=value pairs. If key appears more than once the first
// occurrence is used. Keys are case-sensitive.
func (t Track) PackagingMetadataValue(key string) (string, bool) {
	return packagingMetadataValue(t.PackagingMetadata, key)
}

// packagingMetadataValue returns the first value of key in metadata.
func packagingMetadataValue(metadata, key string) (string, bool) {
	if metadata == "" {
		return "", false
	}
	for pair := range strings.SplitSeq(metadata, ";") {
		k, v, ok := strings.Cut(pair, "=")
		if ok && k == key {
			return v, true
		}
	}
	return "", false
}

// StreamMapping returns the track's LOC stream mapping mode from the
// streamMapping key of its packagingMetadata, or StreamMappingGroup when the
// key is absent (draft-ietf-moq-msf-02 §4.1.1).
func (t Track) StreamMapping() StreamMapping {
	if v, ok := t.PackagingMetadataValue(packagingMetadataStreamMapping); ok {
		return StreamMapping(v)
	}
	return StreamMappingGroup
}

// validatePackagingMetadata checks the list syntax of draft-ietf-moq-msf-02
// §5.2.5, "a semicolon-separated list of key=value pairs" whose keys and
// values "MUST NOT contain whitespace, = or ;":
//   - an empty string is a list of no pairs;
//   - every item between semicolons must be a pair, so an empty item, as a
//     leading, trailing or doubled ';' produces, is rejected;
//   - keys must be non-empty; an empty value is allowed, as the text does not
//     forbid it;
//   - whitespace is any Unicode space (unicode.IsSpace).
//
// For loc packaging, a streamMapping value must be one of the defined modes.
func validatePackagingMetadata(metadata string, packaging Packaging) error {
	if metadata == "" {
		return nil
	}
	for pair := range strings.SplitSeq(metadata, ";") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			return fmt.Errorf("packagingMetadata entry %q must be a key=value pair", pair)
		}
		if strings.ContainsRune(v, '=') || strings.IndexFunc(pair, unicode.IsSpace) >= 0 {
			return fmt.Errorf("packagingMetadata entry %q must not contain whitespace or a second '='", pair)
		}
	}
	if packaging != PackagingLOC {
		return nil
	}
	if v, ok := packagingMetadataValue(metadata, packagingMetadataStreamMapping); ok {
		switch StreamMapping(v) {
		case StreamMappingGroup, StreamMappingObject:
		default:
			return fmt.Errorf("packagingMetadata streamMapping %q must be %q or %q", v, StreamMappingGroup, StreamMappingObject)
		}
	}
	return nil
}
