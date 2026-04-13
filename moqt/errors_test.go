package moqt

import (
	"testing"

	"github.com/okdaichi/gomoqt/transport"
	"github.com/stretchr/testify/assert"
)

// Test for standard errors
func TestStandardErrors(t *testing.T) {
	tests := map[string]struct {
		err    error
		expect string
	}{
		"invalid scheme": {
			err:    ErrInvalidScheme,
			expect: "moqt: invalid scheme",
		},
		"closed session": {
			err:    ErrClosedSession,
			expect: "moqt: closed session",
		},
		"server closed": {
			err:    ErrServerClosed,
			expect: "moqt: server closed",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expect, tt.err.Error())
		})
	}
}

// Test for AnnounceErrorCode.String method
func TestAnnounceErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   AnnounceErrorCode
		expect string
	}{
		"internal error code": {
			code:   AnnounceErrorCodeInternal,
			expect: "moqt: internal error",
		},
		"duplicated announce error code": {
			code:   AnnounceErrorCodeDuplicated,
			expect: "moqt: duplicated broadcast path",
		},
		"invalid announce status error code": {
			code:   AnnounceErrorCodeInvalidStatus,
			expect: "moqt: invalid announce status",
		},
		"uninterested error code": {
			code:   UninterestedErrorCode,
			expect: "moqt: uninterested",
		},
		"banned prefix error code": {
			code:   BannedPrefixErrorCode,
			expect: "moqt: banned prefix",
		},
		"invalid prefix error code": {
			code:   AnnounceErrorCodeInvalidPrefix,
			expect: "moqt: invalid prefix",
		},
		"unknown code": {
			code:   AnnounceErrorCode(0xFF), // Some arbitrary value not defined
			expect: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := tt.code.String()
			assert.Equal(t, tt.expect, result)

			// Verify that defined codes always return non-empty strings
			if tt.code != AnnounceErrorCode(0xFF) {
				assert.NotEmpty(t, result, "defined error code should return non-empty text")
			}
		})
	}
}

// Test for SubscribeErrorCode.String method
func TestSubscribeErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   SubscribeErrorCode
		expect string
	}{
		"internal error code": {
			code:   SubscribeErrorCodeInternal,
			expect: "moqt: internal error",
		},
		"invalid range error code": {
			code:   SubscribeErrorCodeInvalidRange,
			expect: "moqt: invalid range",
		},
		"duplicate subscribe ID error code": {
			code:   SubscribeErrorCodeDuplicateID,
			expect: "moqt: duplicated id",
		},
		"track not found error code": {
			code:   SubscribeErrorCodeNotFound,
			expect: "moqt: track does not exist",
		},
		"unauthorized subscribe error code": {
			code:   SubscribeErrorCodeUnauthorized,
			expect: "moqt: unauthorized",
		},
		"subscribe timeout error code": {
			code:   SubscribeErrorCodeTimeout,
			expect: "moqt: timeout",
		},
		"unknown code": {
			code:   SubscribeErrorCode(0xFF), // Some arbitrary value not defined
			expect: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := tt.code.String()
			assert.Equal(t, tt.expect, result)

			// Verify that defined codes always return non-empty strings
			if tt.code != SubscribeErrorCode(0xFF) {
				assert.NotEmpty(t, result, "defined error code should return non-empty text")
			}
		})
	}
}

// Test for SessionErrorCode.String method
func TestSessionErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   SessionErrorCode
		expect string
	}{
		"no error": {
			code:   NoError,
			expect: "moqt: no error",
		},
		"internal session error code": {
			code:   InternalSessionErrorCode,
			expect: "moqt: internal error",
		},
		"unauthorized session error code": {
			code:   UnauthorizedSessionErrorCode,
			expect: "moqt: unauthorized",
		},
		"protocol violation error code": {
			code:   ProtocolViolationErrorCode,
			expect: "moqt: protocol violation",
		},
		"parameter length mismatch error code": {
			code:   ParameterLengthMismatchErrorCode,
			expect: "moqt: parameter length mismatch",
		},
		"too many subscribe error code": {
			code:   TooManySubscribeErrorCode,
			expect: "moqt: too many subscribes",
		},
		"go away timeout error code": {
			code:   GoAwayTimeoutErrorCode,
			expect: "moqt: goaway timeout",
		},
		"unsupported version error code": {
			code:   UnsupportedVersionErrorCode,
			expect: "moqt: unsupported version",
		},
		"setup failed error code": {
			code:   SetupFailedErrorCode,
			expect: "moqt: setup failed",
		},
		"unknown code": {
			code:   SessionErrorCode(0xFF), // Some arbitrary value not defined
			expect: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := tt.code.String()
			assert.Equal(t, tt.expect, result)

			// Verify that defined codes always return non-empty strings
			if tt.code != SessionErrorCode(0xFF) {
				assert.NotEmpty(t, result, "defined error code should return non-empty text")
			}
		})
	}
}

