package moqt

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/quic-go/quic-go/quicvarint"
)

// ExtensionKey represents the type identifier for MOQ protocol parameters.
type ExtensionKey uint64

type parameters map[uint64][]byte

// NewExtension creates a new empty parameters instance.
func NewExtension() *Extension {
	return &Extension{
		parameters: make(parameters),
	}
}

// Extension holds key-value pairs for MOQ protocol negotiation.
// Extension are used during session setup and other protocol operations
// to exchange configuration options between client and server.
type Extension struct {
	parameters parameters
}

// Clone creates a deep copy of the parameters.
func (p *Extension) Clone() *Extension {
	return &Extension{
		parameters: maps.Clone(p.parameters),
	}
}

// String returns a string representation of the parameters for debugging.
func (p Extension) String() string {
	var sb strings.Builder
	sb.WriteString("{")
	for key, value := range p.parameters {
		sb.WriteString(" ")
		sb.WriteString(fmt.Sprintf("%d", key))
		sb.WriteString(": ")
		sb.WriteString(fmt.Sprintf("%v", value))
		sb.WriteString(",")
	}
	sb.WriteString(" }")
	return sb.String()
}

// SetByteArray sets a parameter with a byte array value.
func (p *Extension) SetByteArray(key ExtensionKey, value []byte) {
	if p.parameters == nil {
		p.parameters = make(parameters)
	}
	p.parameters[uint64(key)] = value
}

// SetString sets a parameter with a string value.
func (p *Extension) SetString(key ExtensionKey, value string) {
	if p.parameters == nil {
		p.parameters = make(parameters)
	}
	p.parameters[uint64(key)] = []byte(value)
}

// SetUint sets a parameter with an unsigned integer value encoded as a varint.
func (p *Extension) SetUint(key ExtensionKey, value uint64) {
	if p.parameters == nil {
		p.parameters = make(parameters)
	}
	p.parameters[uint64(key)] = quicvarint.Append(make([]byte, 0), value)
}

// SetBool sets a parameter with a boolean value (1 for true, 0 for false).
func (p *Extension) SetBool(key ExtensionKey, value bool) {
	if p.parameters == nil {
		p.parameters = make(parameters)
	}
	if value {
		p.parameters[uint64(key)] = quicvarint.Append(make([]byte, 0), 1)
	} else {
		p.parameters[uint64(key)] = quicvarint.Append(make([]byte, 0), 0)
	}
}

// Remove removes a parameter by key.
func (p *Extension) Remove(key ExtensionKey) {
	if p.parameters == nil {
		return
	}
	delete(p.parameters, uint64(key))
}

// GetByteArray retrieves a parameter value as a byte array.
// Returns ErrParameterNotFound if the parameter does not exist.
func (p Extension) GetByteArray(key ExtensionKey) ([]byte, error) {
	if p.parameters == nil {
		return nil, ErrParameterNotFound
	}
	value, ok := p.parameters[uint64(key)]
	if !ok {
		return nil, ErrParameterNotFound
	}

	return value, nil
}

// GetString retrieves a parameter value as a string.
// Returns ErrParameterNotFound if the parameter does not exist.
func (p Extension) GetString(key ExtensionKey) (string, error) {
	if p.parameters == nil {
		return "", ErrParameterNotFound
	}

	value, err := p.GetByteArray(key)
	if err != nil {
		return "", err
	}

	return string(value), nil
}

// GetUint retrieves a parameter value as an unsigned integer decoded from a varint.
// Returns ErrParameterNotFound if the parameter does not exist.
func (p Extension) GetUint(key ExtensionKey) (uint64, error) {
	if p.parameters == nil {
		return 0, ErrParameterNotFound
	}

	value, ok := p.parameters[uint64(key)]
	if !ok {
		return 0, ErrParameterNotFound
	}

	num, err := quicvarint.Read(quicvarint.NewReader(bytes.NewReader(value)))
	if err != nil {
		return 0, err
	}

	return num, nil
}

// GetBool retrieves a parameter value as a boolean (1=true, 0=false).
// Returns ErrParameterNotFound if the parameter does not exist.
func (p Extension) GetBool(key ExtensionKey) (bool, error) {
	if p.parameters == nil {
		return false, ErrParameterNotFound
	}

	num, err := p.GetUint(key)
	if err != nil {
		return false, err
	}

	switch num {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, errors.New("invalid value as bool")
	}
}

// ErrParameterNotFound is returned when a parameter requested does not exist
// within the Extension map.
var ErrParameterNotFound = errors.New("parameter not found")

const (
	// param_type_path is the ExtensionKey used to pass the requested
	// QUAL broadcast path when creating or negotiating a session.
	param_type_path ExtensionKey = 0x01
	// param_type_delivery_timeout   ParameterType = 0x03
	// param_type_new_session_uri ParameterType = 0x04

	// max_subscribe_id   ParameterType = 0x02
)
