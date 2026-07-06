package message_test

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input message.SetupMessage
	}{
		"empty parameter list": {
			input: message.SetupMessage{},
		},
		"probe parameter": {
			input: func() message.SetupMessage {
				var sm message.SetupMessage
				sm.AddProbe(message.ProbeLevelReport)
				return sm
			}(),
		},
		"path parameter": {
			input: func() message.SetupMessage {
				var sm message.SetupMessage
				sm.AddPath("/relay/live")
				return sm
			}(),
		},
		"probe and path parameters": {
			input: func() message.SetupMessage {
				var sm message.SetupMessage
				sm.AddProbe(message.ProbeLevelIncrease)
				sm.AddPath("/")
				return sm
			}(),
		},
		"unknown parameter is preserved": {
			input: message.SetupMessage{
				Parameters: []message.SetupParameter{
					{ID: 0x7f, Value: []byte{0x01, 0x02}},
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer

			require.NoError(t, tt.input.Encode(&buf))

			var decoded message.SetupMessage
			require.NoError(t, decoded.Decode(&buf))

			assert.Equal(t, tt.input.ProbeLevel(), decoded.ProbeLevel())
			gotPath, gotOk := decoded.Path()
			wantPath, wantOk := tt.input.Path()
			assert.Equal(t, wantOk, gotOk)
			assert.Equal(t, wantPath, gotPath)
			assert.Len(t, decoded.Parameters, len(tt.input.Parameters))
		})
	}
}

func TestSetupMessage_ProbeLevel(t *testing.T) {
	tests := map[string]struct {
		input    message.SetupMessage
		expected uint64
	}{
		"absent means none": {
			input:    message.SetupMessage{},
			expected: message.ProbeLevelNone,
		},
		"report": {
			input: func() message.SetupMessage {
				var sm message.SetupMessage
				sm.AddProbe(message.ProbeLevelReport)
				return sm
			}(),
			expected: message.ProbeLevelReport,
		},
		"increase": {
			input: func() message.SetupMessage {
				var sm message.SetupMessage
				sm.AddProbe(message.ProbeLevelIncrease)
				return sm
			}(),
			expected: message.ProbeLevelIncrease,
		},
		"malformed value means none": {
			input: message.SetupMessage{
				Parameters: []message.SetupParameter{
					{ID: message.SetupParamProbe, Value: nil},
				},
			},
			expected: message.ProbeLevelNone,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.input.ProbeLevel())
		})
	}
}

func TestSetupMessage_Decode_DuplicateParameter(t *testing.T) {
	var sm message.SetupMessage
	sm.AddProbe(message.ProbeLevelReport)
	sm.AddProbe(message.ProbeLevelIncrease)

	var buf bytes.Buffer
	require.NoError(t, sm.Encode(&buf))

	var decoded message.SetupMessage
	err := decoded.Decode(&buf)
	assert.ErrorIs(t, err, message.ErrDuplicateSetupParameter)
}

func TestSetupMessage_DecodeErrors(t *testing.T) {
	tests := map[string]struct {
		data []byte
	}{
		"empty input":         {data: []byte{}},
		"truncated parameter": {data: []byte{0x03, 0x01, 0x01, 0x05}}, // claims 5-byte value, none follow
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var decoded message.SetupMessage
			err := decoded.Decode(bytes.NewReader(tt.data))
			assert.Error(t, err)
		})
	}
}