// Test for GroupErrorCode.String method
func TestGroupErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   GroupErrorCode
		expect string
	}{
		"internal group error code": {
			code:   InternalGroupErrorCode,
			expect: "moqt: internal error",
		},
		"out of range error code": {
			code:   OutOfRangeErrorCode,
			expect: "moqt: out of range",
		},
		"expired group error code": {
			code:   ExpiredGroupErrorCode,
			expect: "moqt: group expires",
		},
		"subscribe canceled error code": {
			code:   SubscribeCanceledErrorCode,
			expect: "moqt: subscribe canceled",
		},
		"publish aborted error code": {
			code:   PublishAbortedErrorCode,
			expect: "moqt: publish aborted",
		},
		"closed session group error code": {
			code:   ClosedSessionGroupErrorCode,
			expect: "moqt: session closed",
		},
		"invalid subscribe ID error code": {
			code:   InvalidSubscribeIDErrorCode,
			expect: "moqt: invalid subscribe id",
		},
		"unknown code": {
			code:   GroupErrorCode(0xFF), // Some arbitrary value not defined
			expect: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := tt.code.String()
			assert.Equal(t, tt.expect, result)

			// Verify that defined codes always return non-empty strings
			if tt.code != GroupErrorCode(0xFF) {
				assert.NotEmpty(t, result, "defined error code should return non-empty text")
			}
		})
	}
}

// Test for AnnounceError with unknown code fallback
func TestAnnounceError_UnknownCodeFallback(t *testing.T) {
	unknownCode := AnnounceErrorCode(0x99)
	err := AnnounceError{
		&transport.StreamError{
			ErrorCode: transport.StreamErrorCode(unknownCode),
		},
	}

	// For unknown codes, should use the ErrorCode method's output
	result := err.Error()
	assert.Equal(t, unknownCode, err.AnnounceErrorCode())
	// The fallback behavior returns the underlying StreamError.Error()
	assert.NotEmpty(t, result)
}

// Test for SubscribeError with unknown code fallback
func TestSubscribeError_UnknownCodeFallback(t *testing.T) {
	unknownCode := SubscribeErrorCode(0x99)
	err := SubscribeError{
		&transport.StreamError{
			ErrorCode: transport.StreamErrorCode(unknownCode),
		},
	}

	// For unknown codes, should use the ErrorCode method's output
	result := err.Error()
	assert.Equal(t, unknownCode, err.SubscribeErrorCode())
	// The fallback behavior returns the underlying StreamError.Error()
	assert.NotEmpty(t, result)
}

// Test for SessionError with unknown code fallback
func TestSessionError_UnknownCodeFallback(t *testing.T) {
	tests := map[string]struct {
		remote bool
	}{
		"local unknown error": {
			remote: false,
		},
		"remote unknown error": {
			remote: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			unknownCode := SessionErrorCode(0x99)
			err := SessionError{
				&transport.ApplicationError{
					ErrorCode: transport.ApplicationErrorCode(unknownCode),
					Remote:    tt.remote,
				},
			}

			// For unknown codes, should use the ErrorCode method's output
			result := err.Error()
			assert.Equal(t, unknownCode, err.SessionErrorCode())
			// The fallback behavior returns the underlying ApplicationError.Error()
			assert.NotEmpty(t, result)
		})
	}
}

// Test for GroupError with unknown code fallback
func TestGroupError_UnknownCodeFallback(t *testing.T) {
	unknownCode := GroupErrorCode(0x99)
	err := GroupError{
		&transport.StreamError{
			ErrorCode: transport.StreamErrorCode(unknownCode),
		},
	}

	// For unknown codes, should use the ErrorCode method's output
	result := err.Error()
	assert.Equal(t, unknownCode, err.GroupErrorCode())
	// The fallback behavior returns the underlying StreamError.Error()
	assert.NotEmpty(t, result)
}

