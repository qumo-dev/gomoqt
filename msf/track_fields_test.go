package msf

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDependency_JSONRoundTrip(t *testing.T) {
	input := `["video",1,"audio"]`

	var deps []Dependency
	require.NoError(t, json.Unmarshal([]byte(input), &deps))
	assert.Equal(t, []Dependency{DependsOnTrack("video"), DependsOnRenderGroup(1), DependsOnTrack("audio")}, deps)
	assert.False(t, deps[0].IsRenderGroup())
	assert.True(t, deps[1].IsRenderGroup())

	data, err := json.Marshal(deps)
	require.NoError(t, err)
	assert.JSONEq(t, input, string(data))
}

func TestDependency_UnmarshalJSON_RejectsOtherTypes(t *testing.T) {
	tests := map[string]string{
		"boolean":  `true`,
		"fraction": `1.5`,
		"object":   `{"name":"video"}`,
		"null":     `null`,
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			var dep Dependency
			assert.Error(t, json.Unmarshal([]byte(input), &dep))
		})
	}
}

func TestDependency_String(t *testing.T) {
	assert.Equal(t, "video", DependsOnTrack("video").String())
	assert.Equal(t, "renderGroup 3", DependsOnRenderGroup(3).String())
}

func TestDependency_Validate(t *testing.T) {
	tests := map[string]struct {
		dep      Dependency
		errMatch string
	}{
		"track name":        {dep: DependsOnTrack("video")},
		"render group":      {dep: DependsOnRenderGroup(0)},
		"empty track name":  {dep: Dependency{}, errMatch: "empty track name"},
		"both set":          {dep: Dependency{TrackName: "video", RenderGroup: new(int64(1))}, errMatch: "not both"},
		"NUL in track name": {dep: DependsOnTrack("vi\x00deo"), errMatch: "NUL"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.dep.validate()
			if tt.errMatch == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMatch)
		})
	}
}

func TestTrack_PackagingMetadataValue(t *testing.T) {
	tests := map[string]struct {
		metadata string
		key      string
		want     string
		found    bool
	}{
		"single pair":          {metadata: "streamMapping=object", key: "streamMapping", want: "object", found: true},
		"second pair":          {metadata: "a=1;b=2", key: "b", want: "2", found: true},
		"first occurrence":     {metadata: "a=1;a=2", key: "a", want: "1", found: true},
		"keys are case-exact":  {metadata: "StreamMapping=object", key: "streamMapping"},
		"absent key":           {metadata: "a=1", key: "b"},
		"empty metadata":       {metadata: "", key: "a"},
		"empty value is found": {metadata: "a=", key: "a", want: "", found: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, found := Track{PackagingMetadata: tt.metadata}.PackagingMetadataValue(tt.key)
			assert.Equal(t, tt.found, found)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTrack_StreamMapping(t *testing.T) {
	tests := map[string]struct {
		metadata string
		want     StreamMapping
	}{
		"absent metadata defaults to group": {metadata: "", want: StreamMappingGroup},
		"absent key defaults to group":      {metadata: "other=1", want: StreamMappingGroup},
		"object":                            {metadata: "streamMapping=object", want: StreamMappingObject},
		"group":                             {metadata: "streamMapping=group", want: StreamMappingGroup},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, Track{PackagingMetadata: tt.metadata}.StreamMapping())
		})
	}
}

func TestValidatePackagingMetadata(t *testing.T) {
	tests := map[string]struct {
		metadata  string
		packaging Packaging
		errMatch  string
	}{
		"valid loc":                      {metadata: "streamMapping=object;x=1", packaging: PackagingLOC},
		"unknown key ignored":            {metadata: "com.example=v", packaging: PackagingLOC},
		"other packaging any value":      {metadata: "streamMapping=whatever", packaging: PackagingEventTimeline},
		"bad loc streamMapping":          {metadata: "streamMapping=frame", packaging: PackagingLOC, errMatch: "streamMapping"},
		"missing equals":                 {metadata: "streamMapping", packaging: PackagingLOC, errMatch: "key=value"},
		"empty key":                      {metadata: "=object", packaging: PackagingLOC, errMatch: "key=value"},
		"whitespace":                     {metadata: "a=b c", packaging: PackagingLOC, errMatch: "whitespace"},
		"second equals":                  {metadata: "a=b=c", packaging: PackagingLOC, errMatch: "'='"},
		"empty entry between semicolons": {metadata: "a=1;;b=2", packaging: PackagingLOC, errMatch: "key=value"},
		"empty metadata is no pairs":     {metadata: "", packaging: PackagingLOC},
		"trailing semicolon":             {metadata: "a=1;", packaging: PackagingLOC, errMatch: "key=value"},
		"leading semicolon":              {metadata: ";a=1", packaging: PackagingLOC, errMatch: "key=value"},
		"non-ASCII whitespace":           {metadata: "a=b c", packaging: PackagingLOC, errMatch: "whitespace"},
		"ideographic space in key":       {metadata: "a　=b", packaging: PackagingLOC, errMatch: "whitespace"},
		"empty value allowed":            {metadata: "a=", packaging: PackagingLOC},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := validatePackagingMetadata(tt.metadata, tt.packaging)
			if tt.errMatch == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMatch)
		})
	}
}

func TestInitDataRef_PropertyType(t *testing.T) {
	tests := map[string]struct {
		ref     InitDataRef
		want    uint64
		wantErr bool
	}{
		"track property with prefix":   {ref: InitDataRef{Type: InitDataTypeTrackProperty, Data: "0x0D"}, want: 0x0D},
		"object property no prefix":    {ref: InitDataRef{Type: InitDataTypeObjectProperty, Data: "0f"}, want: 0x0F},
		"inline has no property type":  {ref: InitDataRef{Type: InitDataTypeInline, Data: "AAAA"}, wantErr: true},
		"not hex":                      {ref: InitDataRef{Type: InitDataTypeTrackProperty, Data: "0xZZ"}, wantErr: true},
		"prefix only":                  {ref: InitDataRef{Type: InitDataTypeTrackProperty, Data: "0x"}, wantErr: true},
		"empty data for property type": {ref: InitDataRef{Type: InitDataTypeObjectProperty}, wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := tt.ref.PropertyType()
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
