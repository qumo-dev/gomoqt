package moqt

import (
	"testing"

	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
)

func TestStandardErrors(t *testing.T) {
	tests := map[string]struct {
		err    error
		expect string
	}{
		"invalid scheme": {err: ErrInvalidScheme, expect: "moqt: invalid scheme"},
		"closed session": {err: ErrClosedSession, expect: "moqt: closed session"},
		"server closed":  {err: ErrServerClosed, expect: "moqt: server closed"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expect, tt.err.Error())
		})
	}
}

// TestStreamErrorCodes_WireValues pins every stream code to moq-lite-06
// Table 3, or to gomoqt's application range (64+) where moq-lite has none.
// Nothing may be sent from the reserved range 32–47.
func TestStreamErrorCodes_WireValues(t *testing.T) {
	tests := map[string]struct {
		code uint32
		want uint32
	}{
		"announce internal":       {code: uint32(AnnounceErrorCodeInternal), want: 0x0},
		"announce uninterested":   {code: uint32(UninterestedErrorCode), want: 0x1},
		"announce unauthorized":   {code: uint32(AnnounceErrorCodeUnauthorized), want: 0x40},
		"announce invalid prefix": {code: uint32(AnnounceErrorCodeInvalidPrefix), want: 0x43},
		"announce banned prefix":  {code: uint32(BannedPrefixErrorCode), want: 0x44},
		"subscribe internal":      {code: uint32(SubscribeErrorCodeInternal), want: 0x0},
		"subscribe canceled":      {code: uint32(SubscribeErrorCodeCanceled), want: 0x1},
		"subscribe not found":     {code: uint32(SubscribeErrorCodeNotFound), want: 0x33},
		"subscribe unroutable":    {code: uint32(SubscribeErrorCodeUnroutable), want: 0x36},
		"subscribe timeout":       {code: uint32(SubscribeErrorCodeTimeout), want: 0x31},
		"subscribe unauthorized":  {code: uint32(SubscribeErrorCodeUnauthorized), want: 0x40},
		"subscribe invalid range": {code: uint32(SubscribeErrorCodeInvalidRange), want: 0x41},
		"subscribe duplicate id":  {code: uint32(SubscribeErrorCodeDuplicateID), want: 0x42},
		"fetch internal":          {code: uint32(FetchErrorCodeInternal), want: 0x0},
		"fetch timeout":           {code: uint32(FetchErrorCodeTimeout), want: 0x31},
		"probe internal":          {code: uint32(ProbeErrorCodeInternal), want: 0x0},
		"probe timeout":           {code: uint32(ProbeErrorCodeTimeout), want: 0x31},
		"probe not supported":     {code: uint32(ProbeErrorCodeNotSupported), want: 0x45},
		"group internal":          {code: uint32(InternalGroupErrorCode), want: 0x0},
		"group cancelled":         {code: uint32(SubscribeCanceledErrorCode), want: 0x1},
		"group expired":           {code: uint32(ExpiredGroupErrorCode), want: 0x2},
		"group session closed":    {code: uint32(ClosedSessionGroupErrorCode), want: 0x3},
		"group too large":         {code: uint32(GroupTooLargeErrorCode), want: 0x32},
		"group not found":         {code: uint32(NotFoundGroupErrorCode), want: 0x33},
		"group old":               {code: uint32(OldGroupErrorCode), want: 0x34},
		"group evicted":           {code: uint32(EvictedGroupErrorCode), want: 0x35},
		"group frame too large":   {code: uint32(FrameTooLargeErrorCode), want: 0x38},
		"group invalid range":     {code: uint32(InvalidRangeGroupErrorCode), want: 0x41},
		"group unauthorized":      {code: uint32(UnauthorizedGroupErrorCode), want: 0x40},
		"auth stream cancelled":   {code: uint32(authStreamCancelled), want: 0x1},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.code)
			assert.False(t, tt.code >= 32 && tt.code <= 47, "the reserved range is never sent")
		})
	}
}

// TestSessionErrorCodes_WireValues pins every session code to moq-lite-06
// Table 2, or to the application range (64+).
func TestSessionErrorCodes_WireValues(t *testing.T) {
	tests := map[string]struct {
		code SessionErrorCode
		want uint32
	}{
		"no error":                   {code: NoError, want: 0x0},
		"internal":                   {code: InternalSessionErrorCode, want: 0x1},
		"unauthorized":               {code: UnauthorizedSessionErrorCode, want: 0x2},
		"protocol violation":         {code: ProtocolViolationErrorCode, want: 0x3},
		"key-value formatting":       {code: KeyValueFormattingErrorCode, want: 0x6},
		"goaway timeout":             {code: GoAwayTimeoutErrorCode, want: 0x10},
		"control message timeout":    {code: ControlMessageTimeoutErrorCode, want: 0x11},
		"version negotiation failed": {code: VersionNegotiationFailedErrorCode, want: 0x15},
		"not supported (auth)":       {code: NotSupportedErrorCode, want: 0x40},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, uint32(tt.code))
		})
	}
}

func TestAnnounceErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   AnnounceErrorCode
		expect string
	}{
		"internal":       {code: AnnounceErrorCodeInternal, expect: "moqt: internal error"},
		"uninterested":   {code: UninterestedErrorCode, expect: "moqt: uninterested"},
		"banned prefix":  {code: BannedPrefixErrorCode, expect: "moqt: banned prefix"},
		"invalid prefix": {code: AnnounceErrorCodeInvalidPrefix, expect: "moqt: invalid prefix"},
		"unauthorized":   {code: AnnounceErrorCodeUnauthorized, expect: "moqt: unauthorized"},
		"unknown":        {code: AnnounceErrorCode(0x99), expect: ""},
		"reserved range": {code: AnnounceErrorCode(0x20), expect: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expect, tt.code.String())
		})
	}
}

func TestSubscribeErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   SubscribeErrorCode
		expect string
	}{
		"internal":      {code: SubscribeErrorCodeInternal, expect: "moqt: internal error"},
		"canceled":      {code: SubscribeErrorCodeCanceled, expect: "moqt: cancelled"},
		"invalid range": {code: SubscribeErrorCodeInvalidRange, expect: "moqt: invalid range"},
		"duplicate id":  {code: SubscribeErrorCodeDuplicateID, expect: "moqt: duplicated id"},
		"not found":     {code: SubscribeErrorCodeNotFound, expect: "moqt: track does not exist"},
		"unauthorized":  {code: SubscribeErrorCodeUnauthorized, expect: "moqt: unauthorized"},
		"timeout":       {code: SubscribeErrorCodeTimeout, expect: "moqt: timeout"},
		"unroutable":    {code: SubscribeErrorCodeUnroutable, expect: "moqt: unroutable"},
		"no capacity":   {code: SubscribeErrorCode(0x30), expect: "moqt: no capacity"},
		"unknown":       {code: SubscribeErrorCode(0x99), expect: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expect, tt.code.String())
		})
	}
}

func TestSessionErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   SessionErrorCode
		expect string
	}{
		"no error":                   {code: NoError, expect: "moqt: no error"},
		"internal":                   {code: InternalSessionErrorCode, expect: "moqt: internal error"},
		"unauthorized":               {code: UnauthorizedSessionErrorCode, expect: "moqt: unauthorized"},
		"protocol violation":         {code: ProtocolViolationErrorCode, expect: "moqt: protocol violation"},
		"key-value formatting":       {code: KeyValueFormattingErrorCode, expect: "moqt: key-value formatting error"},
		"goaway timeout":             {code: GoAwayTimeoutErrorCode, expect: "moqt: goaway timeout"},
		"control message timeout":    {code: ControlMessageTimeoutErrorCode, expect: "moqt: control message timeout"},
		"version negotiation failed": {code: VersionNegotiationFailedErrorCode, expect: "moqt: version negotiation failed"},
		"not supported":              {code: NotSupportedErrorCode, expect: "moqt: not supported"},
		"unknown":                    {code: SessionErrorCode(0x99), expect: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expect, tt.code.String())
		})
	}
}

func TestGroupErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   GroupErrorCode
		expect string
	}{
		"internal":           {code: InternalGroupErrorCode, expect: "moqt: internal error"},
		"subscribe canceled": {code: SubscribeCanceledErrorCode, expect: "moqt: subscribe canceled"},
		"expired":            {code: ExpiredGroupErrorCode, expect: "moqt: group expires"},
		"session closed":     {code: ClosedSessionGroupErrorCode, expect: "moqt: session closed"},
		"too large":          {code: GroupTooLargeErrorCode, expect: "moqt: group too large"},
		"not found":          {code: NotFoundGroupErrorCode, expect: "moqt: not found"},
		"old":                {code: OldGroupErrorCode, expect: "moqt: old group"},
		"evicted":            {code: EvictedGroupErrorCode, expect: "moqt: evicted"},
		"frame too large":    {code: FrameTooLargeErrorCode, expect: "moqt: frame too large"},
		"invalid range":      {code: InvalidRangeGroupErrorCode, expect: "moqt: invalid range"},
		"unauthorized":       {code: UnauthorizedGroupErrorCode, expect: "moqt: unauthorized"},
		"unknown":            {code: GroupErrorCode(0x99), expect: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expect, tt.code.String())
		})
	}
}

func TestFetchErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   FetchErrorCode
		expect string
	}{
		"internal": {code: FetchErrorCodeInternal, expect: "moqt: internal error"},
		"timeout":  {code: FetchErrorCodeTimeout, expect: "moqt: timeout"},
		"unknown":  {code: FetchErrorCode(0x99), expect: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expect, tt.code.String())
		})
	}
}

func TestProbeErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   ProbeErrorCode
		expect string
	}{
		"internal":      {code: ProbeErrorCodeInternal, expect: "moqt: internal error"},
		"timeout":       {code: ProbeErrorCodeTimeout, expect: "moqt: timeout"},
		"not supported": {code: ProbeErrorCodeNotSupported, expect: "moqt: not supported"},
		"unknown":       {code: ProbeErrorCode(0x99), expect: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expect, tt.code.String())
		})
	}
}

func TestStreamErrors_Error(t *testing.T) {
	known := &transport.StreamError{ErrorCode: transport.StreamErrorCode(SubscribeErrorCodeNotFound)}
	unknown := &transport.StreamError{ErrorCode: 0x99}

	tests := map[string]struct {
		err  error
		want string
	}{
		"announce known":    {err: &AnnounceError{known}, want: "moqt: not found"},
		"subscribe known":   {err: &SubscribeError{known}, want: "moqt: track does not exist"},
		"fetch known":       {err: &FetchError{known}, want: "moqt: not found"},
		"probe known":       {err: &ProbeError{known}, want: "moqt: not found"},
		"group known":       {err: &GroupError{known}, want: "moqt: not found"},
		"announce unknown":  {err: &AnnounceError{unknown}, want: unknown.Error()},
		"subscribe unknown": {err: &SubscribeError{unknown}, want: unknown.Error()},
		"fetch unknown":     {err: &FetchError{unknown}, want: unknown.Error()},
		"probe unknown":     {err: &ProbeError{unknown}, want: unknown.Error()},
		"group unknown":     {err: &GroupError{unknown}, want: unknown.Error()},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.err.Error())
		})
	}
}

func TestStreamErrors_Code(t *testing.T) {
	strErr := &transport.StreamError{ErrorCode: 0x41}

	assert.Equal(t, AnnounceErrorCode(0x41), (&AnnounceError{strErr}).AnnounceErrorCode())
	assert.Equal(t, SubscribeErrorCodeInvalidRange, (&SubscribeError{strErr}).SubscribeErrorCode())
	assert.Equal(t, FetchErrorCode(0x41), (&FetchError{strErr}).FetchErrorCode())
	assert.Equal(t, ProbeErrorCode(0x41), (&ProbeError{strErr}).ProbeErrorCode())
	assert.Equal(t, InvalidRangeGroupErrorCode, (&GroupError{strErr}).GroupErrorCode())
}

func TestSessionError(t *testing.T) {
	tests := map[string]struct {
		err      SessionError
		expected string
		code     SessionErrorCode
	}{
		"local internal error": {
			err:      SessionError{&transport.ApplicationError{ErrorCode: transport.ApplicationErrorCode(InternalSessionErrorCode)}},
			expected: "moqt: internal error (local)",
			code:     InternalSessionErrorCode,
		},
		"remote protocol violation": {
			err:      SessionError{&transport.ApplicationError{ErrorCode: transport.ApplicationErrorCode(ProtocolViolationErrorCode), Remote: true}},
			expected: "moqt: protocol violation (remote)",
			code:     ProtocolViolationErrorCode,
		},
		"remote control message timeout": {
			err:      SessionError{&transport.ApplicationError{ErrorCode: transport.ApplicationErrorCode(ControlMessageTimeoutErrorCode), Remote: true}},
			expected: "moqt: control message timeout (remote)",
			code:     ControlMessageTimeoutErrorCode,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.Equal(t, tt.code, tt.err.SessionErrorCode())
		})
	}
}

func TestSessionError_UnknownCodeFallback(t *testing.T) {
	appErr := &transport.ApplicationError{ErrorCode: 0x99, Remote: true}
	err := SessionError{appErr}

	assert.Equal(t, SessionErrorCode(0x99), err.SessionErrorCode())
	assert.Equal(t, appErr.Error(), err.Error())
}