// Test for AnnounceError
func TestAnnounceError(t *testing.T) {
	tests := map[string]struct {
		err            AnnounceError
		expectedString string
		expectedCode   AnnounceErrorCode
	}{
		"internal error": {
			err: AnnounceError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(AnnounceErrorCodeInternal),
				},
			},
			expectedString: "moqt: internal error",
			expectedCode:   AnnounceErrorCodeInternal,
		},
		"duplicated broadcast path": {
			err: AnnounceError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(AnnounceErrorCodeDuplicated),
				},
			},
			expectedString: "moqt: duplicated broadcast path",
			expectedCode:   AnnounceErrorCodeDuplicated,
		},
		"invalid announce status": {
			err: AnnounceError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(AnnounceErrorCodeInvalidStatus),
				},
			},
			expectedString: "moqt: invalid announce status",
			expectedCode:   AnnounceErrorCodeInvalidStatus,
		},
		"uninterested": {
			err: AnnounceError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(UninterestedErrorCode),
				},
			},
			expectedString: "moqt: uninterested",
			expectedCode:   UninterestedErrorCode,
		},
		"banned prefix": {
			err: AnnounceError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(BannedPrefixErrorCode),
				},
			},
			expectedString: "moqt: banned prefix",
			expectedCode:   BannedPrefixErrorCode,
		},
		"invalid prefix": {
			err: AnnounceError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(AnnounceErrorCodeInvalidPrefix),
				},
			},
			expectedString: "moqt: invalid prefix",
			expectedCode:   AnnounceErrorCodeInvalidPrefix,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expectedString, tt.err.Error())
			assert.Equal(t, tt.expectedCode, tt.err.AnnounceErrorCode())
		})
	}
}

// Test for SubscribeError
func TestSubscribeError(t *testing.T) {
	tests := map[string]struct {
		err            SubscribeError
		expectedString string
		expectedCode   SubscribeErrorCode
	}{
		"internal error": {
			err: SubscribeError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(SubscribeErrorCodeInternal),
				},
			},
			expectedString: "moqt: internal error",
			expectedCode:   SubscribeErrorCodeInternal,
		},
		"invalid range": {
			err: SubscribeError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(SubscribeErrorCodeInvalidRange),
				},
			},
			expectedString: "moqt: invalid range",
			expectedCode:   SubscribeErrorCodeInvalidRange,
		},
		"duplicate subscribe ID": {
			err: SubscribeError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(SubscribeErrorCodeDuplicateID),
				},
			},
			expectedString: "moqt: duplicated id",
			expectedCode:   SubscribeErrorCodeDuplicateID,
		},
		"track not found": {
			err: SubscribeError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(SubscribeErrorCodeNotFound),
				},
			},
			expectedString: "moqt: track does not exist",
			expectedCode:   SubscribeErrorCodeNotFound,
		},
		"unauthorized": {
			err: SubscribeError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(SubscribeErrorCodeUnauthorized),
				},
			},
			expectedString: "moqt: unauthorized",
			expectedCode:   SubscribeErrorCodeUnauthorized,
		},
		"timeout": {
			err: SubscribeError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(SubscribeErrorCodeTimeout),
				},
			},
			expectedString: "moqt: timeout",
			expectedCode:   SubscribeErrorCodeTimeout,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expectedString, tt.err.Error())
			assert.Equal(t, tt.expectedCode, tt.err.SubscribeErrorCode())
		})
	}
}

