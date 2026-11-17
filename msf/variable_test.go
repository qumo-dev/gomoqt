package msf

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveVariables(t *testing.T) {
	tests := map[string]struct {
		input  string
		vars   map[string]string
		want   string
		wantOk bool
	}{
		"no variables": {
			input:  "live/demo/video",
			vars:   map[string]string{"token": "abc"},
			want:   "live/demo/video",
			wantOk: true,
		},
		"single variable": {
			input:  "live/%token%/video",
			vars:   map[string]string{"token": "abc123"},
			want:   "live/abc123/video",
			wantOk: true,
		},
		"multiple variables": {
			input:  "%ns%/%name%",
			vars:   map[string]string{"ns": "live", "name": "demo"},
			want:   "live/demo",
			wantOk: true,
		},
		"adjacent variables": {
			input:  "%a%%b%",
			vars:   map[string]string{"a": "x", "b": "y"},
			want:   "xy",
			wantOk: true,
		},
		"hyphen and underscore names": {
			input:  "%track-1%/%sub_name%",
			vars:   map[string]string{"track-1": "v", "sub_name": "a"},
			want:   "v/a",
			wantOk: true,
		},
		"any name characters": {
			input:  "live/%bad/name%/%na me%",
			vars:   map[string]string{"bad/name": "abc", "na me": "x"},
			want:   "live/abc/x",
			wantOk: true,
		},
		"at sign in value": {
			input:  "user=%user%",
			vars:   map[string]string{"user": "alice@domain"},
			want:   "user=alice@domain",
			wantOk: true,
		},
		"plus slash equals period in value": {
			input:  "%token%",
			vars:   map[string]string{"token": "a+b/c=d.e"},
			want:   "a+b/c=d.e",
			wantOk: true,
		},
		"missing variable": {
			input:  "live/%missing%/video",
			vars:   map[string]string{"token": "abc"},
			wantOk: false,
		},
		"invalid value characters": {
			input:  "v=%token%",
			vars:   map[string]string{"token": "bad;value"},
			wantOk: false,
		},
		"stray percent": {
			input:  "50% done",
			vars:   map[string]string{},
			wantOk: false,
		},
		"unterminated variable": {
			input:  "live/%token",
			vars:   map[string]string{"token": "abc"},
			wantOk: false,
		},
		"empty name between percents": {
			input:  "live/%%/video",
			vars:   map[string]string{"": "abc"},
			wantOk: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := ResolveVariables(tt.input, tt.vars)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestParseFragmentVariables(t *testing.T) {
	tests := map[string]struct {
		fragment string
		want     map[string]string
	}{
		"spec example skips the track locator": {
			fragment: "#msf:namespace--catalog&token=1234&id=c3e5s7w&event=xyz",
			want:     map[string]string{"token": "1234", "id": "c3e5s7w", "event": "xyz"},
		},
		"split at the first equals sign": {
			fragment: "token=YWJj==&cat=a=b",
			want:     map[string]string{"token": "YWJj==", "cat": "a=b"},
		},
		"first occurrence wins": {
			fragment: "a=1&a=2",
			want:     map[string]string{"a": "1"},
		},
		"empty name skipped and empty value kept": {
			fragment: "=x&b=",
			want:     map[string]string{"b": ""},
		},
		"empty fragment": {
			fragment: "",
			want:     map[string]string{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseFragmentVariables(tt.fragment))
		})
	}
}

func TestValidateVariableSyntax(t *testing.T) {
	tests := map[string]struct {
		input    string
		wantErr  bool
		errMatch string
	}{
		"plain string": {
			input:   "live/demo/video",
			wantErr: false,
		},
		"valid variable": {
			input:   "live/%token%/video",
			wantErr: false,
		},
		"multiple valid variables": {
			input:   "%a%-%b%",
			wantErr: false,
		},
		"any name characters": {
			input:   "live/%na me%/video",
			wantErr: false,
		},
		"unterminated variable": {
			input:    "live/%token",
			wantErr:  true,
			errMatch: "unterminated variable reference",
		},
		"empty name": {
			input:    "live/%%/video",
			wantErr:  true,
			errMatch: "empty variable name",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateVariableSyntax(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMatch != "" {
					assert.Contains(t, err.Error(), tt.errMatch)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCatalog_ResolveVariables(t *testing.T) {
	catalog := Catalog{
		Version:          CurrentVersion,
		DefaultNamespace: "live/demo",
		Tracks: []Track{{
			Namespace: "advertising/%event%",
			Name:      "tracking-%id%",
			Packaging: PackagingEventTimeline,
			EventType: "com.example.iab.vast",
			IsLive:    new(true),
			AuthInfo:  map[string]json.RawMessage{"cat": json.RawMessage(`"%token%"`)},
		}},
		PublishTracks: []Track{{
			Namespace:   "metrics.moq.arpa/metrics-v1/%resourceId%",
			Name:        "4",
			Packaging:   PackagingMoqMetrics,
			Role:        RoleMetrics,
			IsLive:      new(true),
			ExtraFields: map[string]json.RawMessage{"com.example.tag": json.RawMessage(`{"v":"%id%"}`)},
		}},
	}
	vars := ParseFragmentVariables("msf:live-demo--catalog&token=a+b/c=&id=c3e5s7w&event=xyz&resourceId=r1")

	resolved, err := catalog.ResolveVariables(vars)
	require.NoError(t, err)
	require.Len(t, resolved.Tracks, 1)
	assert.Equal(t, "live/demo", resolved.DefaultNamespace)
	assert.Equal(t, "advertising/xyz", resolved.Tracks[0].Namespace)
	assert.Equal(t, "tracking-c3e5s7w", resolved.Tracks[0].Name)
	assert.JSONEq(t, `"a+b/c="`, string(resolved.Tracks[0].AuthInfo["cat"]))
	require.Len(t, resolved.PublishTracks, 1)
	assert.Equal(t, "metrics.moq.arpa/metrics-v1/r1", resolved.PublishTracks[0].Namespace)
	assert.JSONEq(t, `{"v":"c3e5s7w"}`, string(resolved.PublishTracks[0].ExtraFields["com.example.tag"]))
	// The original catalog is not mutated.
	assert.Equal(t, "advertising/%event%", catalog.Tracks[0].Namespace)
	assert.JSONEq(t, `"%token%"`, string(catalog.Tracks[0].AuthInfo["cat"]))
}

func TestCatalog_ResolveVariables_Errors(t *testing.T) {
	tests := map[string]struct {
		track    Track
		vars     map[string]string
		errMatch string
	}{
		"missing variable": {
			track:    Track{Namespace: "live/%missing%", Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
			vars:     map[string]string{},
			errMatch: "failed to resolve variables",
		},
		"connectionURI is not substituted": {
			track:    Track{Name: "video", Packaging: PackagingLOC, IsLive: new(true), ConnectionURI: "moqt://%host%/live"},
			vars:     map[string]string{"host": "example.com"},
			errMatch: "variables in connectionURI are not substituted",
		},
		"result is validated": {
			track:    Track{Namespace: "live/%ns%", Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
			vars:     map[string]string{"ns": "a//b"},
			errMatch: "empty field",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			catalog := Catalog{Version: CurrentVersion, Tracks: []Track{tt.track}}
			resolved, err := catalog.ResolveVariables(tt.vars)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMatch)
			assert.Equal(t, Catalog{}, resolved)
		})
	}
}

func TestCatalog_ResolveVariables_KeepsLargeIntegers(t *testing.T) {
	generatedAt := int64(1<<53 + 1)
	catalog := Catalog{
		Version:     CurrentVersion,
		GeneratedAt: &generatedAt,
		Tracks:      []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
	}

	resolved, err := catalog.ResolveVariables(nil)
	require.NoError(t, err)
	require.NotNil(t, resolved.GeneratedAt)
	assert.Equal(t, generatedAt, *resolved.GeneratedAt)
}

func TestValidateVariableSyntax_Names(t *testing.T) {
	tests := map[string]struct {
		s       string
		wantErr bool
	}{
		"empty name":   {s: "x%%y", wantErr: true},
		"alphanumeric": {s: "%abc123%"},
		"slash":        {s: "%bad/name%"},
		"space":        {s: "%bad name%"},
		"unicode":      {s: "%naïve%"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateVariableSyntax(tt.s)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestIsValidVariableValue(t *testing.T) {
	tests := map[string]struct {
		value string
		want  bool
	}{
		"empty allowed": {"", true},
		"alphanumeric":  {"abc123", true},
		"hyphen":        {"a-b", true},
		"underscore":    {"a_b", true},
		"at sign":       {"alice@domain", true},
		"plus":          {"a+b", true},
		"slash":         {"a/b", true},
		"equals":        {"key=value", true},
		"period":        {"a.b", true},
		"space":         {"bad value", false},
		"percent":       {"bad%value", false},
		"comma":         {"a,b", false},
		"semicolon":     {"a;b", false},
		"quote":         {`a"b`, false},
		"ampersand":     {"a&b", false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidVariableValue(tt.value))
		})
	}
}

// Ensure a catalog with nothing to resolve comes back unchanged.
func TestCatalog_ResolveVariables_NoPercent(t *testing.T) {
	catalog := Catalog{
		Version: CurrentVersion,
		Tracks: []Track{{
			Namespace: "live/demo",
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(true),
		}},
	}

	resolved, err := catalog.ResolveVariables(map[string]string{"unused": "x"})
	require.NoError(t, err)
	assert.Equal(t, "live/demo", resolved.Tracks[0].Namespace)
}

// TestCatalog_ResolveVariables_AgreesWithValidate checks that the resolver's
// JSON round trip cannot hide a validation problem: with nothing to
// substitute, ResolveVariables fails exactly when Validate does.
func TestCatalog_ResolveVariables_AgreesWithValidate(t *testing.T) {
	tests := map[string]Catalog{
		"valid": {Version: CurrentVersion, Tracks: []Track{{Name: "v", Packaging: PackagingLOC, IsLive: new(true)}}},
		"missing isLive": {
			Version: CurrentVersion, Tracks: []Track{{Name: "v", Packaging: PackagingLOC}},
		},
		"loc video missing required fields": {
			Version: CurrentVersion, Tracks: []Track{{Name: "v", Packaging: PackagingLOC, Role: RoleVideo, IsLive: new(true)}},
		},
		"bad packagingMetadata": {
			Version: CurrentVersion, Tracks: []Track{{Name: "v", Packaging: PackagingLOC, IsLive: new(true), PackagingMetadata: "a=1;"}},
		},
		"parentName on a plain track": {
			Version: CurrentVersion,
			Tracks: []Track{{Name: "v", Packaging: PackagingLOC, IsLive: new(true),
				ExtraFields: map[string]json.RawMessage{"parentName": json.RawMessage(`"x"`)}}},
		},
		"duplicate across tracks and publishTracks": {
			Version:       CurrentVersion,
			Tracks:        []Track{{Name: "v", Packaging: PackagingLOC, IsLive: new(true)}},
			PublishTracks: []Track{{Name: "v", Packaging: PackagingLOC, IsLive: new(true)}},
		},
	}

	for name, catalog := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := catalog.ResolveVariables(nil)
			if catalog.Validate() == nil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestCatalog_ResolveVariables_SubstitutedEmptyNamespaceIsInvalid(t *testing.T) {
	catalog := Catalog{
		Version: CurrentVersion,
		Tracks:  []Track{{Namespace: "%ns%", Name: "v", Packaging: PackagingLOC, IsLive: new(true)}},
	}

	_, err := catalog.ResolveVariables(map[string]string{"ns": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace must not be empty")
}
