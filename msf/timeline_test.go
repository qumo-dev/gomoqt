package msf

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocationJSON(t *testing.T) {
	location := Location{GroupID: 12, ObjectID: 3}

	data, err := json.Marshal(location)
	require.NoError(t, err)
	assert.JSONEq(t, `[12,3]`, string(data))

	var decoded Location
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, location, decoded)
}

func TestLocationJSON_InvalidEntryLength(t *testing.T) {
	tests := []string{
		`[1]`,
		`[1,2,3]`,
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			var location Location
			err := json.Unmarshal([]byte(input), &location)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "exactly 2 items")
		})
	}
}

func TestMediaTimelineEntryJSON(t *testing.T) {
	entry := MediaTimelineEntry{
		MediaTime: 2002,
		Location:  Location{GroupID: 1, ObjectID: 0},
		Wallclock: 1759924160383,
	}

	data, err := json.Marshal(entry)
	require.NoError(t, err)
	assert.JSONEq(t, `[2002,[1,0],1759924160383]`, string(data))

	var decoded MediaTimelineEntry
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, entry, decoded)
}

func TestMediaTimelineEntryJSON_InvalidEntryLength(t *testing.T) {
	var entry MediaTimelineEntry

	err := json.Unmarshal([]byte(`[1,[2,3]]`), &entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly 3 items")
}

func TestEventTimelineRecordJSONAndValidation(t *testing.T) {
	wallclock := int64(1756885678361)
	record := EventTimelineRecord{
		Wallclock: &wallclock,
		Data:      json.RawMessage(`{"status":"in_progress"}`),
	}

	require.NoError(t, record.Validate())
	data, err := json.Marshal(record)
	require.NoError(t, err)
	assert.JSONEq(t, `{"t":1756885678361,"data":{"status":"in_progress"}}`, string(data))

	var decoded EventTimelineRecord
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NoError(t, decoded.Validate())
	require.NotNil(t, decoded.Wallclock)
	assert.Equal(t, wallclock, *decoded.Wallclock)
	assert.JSONEq(t, `{"status":"in_progress"}`, string(decoded.Data))
}

func TestEventTimelineRecordValidate_RejectsMultipleIndexes(t *testing.T) {
	wallclock := int64(1)
	mediaTime := int64(2)
	record := EventTimelineRecord{
		Wallclock: &wallclock,
		MediaTime: &mediaTime,
		Data:      json.RawMessage(`{}`),
	}

	err := record.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one of t, l, or m")
}

func TestEventTimelineRecordValidate_Errors(t *testing.T) {
	tests := map[string]struct {
		record       EventTimelineRecord
		errorMessage string
	}{
		"missing selector": {
			record:       EventTimelineRecord{Data: json.RawMessage(`{}`)},
			errorMessage: "exactly one of t, l, or m",
		},
		"missing data": {
			record:       EventTimelineRecord{Wallclock: new(int64(1))},
			errorMessage: "must contain data",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.record.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMessage)
		})
	}
}

func TestEventTimelineRecordJSON_PreservesExtraFields(t *testing.T) {
	record := EventTimelineRecord{
		Wallclock:   new(int64(1756885678361)),
		Data:        json.RawMessage(`{"status":"ready"}`),
		ExtraFields: map[string]json.RawMessage{"ext": json.RawMessage(`42`)},
	}

	data, err := json.Marshal(record)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"ext":42`)

	var decoded EventTimelineRecord
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Contains(t, decoded.ExtraFields, "ext")
	assert.Equal(t, json.RawMessage(`42`), decoded.ExtraFields["ext"])
}