// Test for SessionError
func TestSessionError(t *testing.T) {
	tests := map[string]struct {
		err            SessionError
		expectedString string
		expectedCode   SessionErrorCode
	}{
		"local internal error": {
			err: SessionError{
				&transport.ApplicationError{
					ErrorCode: transport.ApplicationErrorCode(InternalSessionErrorCode),
					Remote:    false,
				},
			},
			expectedString: "moqt: internal error (local)",
			expectedCode:   InternalSessionErrorCode,
		},
		"remote unauthorized": {
			err: SessionError{
				&transport.ApplicationError{
					ErrorCode: transport.ApplicationErrorCode(UnauthorizedSessionErrorCode),
					Remote:    true,
				},
			},
			expectedString: "moqt: unauthorized (remote)",
			expectedCode:   UnauthorizedSessionErrorCode,
		},
		"local protocol violation": {
			err: SessionError{
				&transport.ApplicationError{
					ErrorCode: transport.ApplicationErrorCode(ProtocolViolationErrorCode),
					Remote:    false,
				},
			},
			expectedString: "moqt: protocol violation (local)",
			expectedCode:   ProtocolViolationErrorCode,
		},
		"remote parameter length mismatch": {
			err: SessionError{
				&transport.ApplicationError{
					ErrorCode: transport.ApplicationErrorCode(ParameterLengthMismatchErrorCode),
					Remote:    true,
				},
			},
			expectedString: "moqt: parameter length mismatch (remote)",
			expectedCode:   ParameterLengthMismatchErrorCode,
		},
		"local too many subscribes": {
			err: SessionError{
				&transport.ApplicationError{
					ErrorCode: transport.ApplicationErrorCode(TooManySubscribeErrorCode),
					Remote:    false,
				},
			},
			expectedString: "moqt: too many subscribes (local)",
			expectedCode:   TooManySubscribeErrorCode,
		},
		"local goaway timeout": {
			err: SessionError{
				&transport.ApplicationError{
					ErrorCode: transport.ApplicationErrorCode(GoAwayTimeoutErrorCode),
					Remote:    false,
				},
			},
			expectedString: "moqt: goaway timeout (local)",
			expectedCode:   GoAwayTimeoutErrorCode,
		},
		"remote unsupported version": {
			err: SessionError{
				&transport.ApplicationError{
					ErrorCode: transport.ApplicationErrorCode(UnsupportedVersionErrorCode),
					Remote:    true,
				},
			},
			expectedString: "moqt: unsupported version (remote)",
			expectedCode:   UnsupportedVersionErrorCode,
		},
		"local setup failed": {
			err: SessionError{
				&transport.ApplicationError{
					ErrorCode: transport.ApplicationErrorCode(SetupFailedErrorCode),
					Remote:    false,
				},
			},
			expectedString: "moqt: setup failed (local)",
			expectedCode:   SetupFailedErrorCode,
		},
		"local no error": {
			err: SessionError{
				&transport.ApplicationError{
					ErrorCode: transport.ApplicationErrorCode(NoError),
					Remote:    false,
				},
			},
			expectedString: "moqt: no error (local)",
			expectedCode:   NoError,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expectedString, tt.err.Error())
			assert.Equal(t, tt.expectedCode, tt.err.SessionErrorCode())
		})
	}
}

// Test for GroupError
func TestGroupError(t *testing.T) {
	tests := map[string]struct {
		err            GroupError
		expectedString string
		expectedCode   GroupErrorCode
	}{
		"internal error": {
			err: GroupError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(InternalGroupErrorCode),
				},
			},
			expectedString: "moqt: internal error",
			expectedCode:   InternalGroupErrorCode,
		},
		"out of range": {
			err: GroupError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(OutOfRangeErrorCode),
				},
			},
			expectedString: "moqt: out of range",
			expectedCode:   OutOfRangeErrorCode,
		},
		"expired group": {
			err: GroupError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(ExpiredGroupErrorCode),
				},
			},
			expectedString: "moqt: group expires",
			expectedCode:   ExpiredGroupErrorCode,
		},
		"subscribe canceled": {
			err: GroupError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(SubscribeCanceledErrorCode),
				},
			},
			expectedString: "moqt: subscribe canceled",
			expectedCode:   SubscribeCanceledErrorCode,
		},
		"publish aborted": {
			err: GroupError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(PublishAbortedErrorCode),
				},
			},
			expectedString: "moqt: publish aborted",
			expectedCode:   PublishAbortedErrorCode,
		},
		"session closed": {
			err: GroupError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(ClosedSessionGroupErrorCode),
				},
			},
			expectedString: "moqt: session closed",
			expectedCode:   ClosedSessionGroupErrorCode,
		},
		"invalid subscribe ID": {
			err: GroupError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(InvalidSubscribeIDErrorCode),
				},
			},
			expectedString: "moqt: invalid subscribe id",
			expectedCode:   InvalidSubscribeIDErrorCode,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expectedString, tt.err.Error())
			assert.Equal(t, tt.expectedCode, tt.err.GroupErrorCode())
		})
	}
}

