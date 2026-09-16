package message

import (
	"errors"
	"io"
)

// Setup Parameter IDs defined by moq-lite-05.
const (
	SetupParamProbe uint64 = 0x1
	SetupParamPath  uint64 = 0x2
)

// Probe capability levels carried by the Probe Setup Parameter.
// Each level includes the ones below it.
const (
	ProbeLevelNone     uint64 = 0
	ProbeLevelReport   uint64 = 1
	ProbeLevelIncrease uint64 = 2
)

var ErrDuplicateSetupParameter = errors.New("duplicate setup parameter")

// SetupParameter is a single capability or extension advertisement
// within a SETUP message.
type SetupParameter struct {
	ID    uint64
	Value []byte
}

/*
 *	SETUP Message {
 *	  Message Length (i)
 *	  Parameter Count (i)
 *	  Setup Parameter (..) ...
 *	}
 *
 *	Setup Parameter {
 *	  Parameter ID (i)
 *	  Parameter Length (i)
 *	  Parameter Value (..)
 *	}
 */
type SetupMessage struct {
	Parameters []SetupParameter
}

// AddProbe appends a Probe parameter advertising the given capability level.
func (sm *SetupMessage) AddProbe(level uint64) {
	value, _ := WriteVarint(nil, level)
	sm.Parameters = append(sm.Parameters, SetupParameter{ID: SetupParamProbe, Value: value})
}

// AddPath appends a Path parameter carrying the request path.
func (sm *SetupMessage) AddPath(path string) {
	sm.Parameters = append(sm.Parameters, SetupParameter{ID: SetupParamPath, Value: []byte(path)})
}

// ProbeLevel returns the advertised probe capability level.
// It returns ProbeLevelNone if the parameter is absent or malformed,
// matching the spec's "absent means unsupported" semantics.
func (sm SetupMessage) ProbeLevel() uint64 {
	for _, p := range sm.Parameters {
		if p.ID != SetupParamProbe {
			continue
		}
		level, _, err := ReadVarint(p.Value)
		if err != nil {
			return ProbeLevelNone
		}
		return level
	}
	return ProbeLevelNone
}

// Path returns the Path parameter value and whether it was present.
func (sm SetupMessage) Path() (string, bool) {
	for _, p := range sm.Parameters {
		if p.ID == SetupParamPath {
			return string(p.Value), true
		}
	}
	return "", false
}

func (sm SetupMessage) Len() int {
	l := VarintLen(uint64(len(sm.Parameters)))
	for _, p := range sm.Parameters {
		l += VarintLen(p.ID) + BytesLen(p.Value)
	}
	return l
}

func (sm SetupMessage) Encode(w io.Writer) error {
	msgLen := sm.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, uint64(len(sm.Parameters)))
	for _, p := range sm.Parameters {
		b, _ = WriteVarint(b, p.ID)
		b, _ = WriteBytes(b, p.Value)
	}

	_, err := w.Write(b)

	return err
}

func (sm *SetupMessage) Decode(src io.Reader) error {
	size, err := ReadMessageLength(src)
	if err != nil {
		return err
	}

	if size > MaxMessageSize {
		return ErrMessageTooLarge
	}

	b := make([]byte, size)

	_, err = io.ReadFull(src, b)
	if err != nil {
		return err
	}

	count, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	b = b[n:]

	allocCap := count
	if count > uint64(len(b)) {
		allocCap = uint64(len(b))
	}
	params := make([]SetupParameter, 0, allocCap)
	seen := make(map[uint64]struct{}, allocCap)
	for range count {
		id, n, err := ReadVarint(b)
		if err != nil {
			return err
		}
		b = b[n:]

		if _, ok := seen[id]; ok {
			return ErrDuplicateSetupParameter
		}
		seen[id] = struct{}{}

		value, n, err := ReadBytes(b)
		if err != nil {
			return err
		}
		b = b[n:]

		// Copy the value out of the shared decode buffer.
		params = append(params, SetupParameter{ID: id, Value: append([]byte(nil), value...)})
	}
	sm.Parameters = params

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
