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

func TestEventTimelineRecord_LocationSelector(t *testing.T) {
	loc := Location{GroupID: 5, ObjectID: 2}
	record := EventTimelineRecord{
		Location: &loc,
		Data:     json.RawMessage(`{"scene":"forest"}`),
	}

	require.NoError(t, record.Validate())

	data, err := json.Marshal(record)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"l":[5,2]`)
	assert.Contains(t, string(data), `"data":`)

	var decoded EventTimelineRecord
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NoError(t, decoded.Validate())
	require.NotNil(t, decoded.Location)
	assert.Equal(t, uint64(5), decoded.Location.GroupID)
	assert.Equal(t, uint64(2), decoded.Location.ObjectID)
	assert.Nil(t, decoded.Wallclock)
	assert.Nil(t, decoded.MediaTime)
}

func TestEventTimelineRecord_MediaTimeSelector(t *testing.T) {
	mediaTime := int64(90000)
	record := EventTimelineRecord{
		MediaTime: &mediaTime,
		Data:      json.RawMessage(`{"type":"keyframe"}`),
	}

	require.NoError(t, record.Validate())

	data, err := json.Marshal(record)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"m":90000`)
	assert.Contains(t, string(data), `"data":`)

	var decoded EventTimelineRecord
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NoError(t, decoded.Validate())
	require.NotNil(t, decoded.MediaTime)
	assert.Equal(t, int64(90000), *decoded.MediaTime)
	assert.Nil(t, decoded.Wallclock)
	assert.Nil(t, decoded.Location)
}

func TestEventTimelineRecord_AllThreeSelectorsInvalid(t *testing.T) {
	record := EventTimelineRecord{
		Wallclock: new(int64(1)),
		Location:  &Location{GroupID: 1, ObjectID: 0},
		MediaTime: new(int64(2)),
		Data:      json.RawMessage(`{}`),
	}

	err := record.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one of t, l, or m")
}

func TestLocationJSON_InvalidTypes(t *testing.T) {
	var loc Location
	err := json.Unmarshal([]byte(`"not an array"`), &loc)
	require.Error(t, err)
}

func TestMediaTimelineEntryJSON_InvalidLocation(t *testing.T) {
	var entry MediaTimelineEntry
	err := json.Unmarshal([]byte(`[1,"not a location",2]`), &entry)
	require.Error(t, err)
}

func TestMediaTimelineEntryJSON_InvalidTypes(t *testing.T) {
	var entry MediaTimelineEntry
	err := json.Unmarshal([]byte(`"not an array"`), &entry)
	require.Error(t, err)
}

func TestEventTimelineRecordJSON_InvalidTypes(t *testing.T) {
	var record EventTimelineRecord
	err := json.Unmarshal([]byte(`"not an object"`), &record)
	require.Error(t, err)
}

// --- UnmarshalJSON error paths ---

func TestEventTimelineRecord_UnmarshalJSON_FieldErrors(t *testing.T) {
	tests := map[string]struct {
		input string
	}{
		"bad t": {`{"t": "not-a-number", "data": {}}`},
		"bad l": {`{"l": "not-an-array", "data": {}}`},
		"bad m": {`{"m": "not-a-number", "data": {}}`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var record EventTimelineRecord
			err := json.Unmarshal([]byte(tt.input), &record)
			require.Error(t, err)
		})
	}
}

func TestLocation_UnmarshalJSON_InvalidElementTypes(t *testing.T) {
	tests := map[string]struct {
		input string
	}{
		"first element not number":  {`["not-uint", 3]`},
		"second element not number": {`[5, "not-uint"]`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var loc Location
			err := json.Unmarshal([]byte(tt.input), &loc)
			require.Error(t, err)
		})
	}
}

func TestMediaTimelineEntry_UnmarshalJSON_FieldErrors(t *testing.T) {
	tests := map[string]struct {
		input string
	}{
		"bad mediaTime": {`["not-a-number", [1,0], 12345]`},
		"bad location":  {`[2002, "not-an-array", 12345]`},
		"bad wallclock": {`[2002, [1,0], "not-a-number"]`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var entry MediaTimelineEntry
			err := json.Unmarshal([]byte(tt.input), &entry)
			require.Error(t, err)
		})
	}
}

func TestEventTimelineRecord_MarshalJSON(t *testing.T) {
	tests := map[string]struct {
		record EventTimelineRecord
		want   string
	}{
		"wallclock": {
			record: EventTimelineRecord{
				Wallclock: func() *int64 { v := int64(100); return &v }(),
				Data:      json.RawMessage(`{"a":1}`),
			},
			want: `{"t":100,"data":{"a":1}}`,
		},
		"location": {
			record: EventTimelineRecord{
				Location: &Location{GroupID: 1, ObjectID: 2},
				Data:     json.RawMessage(`{"a":1}`),
			},
			want: `{"l":[1,2],"data":{"a":1}}`,
		},
		"media_time": {
			record: EventTimelineRecord{
				MediaTime: func() *int64 { v := int64(200); return &v }(),
				Data:      json.RawMessage(`{"a":1}`),
			},
			want: `{"m":200,"data":{"a":1}}`,
		},
		"extra_fields": {
			record: EventTimelineRecord{
				Wallclock:   func() *int64 { v := int64(100); return &v }(),
				Data:        json.RawMessage(`{"a":1}`),
				ExtraFields: map[string]json.RawMessage{"ext": json.RawMessage(`42`)},
			},
			want: `{"t":100,"data":{"a":1},"ext":42}`,
		},
		"no_data": {
			record: EventTimelineRecord{
				Wallclock: func() *int64 { v := int64(100); return &v }(),
			},
			want: `{"t":100}`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b, err := tt.record.MarshalJSON()
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(b))
		})
	}
}