// Test consistency between ErrorText functions and Error types
func TestErrorTextConsistency(t *testing.T) {
	t.Run("AnnounceError consistency", func(t *testing.T) {
		// Test all defined announce error codes
		codes := []AnnounceErrorCode{
			AnnounceErrorCodeInternal,
			AnnounceErrorCodeDuplicated,
			AnnounceErrorCodeInvalidStatus,
			UninterestedErrorCode,
			BannedPrefixErrorCode,
			AnnounceErrorCodeInvalidPrefix,
		}

		for _, code := range codes {
			text := code.String()
			err := AnnounceError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(code),
				},
			}
			assert.Equal(t, text, err.Error(), "AnnounceErrorCode.String and AnnounceError.Error() should return the same text for code %v", code)
		}
	})

	t.Run("SubscribeError consistency", func(t *testing.T) {
		// Test all defined subscribe error codes
		codes := []SubscribeErrorCode{
			SubscribeErrorCodeInternal,
			SubscribeErrorCodeInvalidRange,
			SubscribeErrorCodeDuplicateID,
			SubscribeErrorCodeNotFound,
			SubscribeErrorCodeUnauthorized,
			SubscribeErrorCodeTimeout,
		}

		for _, code := range codes {
			text := code.String()
			err := SubscribeError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(code),
				},
			}
			assert.Equal(t, text, err.Error(), "SubscribeErrorCode.String and SubscribeError.Error() should return the same text for code %v", code)
		}
	})

	t.Run("SessionError consistency", func(t *testing.T) {
		// Test all defined session error codes
		codes := []SessionErrorCode{
			NoError,
			InternalSessionErrorCode,
			UnauthorizedSessionErrorCode,
			ProtocolViolationErrorCode,
			ParameterLengthMismatchErrorCode,
			TooManySubscribeErrorCode,
			GoAwayTimeoutErrorCode,
			UnsupportedVersionErrorCode,
			SetupFailedErrorCode,
		}

		for _, code := range codes {
			text := code.String()

			// Test both local and remote
			for _, remote := range []bool{false, true} {
				err := SessionError{
					&transport.ApplicationError{
						ErrorCode: transport.ApplicationErrorCode(code),
						Remote:    remote,
					},
				}

				suffix := "(local)"
				if remote {
					suffix = "(remote)"
				}
				expectedText := text + " " + suffix

				assert.Equal(t, expectedText, err.Error(), "SessionError.Error() should return text with suffix for code %v (remote=%v)", code, remote)
			}
		}
	})

	t.Run("GroupError consistency", func(t *testing.T) {
		// Test all defined group error codes
		codes := []GroupErrorCode{
			InternalGroupErrorCode,
			OutOfRangeErrorCode,
			ExpiredGroupErrorCode,
			SubscribeCanceledErrorCode,
			PublishAbortedErrorCode,
			ClosedSessionGroupErrorCode,
			InvalidSubscribeIDErrorCode,
		}

		for _, code := range codes {
			text := code.String()
			err := GroupError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(code),
				},
			}
			assert.Equal(t, text, err.Error(), "GroupErrorCode.String and GroupError.Error() should return the same text for code %v", code)
		}
	})
}

// Test that all error codes return non-empty text
func TestErrorText_NonEmpty(t *testing.T) {
	t.Run("AnnounceErrorCode.String returns non-empty for all defined codes", func(t *testing.T) {
		codes := []AnnounceErrorCode{
			AnnounceErrorCodeInternal,
			AnnounceErrorCodeDuplicated,
			AnnounceErrorCodeInvalidStatus,
			UninterestedErrorCode,
			BannedPrefixErrorCode,
			AnnounceErrorCodeInvalidPrefix,
		}

		for _, code := range codes {
			text := code.String()
			assert.NotEmpty(t, text, "AnnounceErrorCode.String should return non-empty text for code %v", code)
		}
	})

	t.Run("SubscribeErrorCode.String returns non-empty for all defined codes", func(t *testing.T) {
		codes := []SubscribeErrorCode{
			SubscribeErrorCodeInternal,
			SubscribeErrorCodeInvalidRange,
			SubscribeErrorCodeDuplicateID,
			SubscribeErrorCodeNotFound,
			SubscribeErrorCodeUnauthorized,
			SubscribeErrorCodeTimeout,
		}

		for _, code := range codes {
			text := code.String()
			assert.NotEmpty(t, text, "SubscribeErrorCode.String should return non-empty text for code %v", code)
		}
	})

	t.Run("SessionErrorCode.String returns non-empty for all defined codes", func(t *testing.T) {
		codes := []SessionErrorCode{
			NoError,
			InternalSessionErrorCode,
			UnauthorizedSessionErrorCode,
			ProtocolViolationErrorCode,
			ParameterLengthMismatchErrorCode,
			TooManySubscribeErrorCode,
			GoAwayTimeoutErrorCode,
			UnsupportedVersionErrorCode,
			SetupFailedErrorCode,
		}

		for _, code := range codes {
			text := code.String()
			assert.NotEmpty(t, text, "SessionErrorCode.String should return non-empty text for code %v", code)
		}
	})

	t.Run("GroupErrorCode.String returns non-empty for all defined codes", func(t *testing.T) {
		codes := []GroupErrorCode{
			InternalGroupErrorCode,
			OutOfRangeErrorCode,
			ExpiredGroupErrorCode,
			SubscribeCanceledErrorCode,
			PublishAbortedErrorCode,
			ClosedSessionGroupErrorCode,
			InvalidSubscribeIDErrorCode,
		}

		for _, code := range codes {
			text := code.String()
			assert.NotEmpty(t, text, "GroupErrorCode.String should return non-empty text for code %v", code)
		}
	})
}

