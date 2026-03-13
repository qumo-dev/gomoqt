package moqt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSupportedNativeMOQALPNs(t *testing.T) {
	assert.Equal(t, []string{NextProtoMOQ}, SupportedNativeMOQALPNs)
}

func TestDefaultServerNextProtos(t *testing.T) {
	assert.Equal(t, []string{NextProtoH3, NextProtoMOQ}, DefaultServerNextProtos)
}
