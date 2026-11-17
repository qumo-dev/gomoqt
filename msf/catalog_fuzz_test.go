package msf

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fuzzCatalogSeeds are hand-written catalogs covering edge cases the spec
// examples do not.
var fuzzCatalogSeeds = []string{
	`{"version":"draft-02","tracks":[]}`,
	`{"version":"draft-02","tracks":[{"name":"v","namespace":null,"packaging":"loc","isLive":null}]}`,
	`{"version":"draft-02","tracks":[{"name":"café","packaging":"loc","isLive":true,"depends":["x",1]}]}`,
	`{"version":"draft-02","tracks":[{"name":"😀","packaging":"loc","isLive":true}]}`,
	`{"version":"draft-02","tracks":[{"name":"v","namespace":"a//b","packaging":"loc","isLive":true}]}`,
	`{"version":"draft-02","tracks":[{"name":"v","packaging":"loc","isLive":true,"packagingMetadata":"streamMapping=object;"}]}`,
	`{"version":"draft-02","tracks":[{"name":"v","packaging":"loc","isLive":true}],"tracks":[]}`,
	`{"version":"draft-02","publishTracks":[{"name":"6","namespace":"logs/r1","packaging":"moqlog","role":"log","isLive":true}],"tracks":[]}`,
	`{"version":"draft-02","initDataList":[{"id":"i","type":"track-property","data":"0x0D"}],"tracks":[{"name":"v","packaging":"loc","isLive":true,"initRef":"i"}]}`,
	`{"version":2}`,
	`[]`,
	"\xEF\xBB\xBF{}",
}

// fuzzDeltaSeeds are hand-written deltas covering edge cases the spec
// examples do not.
var fuzzDeltaSeeds = []string{
	`{"generatedAt":1,"deltaUpdate":[]}`,
	`{"generatedAt":null,"deltaUpdate":[{"op":"remove","tracks":[{"name":"v"}]}]}`,
	`{"generatedAt":1,"deltaUpdate":[{"op":"update","tracks":[{"parentName":"v","bitrate":1}]}],"deltaUpdate":[]}`,
	`{"generatedAt":1,"deltaUpdate":[{"op":"clone","tracks":[{"parentName":"v","parentNamespace":"a/b","name":"w"}]}]}`,
	`{"generatedAt":1,"deltaUpdate":[{"op":"add","tracks":[{"name":"w","packaging":"loc","isLive":true,"depends":["v"]}]}]}`,
	`{"generatedAt":1,"isComplete":true,"deltaUpdate":[]}`,
	`{"deltaUpdate":[{"op":"bogus","tracks":[]}]}`,
}

// assertCatalogRoundTrip checks, for data that parses as a catalog, that its
// encoding parses again, validates exactly as the original does, and
// re-encodes to the same bytes.
func assertCatalogRoundTrip(tb testing.TB, data []byte) {
	tb.Helper()
	catalog, err := ParseCatalog(data)
	if err != nil {
		return
	}
	encoded, err := json.Marshal(catalog)
	require.NoError(tb, err)
	again, err := ParseCatalog(encoded)
	require.NoError(tb, err, "the encoding of a parsed catalog must parse: %s", encoded)
	assert.Equal(tb, errorString(catalog.Validate()), errorString(again.Validate()), "validation must survive a round trip")
	reencoded, err := json.Marshal(again)
	require.NoError(tb, err)
	assert.Equal(tb, string(encoded), string(reencoded), "encoding must be stable")
}

// assertDeltaRoundTrip is assertCatalogRoundTrip for catalog deltas.
func assertDeltaRoundTrip(tb testing.TB, data []byte) {
	tb.Helper()
	delta, err := ParseCatalogDelta(data)
	if err != nil {
		return
	}
	encoded, err := json.Marshal(delta)
	require.NoError(tb, err)
	again, err := ParseCatalogDelta(encoded)
	require.NoError(tb, err, "the encoding of a parsed delta must parse: %s", encoded)
	assert.Equal(tb, errorString(delta.Validate()), errorString(again.Validate()), "validation must survive a round trip")
	reencoded, err := json.Marshal(again)
	require.NoError(tb, err)
	assert.Equal(tb, string(encoded), string(reencoded), "encoding must be stable")
}

// errorString returns err's message, or "" for nil.
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func FuzzParseCatalog(f *testing.F) {
	for _, data := range readSpecExamples(f) {
		if !isDeltaJSON(data) {
			f.Add(data)
		}
	}
	for _, seed := range fuzzCatalogSeeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		assertCatalogRoundTrip(t, data)
	})
}

func FuzzParseCatalogDelta(f *testing.F) {
	for _, data := range readSpecExamples(f) {
		if isDeltaJSON(data) {
			f.Add(data)
		}
	}
	for _, seed := range fuzzDeltaSeeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		assertDeltaRoundTrip(t, data)
	})
}

// FuzzCatalogApplyDelta checks the ApplyDelta property: a delta that applies
// without error always yields a catalog that validates, round-trips, and
// leaves the base catalog unchanged.
func FuzzCatalogApplyDelta(f *testing.F) {
	examples := readSpecExamples(f)
	bases := []string{
		string(examples["5.6.1-1.json"]),
		`{"version":"draft-02","tracks":[{"name":"video-1080","packaging":"loc","isLive":true,"renderGroup":1},` +
			`{"name":"video-1080","namespace":"example.com/custom","packaging":"loc","isLive":true},` +
			`{"name":"video","packaging":"loc","isLive":true},{"name":"slides","packaging":"loc","isLive":true}]}`,
	}
	deltas := []string{string(examples["5.6.4-1.json"]), string(examples["5.6.5-1.json"])}
	deltas = append(deltas, fuzzDeltaSeeds...)
	for _, base := range bases {
		for _, delta := range deltas {
			f.Add([]byte(base), []byte(delta))
		}
	}

	f.Fuzz(func(t *testing.T, baseData, deltaData []byte) {
		base, err := ParseCatalog(baseData)
		if err != nil {
			return
		}
		delta, err := ParseCatalogDelta(deltaData)
		if err != nil {
			return
		}
		assertApplyDeltaProperty(t, base, delta)
	})
}

// assertApplyDeltaProperty applies delta to base and checks that success
// implies a valid, round-tripping result and an unchanged base.
func assertApplyDeltaProperty(tb testing.TB, base Catalog, delta CatalogDelta) {
	tb.Helper()
	before, err := json.Marshal(base)
	require.NoError(tb, err)

	result, err := base.ApplyDelta(delta)

	after, marshalErr := json.Marshal(base)
	require.NoError(tb, marshalErr)
	assert.Equal(tb, string(before), string(after), "ApplyDelta must not change its receiver")
	if err != nil {
		return
	}
	require.NoError(tb, result.Validate(), "ApplyDelta returned a catalog that does not validate")
	encoded, err := json.Marshal(result)
	require.NoError(tb, err)
	again, err := ParseCatalog(encoded)
	require.NoError(tb, err)
	again.DefaultNamespace = result.DefaultNamespace
	assert.NoError(tb, again.Validate())
}
