package msf

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitNamespace(t *testing.T) {
	tests := map[string]struct {
		namespace string
		want      []string
		errMatch  string
	}{
		"spec example":        {namespace: "example.com/live/channel1", want: []string{"example.com", "live", "channel1"}},
		"single field":        {namespace: "live", want: []string{"live"}},
		"non-ASCII is kept":   {namespace: "customer/café", want: []string{"customer", "café"}},
		"32 fields":           {namespace: strings.Repeat("a/", 31) + "a", want: strings.Split(strings.Repeat("a/", 31)+"a", "/")},
		"empty":               {namespace: "", errMatch: "must not be empty"},
		"leading slash":       {namespace: "/live", errMatch: "empty field"},
		"trailing slash":      {namespace: "live/", errMatch: "empty field"},
		"double slash":        {namespace: "live//demo", errMatch: "empty field"},
		"lone slash":          {namespace: "/", errMatch: "empty field"},
		"33 fields":           {namespace: strings.Repeat("a/", 32) + "a", errMatch: "at most 32"},
		"NUL character":       {namespace: "live/de\x00mo", errMatch: "NUL"},
		"invalid UTF-8":       {namespace: "live/\xff", errMatch: "not valid UTF-8"},
		"encoded surrogate":   {namespace: "live/\xed\xba\xad", errMatch: "not valid UTF-8"},
		"case is significant": {namespace: "Live/Demo", want: []string{"Live", "Demo"}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := SplitNamespace(tt.namespace)
			if tt.errMatch != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMatch)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseCatalog_RejectsInvalidText(t *testing.T) {
	tests := map[string]struct {
		input    string
		errMatch string
	}{
		"byte order mark": {
			input:    "\xEF\xBB\xBF" + `{"version":"draft-02","tracks":[]}`,
			errMatch: "byte order mark",
		},
		"invalid UTF-8": {
			input:    `{"version":"draft-02","tracks":[{"name":"vid` + "\xff" + `eo"}]}`,
			errMatch: "not valid UTF-8",
		},
		"lone low surrogate": {
			input:    `{"version":"draft-02","tracks":[{"name":"\uDEAD"}]}`,
			errMatch: "unpaired surrogate",
		},
		"lone high surrogate": {
			input:    `{"version":"draft-02","tracks":[{"name":"\uD83Dx"}]}`,
			errMatch: "unpaired surrogate",
		},
		"high surrogate followed by non-surrogate escape": {
			input:    `{"version":"draft-02","tracks":[{"name":"\uD83DA"}]}`,
			errMatch: "unpaired surrogate",
		},
		"surrogate outside naming fields": {
			input:    `{"version":"draft-02","tracks":[],"com.example":"\uDC00"}`,
			errMatch: "unpaired surrogate",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseCatalogString(tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMatch)
		})
	}
}

func TestParseCatalog_AcceptsValidEscapes(t *testing.T) {
	tests := map[string]struct {
		input string
		name  string
	}{
		"surrogate pair":                {input: `{"version":"draft-02","tracks":[{"name":"😀"}]}`, name: "\U0001F600"},
		"escaped backslash before u":    {input: `{"version":"draft-02","tracks":[{"name":"a\\uDEAD"}]}`, name: `a\uDEAD`},
		"escaped solidus":               {input: `{"version":"draft-02","tracks":[{"name":"a\/b"}]}`, name: "a/b"},
		"escaped precomposed character": {input: `{"version":"draft-02","tracks":[{"name":"café"}]}`, name: "café"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			catalog, err := ParseCatalogString(tt.input)
			require.NoError(t, err)
			require.Len(t, catalog.Tracks, 1)
			assert.Equal(t, tt.name, catalog.Tracks[0].Name)
		})
	}
}

func TestParseCatalogDelta_RejectsInvalidText(t *testing.T) {
	_, err := ParseCatalogDeltaString("\xEF\xBB\xBF" + `{"generatedAt":1,"deltaUpdate":[]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "byte order mark")
}

// TestCatalogValidate_NamesCompareByMappedBytes checks the §11.2 examples:
// escaped and literal forms of the same scalar values are the same track,
// while decomposed and differently cased forms are different tracks.
func TestCatalogValidate_NamesCompareByMappedBytes(t *testing.T) {
	tests := map[string]struct {
		second    string
		duplicate bool
	}{
		"escaped precomposed equals literal": {second: `café`, duplicate: true},
		"decomposed differs":                 {second: `café`, duplicate: false},
		"case differs":                       {second: `Café`, duplicate: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			catalog, err := ParseCatalogString(`{"version":"draft-02","tracks":[` +
				`{"name":"café","packaging":"loc","isLive":true},` +
				`{"name":"` + tt.second + `","packaging":"loc","isLive":true}]}`)
			require.NoError(t, err)

			err = catalog.Validate()
			if !tt.duplicate {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "duplicate track identity")
		})
	}
}