// Test for FetchErrorCode.String method
func TestFetchErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   FetchErrorCode
		expect string
	}{
		"internal error code": {
			code:   FetchErrorCodeInternal,
			expect: "moqt: internal error",
		},
		"timeout error code": {
			code:   FetchErrorCodeTimeout,
			expect: "moqt: timeout",
		},
		"unknown code": {
			code:   FetchErrorCode(0xFF),
			expect: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := tt.code.String()
			assert.Equal(t, tt.expect, result)
		})
	}
}

// Test for FetchError
func TestFetchError(t *testing.T) {
	tests := map[string]struct {
		err            FetchError
		expectedString string
		expectedCode   FetchErrorCode
	}{
		"internal error": {
			err: FetchError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(FetchErrorCodeInternal),
				},
			},
			expectedString: "moqt: internal error",
			expectedCode:   FetchErrorCodeInternal,
		},
		"timeout": {
			err: FetchError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(FetchErrorCodeTimeout),
				},
			},
			expectedString: "moqt: timeout",
			expectedCode:   FetchErrorCodeTimeout,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expectedString, tt.err.Error())
			assert.Equal(t, tt.expectedCode, tt.err.FetchErrorCode())
		})
	}
}

// Test for FetchError with unknown code fallback
func TestFetchError_UnknownCodeFallback(t *testing.T) {
	unknownCode := FetchErrorCode(0x99)
	err := FetchError{
		&transport.StreamError{
			ErrorCode: transport.StreamErrorCode(unknownCode),
		},
	}

	result := err.Error()
	assert.Equal(t, unknownCode, err.FetchErrorCode())
	assert.NotEmpty(t, result)
}

// Test for ProbeErrorCode.String method
func TestProbeErrorCode_String(t *testing.T) {
	tests := map[string]struct {
		code   ProbeErrorCode
		expect string
	}{
		"internal error code": {
			code:   ProbeErrorCodeInternal,
			expect: "moqt: internal error",
		},
		"timeout error code": {
			code:   ProbeErrorCodeTimeout,
			expect: "moqt: timeout",
		},
		"not supported error code": {
			code:   ProbeErrorCodeNotSupported,
			expect: "moqt: not supported",
		},
		"unknown code": {
			code:   ProbeErrorCode(0xFF),
			expect: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := tt.code.String()
			assert.Equal(t, tt.expect, result)
		})
	}
}

// Test for ProbeError
func TestProbeError(t *testing.T) {
	tests := map[string]struct {
		err            ProbeError
		expectedString string
		expectedCode   ProbeErrorCode
	}{
		"internal error": {
			err: ProbeError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(ProbeErrorCodeInternal),
				},
			},
			expectedString: "moqt: internal error",
			expectedCode:   ProbeErrorCodeInternal,
		},
		"timeout": {
			err: ProbeError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(ProbeErrorCodeTimeout),
				},
			},
			expectedString: "moqt: timeout",
			expectedCode:   ProbeErrorCodeTimeout,
		},
		"not supported": {
			err: ProbeError{
				&transport.StreamError{
					ErrorCode: transport.StreamErrorCode(ProbeErrorCodeNotSupported),
				},
			},
			expectedString: "moqt: not supported",
			expectedCode:   ProbeErrorCodeNotSupported,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expectedString, tt.err.Error())
			assert.Equal(t, tt.expectedCode, tt.err.ProbeErrorCode())
		})
	}
}

// Test for ProbeError with unknown code fallback
func TestProbeError_UnknownCodeFallback(t *testing.T) {
	unknownCode := ProbeErrorCode(0x99)
	err := ProbeError{
		&transport.StreamError{
			ErrorCode: transport.StreamErrorCode(unknownCode),
		},
	}

	result := err.Error()
	assert.Equal(t, unknownCode, err.ProbeErrorCode())
	assert.NotEmpty(t, result)
}
