package msf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// specExampleDir holds the catalog examples of draft-ietf-moq-msf-02 §5.6,
// extracted verbatim from the draft text, one JSON object per file named
// after its section (5.6.14 has the template and its resolved form).
const specExampleDir = "testdata/draft02"

// readSpecExamples returns the spec example files keyed by file name.
func readSpecExamples(tb testing.TB) map[string][]byte {
	tb.Helper()
	paths, err := filepath.Glob(filepath.Join(specExampleDir, "*.json"))
	require.NoError(tb, err)
	require.NotEmpty(tb, paths)
	examples := make(map[string][]byte, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		require.NoError(tb, err)
		examples[filepath.Base(path)] = data
	}
	return examples
}

// isDeltaJSON reports whether data is a delta update rather than an
// independent catalog.
func isDeltaJSON(data []byte) bool {
	return bytes.Contains(data, []byte(`"deltaUpdate"`))
}

// TestSpecExamples_ParseAndValidate runs every catalog example of the draft
// through the parser and Validate. All 18 examples of draft-02 validate as
// written; an example the draft gets wrong would be recorded here with the
// rule it breaks rather than relaxing Validate.
func TestSpecExamples_ParseAndValidate(t *testing.T) {
	examples := readSpecExamples(t)
	assert.Len(t, examples, 18, "the draft has 17 catalog examples; 5.6.14 has two documents")

	for name, data := range examples {
		t.Run(name, func(t *testing.T) {
			if isDeltaJSON(data) {
				delta, err := ParseCatalogDelta(data)
				require.NoError(t, err)
				assert.NoError(t, delta.Validate())
				return
			}
			catalog, err := ParseCatalog(data)
			require.NoError(t, err)
			assert.NoError(t, catalog.Validate())
		})
	}
}

// TestSpecExamples_VariableSubstitution resolves the template of §5.6.14 with
// the fragment of its request URI and compares it with the resolved catalog
// the draft gives.
func TestSpecExamples_VariableSubstitution(t *testing.T) {
	examples := readSpecExamples(t)
	template, err := ParseCatalog(examples["5.6.14-1.json"])
	require.NoError(t, err)
	want, err := ParseCatalog(examples["5.6.14-2.json"])
	require.NoError(t, err)

	vars := ParseFragmentVariables("msf:namespace--catalog&token=1234&id=c3e5s7w&event=xyz")
	got, err := template.ResolveVariables(vars)
	require.NoError(t, err)

	gotJSON, err := got.MarshalJSON()
	require.NoError(t, err)
	wantJSON, err := want.MarshalJSON()
	require.NoError(t, err)
	assert.JSONEq(t, string(wantJSON), string(gotJSON))
}

// TestSpecExamples_RoundTrip checks that every example re-encodes to a
// document that parses and validates to the same encoding.
func TestSpecExamples_RoundTrip(t *testing.T) {
	for name, data := range readSpecExamples(t) {
		t.Run(name, func(t *testing.T) {
			if isDeltaJSON(data) {
				assertDeltaRoundTrip(t, data)
				return
			}
			assertCatalogRoundTrip(t, data)
		})
	}
}
