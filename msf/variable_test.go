package msf

import (
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
		"at sign in value": {
			input:  "user=%user%",
			vars:   map[string]string{"user": "alice@domain"},
			want:   "user=alice@domain",
			wantOk: true,
		},
		"missing variable": {
			input:  "live/%missing%/video",
			vars:   map[string]string{"token": "abc"},
			wantOk: false,
		},
		"invalid value characters": {
			input:  "v=%token%",
			vars:   map[string]string{"token": "bad/value"},
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
		"invalid name characters": {
			input:  "live/%bad/name%/video",
			vars:   map[string]string{"bad/name": "abc"},
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
		"stray percent with space": {
			input:    "50% off%",
			wantErr:  true,
			errMatch: "invalid variable name",
		},
		"unterminated variable": {
			input:    "live/%token",
			wantErr:  true,
			errMatch: "unterminated variable reference",
		},
		"empty name": {
			input:    "live/%%/video",
			wantErr:  true,
			errMatch: "invalid variable name",
		},
		"invalid name characters": {
			input:    "live/%na me%/video",
			wantErr:  true,
			errMatch: "invalid variable name",
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
		Version:          1,
		DefaultNamespace: "live/%ns%",
		Tracks: []Track{{
			Namespace: "live/%ns%",
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(true),
			Token:     "%token%",
		}},
	}

	vars := map[string]string{
		"ns":    "demo",
		"token": "abc-123",
	}

	resolved, err := catalog.ResolveVariables(vars)
	require.NoError(t, err)
	require.Len(t, resolved.Tracks, 1)
	assert.Equal(t, "live/demo", resolved.Tracks[0].Namespace)
	assert.Equal(t, "abc-123", resolved.Tracks[0].Token)
	// Original catalog should not be mutated.
	assert.Equal(t, "live/%ns%", catalog.Tracks[0].Namespace)
	assert.Equal(t, "%token%", catalog.Tracks[0].Token)
}

func TestCatalog_ResolveVariables_Error(t *testing.T) {
	catalog := Catalog{
		Version: 1,
		Tracks: []Track{{
			Namespace: "live/%missing%",
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(true),
		}},
	}

	_, err := catalog.ResolveVariables(map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve variables")
}

func TestIsValidVariableName(t *testing.T) {
	tests := map[string]struct {
		name string
		want bool
	}{
		"empty":        {"", false},
		"alphanumeric": {"abc123", true},
		"hyphen":       {"track-1", true},
		"underscore":   {"sub_name", true},
		"mixed case":   {"TrackName", true},
		"slash":        {"bad/name", false},
		"space":        {"bad name", false},
		"at sign":      {"bad@name", false},
		"percent":      {"bad%name", false},
		"unicode":      {"naïve", false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidVariableName(tt.name))
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
		"slash":         {"bad/value", false},
		"space":         {"bad value", false},
		"equals":        {"key=value", false},
		"percent":       {"bad%value", false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidVariableValue(tt.value))
		})
	}
}

// Ensure the no-percent fast path returns the original string unchanged when
// the catalog has nothing to resolve.
func TestCatalog_ResolveVariables_NoPercent(t *testing.T) {
	catalog := Catalog{
		Version: 1,
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
