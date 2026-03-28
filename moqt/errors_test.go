package moqt

import (
	"github.com/stretchr/testify/assert"
	"testing"
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
		"client closed": {
			err:    ErrClientClosed,
			expect: "moqt: client closed",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expect, tt.err.Error())
		})
	}
}

// Test for AnnounceErrorText function
func TestAnnounceErrorText(t *testing.T) {
	tests := map[string]struct {
		code   AnnounceErrorCode
		expect string
	}{
		"internal error code": {
			code:   InternalAnnounceErrorCode,
			expect: "moqt: internal error",
		},
		"duplicated announce error code": {
			code:   DuplicatedAnnounceErrorCode,
			expect: "moqt: duplicated broadcast path",
		},
		"invalid announce status error code": {
			code:   InvalidAnnounceStatusErrorCode,
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
			code:   InvalidPrefixErrorCode,
			expect: "moqt: invalid prefix",
		},
		"unknown code": {
			code:   AnnounceErrorCode(0xFF), // Some arbitrary value not defined
			expect: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := AnnounceErrorText(tt.code)
			assert.Equal(t, tt.expect, result)

			// Verify that defined codes always return non-empty strings
			if tt.code != AnnounceErrorCode(0xFF) {
				assert.NotEmpty(t, result, "defined error code should return non-empty text")
			}
		})
	}
}

// Test for SubscribeErrorText function
func TestSubscribeErrorText(t *testing.T) {
	tests := map[string]struct {
		code   SubscribeErrorCode
		expect string
	}{
		"internal error code": {
			code:   InternalSubscribeErrorCode,
			expect: "moqt: internal error",
		},
		"invalid range error code": {
			code:   InvalidRangeErrorCode,
			expect: "moqt: invalid range",
		},
		"duplicate subscribe ID error code": {
			code:   DuplicateSubscribeIDErrorCode,
			expect: "moqt: duplicated id",
		},
		"track not found error code": {
			code:   TrackNotFoundErrorCode,
			expect: "moqt: track does not exist",
		},
		"unauthorized subscribe error code": {
			code:   UnauthorizedSubscribeErrorCode,
			expect: "moqt: unauthorized",
		},
		"subscribe timeout error code": {
			code:   SubscribeTimeoutErrorCode,
			expect: "moqt: timeout",
		},
		"unknown code": {
			code:   SubscribeErrorCode(0xFF), // Some arbitrary value not defined
			expect: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := SubscribeErrorText(tt.code)
			assert.Equal(t, tt.expect, result)

			// Verify that defined codes always return non-empty strings
			if tt.code != SubscribeErrorCode(0xFF) {
				assert.NotEmpty(t, result, "defined error code should return non-empty text")
			}
		})
	}
}

// Test for SessionErrorText function
func TestSessionErrorText(t *testing.T) {
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
			result := SessionErrorText(tt.code)
			assert.Equal(t, tt.expect, result)

			// Verify that defined codes always return non-empty strings
			if tt.code != SessionErrorCode(0xFF) {
				assert.NotEmpty(t, result, "defined error code should return non-empty text")
			}
		})
	}
}

// Test for GroupErrorText function
func TestGroupErrorText(t *testing.T) {
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
			result := GroupErrorText(tt.code)
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
		&StreamError{
			ErrorCode: StreamErrorCode(unknownCode),
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
		&StreamError{
			ErrorCode: StreamErrorCode(unknownCode),
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
				&ApplicationError{
					ErrorCode: ApplicationErrorCode(unknownCode),
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
		&StreamError{
			ErrorCode: StreamErrorCode(unknownCode),
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
				&StreamError{
					ErrorCode: StreamErrorCode(InternalAnnounceErrorCode),
				},
			},
			expectedString: "moqt: internal error",
			expectedCode:   InternalAnnounceErrorCode,
		},
		"duplicated broadcast path": {
			err: AnnounceError{
				&StreamError{
					ErrorCode: StreamErrorCode(DuplicatedAnnounceErrorCode),
				},
			},
			expectedString: "moqt: duplicated broadcast path",
			expectedCode:   DuplicatedAnnounceErrorCode,
		},
		"invalid announce status": {
			err: AnnounceError{
				&StreamError{
					ErrorCode: StreamErrorCode(InvalidAnnounceStatusErrorCode),
				},
			},
			expectedString: "moqt: invalid announce status",
			expectedCode:   InvalidAnnounceStatusErrorCode,
		},
		"uninterested": {
			err: AnnounceError{
				&StreamError{
					ErrorCode: StreamErrorCode(UninterestedErrorCode),
				},
			},
			expectedString: "moqt: uninterested",
			expectedCode:   UninterestedErrorCode,
		},
		"banned prefix": {
			err: AnnounceError{
				&StreamError{
					ErrorCode: StreamErrorCode(BannedPrefixErrorCode),
				},
			},
			expectedString: "moqt: banned prefix",
			expectedCode:   BannedPrefixErrorCode,
		},
		"invalid prefix": {
			err: AnnounceError{
				&StreamError{
					ErrorCode: StreamErrorCode(InvalidPrefixErrorCode),
				},
			},
			expectedString: "moqt: invalid prefix",
			expectedCode:   InvalidPrefixErrorCode,
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
				&StreamError{
					ErrorCode: StreamErrorCode(InternalSubscribeErrorCode),
				},
			},
			expectedString: "moqt: internal error",
			expectedCode:   InternalSubscribeErrorCode,
		},
		"invalid range": {
			err: SubscribeError{
				&StreamError{
					ErrorCode: StreamErrorCode(InvalidRangeErrorCode),
				},
			},
			expectedString: "moqt: invalid range",
			expectedCode:   InvalidRangeErrorCode,
		},
		"duplicate subscribe ID": {
			err: SubscribeError{
				&StreamError{
					ErrorCode: StreamErrorCode(DuplicateSubscribeIDErrorCode),
				},
			},
			expectedString: "moqt: duplicated id",
			expectedCode:   DuplicateSubscribeIDErrorCode,
		},
		"track not found": {
			err: SubscribeError{
				&StreamError{
					ErrorCode: StreamErrorCode(TrackNotFoundErrorCode),
				},
			},
			expectedString: "moqt: track does not exist",
			expectedCode:   TrackNotFoundErrorCode,
		},
		"unauthorized": {
			err: SubscribeError{
				&StreamError{
					ErrorCode: StreamErrorCode(UnauthorizedSubscribeErrorCode),
				},
			},
			expectedString: "moqt: unauthorized",
			expectedCode:   UnauthorizedSubscribeErrorCode,
		},
		"timeout": {
			err: SubscribeError{
				&StreamError{
					ErrorCode: StreamErrorCode(SubscribeTimeoutErrorCode),
				},
			},
			expectedString: "moqt: timeout",
			expectedCode:   SubscribeTimeoutErrorCode,
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
				&ApplicationError{
					ErrorCode: ApplicationErrorCode(InternalSessionErrorCode),
					Remote:    false,
				},
			},
			expectedString: "moqt: internal error (local)",
			expectedCode:   InternalSessionErrorCode,
		},
		"remote unauthorized": {
			err: SessionError{
				&ApplicationError{
					ErrorCode: ApplicationErrorCode(UnauthorizedSessionErrorCode),
					Remote:    true,
				},
			},
			expectedString: "moqt: unauthorized (remote)",
			expectedCode:   UnauthorizedSessionErrorCode,
		},
		"local protocol violation": {
			err: SessionError{
				&ApplicationError{
					ErrorCode: ApplicationErrorCode(ProtocolViolationErrorCode),
					Remote:    false,
				},
			},
			expectedString: "moqt: protocol violation (local)",
			expectedCode:   ProtocolViolationErrorCode,
		},
		"remote parameter length mismatch": {
			err: SessionError{
				&ApplicationError{
					ErrorCode: ApplicationErrorCode(ParameterLengthMismatchErrorCode),
					Remote:    true,
				},
			},
			expectedString: "moqt: parameter length mismatch (remote)",
			expectedCode:   ParameterLengthMismatchErrorCode,
		},
		"local too many subscribes": {
			err: SessionError{
				&ApplicationError{
					ErrorCode: ApplicationErrorCode(TooManySubscribeErrorCode),
					Remote:    false,
				},
			},
			expectedString: "moqt: too many subscribes (local)",
			expectedCode:   TooManySubscribeErrorCode,
		},
		"local goaway timeout": {
			err: SessionError{
				&ApplicationError{
					ErrorCode: ApplicationErrorCode(GoAwayTimeoutErrorCode),
					Remote:    false,
				},
			},
			expectedString: "moqt: goaway timeout (local)",
			expectedCode:   GoAwayTimeoutErrorCode,
		},
		"remote unsupported version": {
			err: SessionError{
				&ApplicationError{
					ErrorCode: ApplicationErrorCode(UnsupportedVersionErrorCode),
					Remote:    true,
				},
			},
			expectedString: "moqt: unsupported version (remote)",
			expectedCode:   UnsupportedVersionErrorCode,
		},
		"local setup failed": {
			err: SessionError{
				&ApplicationError{
					ErrorCode: ApplicationErrorCode(SetupFailedErrorCode),
					Remote:    false,
				},
			},
			expectedString: "moqt: setup failed (local)",
			expectedCode:   SetupFailedErrorCode,
		},
		"local no error": {
			err: SessionError{
				&ApplicationError{
					ErrorCode: ApplicationErrorCode(NoError),
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
				&StreamError{
					ErrorCode: StreamErrorCode(InternalGroupErrorCode),
				},
			},
			expectedString: "moqt: internal error",
			expectedCode:   InternalGroupErrorCode,
		},
		"out of range": {
			err: GroupError{
				&StreamError{
					ErrorCode: StreamErrorCode(OutOfRangeErrorCode),
				},
			},
			expectedString: "moqt: out of range",
			expectedCode:   OutOfRangeErrorCode,
		},
		"expired group": {
			err: GroupError{
				&StreamError{
					ErrorCode: StreamErrorCode(ExpiredGroupErrorCode),
				},
			},
			expectedString: "moqt: group expires",
			expectedCode:   ExpiredGroupErrorCode,
		},
		"subscribe canceled": {
			err: GroupError{
				&StreamError{
					ErrorCode: StreamErrorCode(SubscribeCanceledErrorCode),
				},
			},
			expectedString: "moqt: subscribe canceled",
			expectedCode:   SubscribeCanceledErrorCode,
		},
		"publish aborted": {
			err: GroupError{
				&StreamError{
					ErrorCode: StreamErrorCode(PublishAbortedErrorCode),
				},
			},
			expectedString: "moqt: publish aborted",
			expectedCode:   PublishAbortedErrorCode,
		},
		"session closed": {
			err: GroupError{
				&StreamError{
					ErrorCode: StreamErrorCode(ClosedSessionGroupErrorCode),
				},
			},
			expectedString: "moqt: session closed",
			expectedCode:   ClosedSessionGroupErrorCode,
		},
		"invalid subscribe ID": {
			err: GroupError{
				&StreamError{
					ErrorCode: StreamErrorCode(InvalidSubscribeIDErrorCode),
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

// // Test for InternalError
// func TestInternalError(t *testing.T) {
// 	tests := map[string]struct {
// 		err                      InternalError
// 		expectedString           string
// 		expectedAnnounceCode     AnnounceErrorCode
// 		expectedSubscribeCode    SubscribeErrorCode
// 		expectedSessionCode      SessionErrorCode
// 		expectedGroupCode        GroupErrorCode
// 		shouldMatchInternalError bool
// 	}{
// 		"with reason": {
// 			err:                      InternalError{Reason: "test error"},
// 			expectedString:           "moqt: internal error: test error",
// 			expectedAnnounceCode:     InternalAnnounceErrorCode,
// 			expectedSubscribeCode:    InternalSubscribeErrorCode,
// 			expectedSessionCode:      InternalSessionErrorCode,
// 			expectedGroupCode:        InternalGroupErrorCode,
// 			shouldMatchInternalError: true,
// 		},
// 		"empty reason": {
// 			err:                      InternalError{Reason: ""},
// 			expectedString:           "moqt: internal error: ",
// 			expectedAnnounceCode:     InternalAnnounceErrorCode,
// 			expectedSubscribeCode:    InternalSubscribeErrorCode,
// 			expectedSessionCode:      InternalSessionErrorCode,
// 			expectedGroupCode:        InternalGroupErrorCode,
// 			shouldMatchInternalError: true,
// 		},
// 	}

// 	for name, tt := range tests {
// 		t.Run(name, func(t *testing.T) {
// 			assert.Equal(t, tt.expectedString, tt.err.Error())
// 			assert.Equal(t, tt.expectedAnnounceCode, tt.err.AnnounceErrorCode())
// 			assert.Equal(t, tt.expectedSubscribeCode, tt.err.SubscribeErrorCode())
// 			assert.Equal(t, tt.expectedSessionCode, tt.err.SessionErrorCode())
// 			assert.Equal(t, tt.expectedGroupCode, tt.err.GroupErrorCode())

// 			// Test Is method
// 			var internalErr InternalError
// 			assert.Equal(t, tt.shouldMatchInternalError, errors.Is(tt.err, internalErr))
// 		})
// 	}
// }

// // Test for UnauthorizedError
// func TestUnauthorizedError(t *testing.T) {
// 	tests := map[string]struct {
// 		err                        UnauthorizedError
// 		expectedString             string
// 		expectedSubscribeCode      SubscribeErrorCode
// 		expectedSessionCode        SessionErrorCode
// 		shouldMatchUnauthorizedErr bool
// 	}{
// 		"default": {
// 			err:                        UnauthorizedError{},
// 			expectedString:             "moqt: unauthorized",
// 			expectedSubscribeCode:      UnauthorizedSubscribeErrorCode,
// 			expectedSessionCode:        UnauthorizedSessionErrorCode,
// 			shouldMatchUnauthorizedErr: true,
// 		},
// 	}

// 	for name, tt := range tests {
// 		t.Run(name, func(t *testing.T) {
// 			assert.Equal(t, tt.expectedString, tt.err.Error())
// 			assert.Equal(t, tt.expectedSubscribeCode, tt.err.SubscribeErrorCode())
// 			assert.Equal(t, tt.expectedSessionCode, tt.err.SessionErrorCode())

// 			// Test Is method
// 			var unauthorizedErr UnauthorizedError
// 			assert.Equal(t, tt.shouldMatchUnauthorizedErr, errors.Is(tt.err, unauthorizedErr))
// 		})
// 	}
// }

// // Test for error compatibility with standard errors
// func TestErrorCompatibility(t *testing.T) {
// 	// Test errors.Is compatibility
// 	t.Run("errors.Is with InternalError", func(t *testing.T) {
// 		err := InternalError{Reason: "test"}
// 		var internalErr InternalError
// 		assert.True(t, errors.Is(err, internalErr))
// 	})

// 	t.Run("errors.Is with UnauthorizedError", func(t *testing.T) {
// 		err := UnauthorizedError{}
// 		var unauthorizedErr UnauthorizedError
// 		assert.True(t, errors.Is(err, unauthorizedErr))
// 	})

// 	// Test errors.As compatibility
// 	t.Run("errors.As with InternalError", func(t *testing.T) {
// 		err := InternalError{Reason: "test"}
// 		var target InternalError
// 		assert.True(t, errors.As(err, &target))
// 		assert.Equal(t, "test", target.Reason)
// 	})

// 	t.Run("errors.As with UnauthorizedError", func(t *testing.T) {
// 		err := UnauthorizedError{}
// 		var target UnauthorizedError
// 		assert.True(t, errors.As(err, &target))
// 	})
// }

// Test consistency between ErrorText functions and Error types
func TestErrorTextConsistency(t *testing.T) {
	t.Run("AnnounceError consistency", func(t *testing.T) {
		// Test all defined announce error codes
		codes := []AnnounceErrorCode{
			InternalAnnounceErrorCode,
			DuplicatedAnnounceErrorCode,
			InvalidAnnounceStatusErrorCode,
			UninterestedErrorCode,
			BannedPrefixErrorCode,
			InvalidPrefixErrorCode,
		}

		for _, code := range codes {
			text := AnnounceErrorText(code)
			err := AnnounceError{
				&StreamError{
					ErrorCode: StreamErrorCode(code),
				},
			}
			assert.Equal(t, text, err.Error(), "AnnounceErrorText and AnnounceError.Error() should return the same text for code %v", code)
		}
	})

	t.Run("SubscribeError consistency", func(t *testing.T) {
		// Test all defined subscribe error codes
		codes := []SubscribeErrorCode{
			InternalSubscribeErrorCode,
			InvalidRangeErrorCode,
			DuplicateSubscribeIDErrorCode,
			TrackNotFoundErrorCode,
			UnauthorizedSubscribeErrorCode,
			SubscribeTimeoutErrorCode,
		}

		for _, code := range codes {
			text := SubscribeErrorText(code)
			err := SubscribeError{
				&StreamError{
					ErrorCode: StreamErrorCode(code),
				},
			}
			assert.Equal(t, text, err.Error(), "SubscribeErrorText and SubscribeError.Error() should return the same text for code %v", code)
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
			text := SessionErrorText(code)

			// Test both local and remote
			for _, remote := range []bool{false, true} {
				err := SessionError{
					&ApplicationError{
						ErrorCode: ApplicationErrorCode(code),
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
			text := GroupErrorText(code)
			err := GroupError{
				&StreamError{
					ErrorCode: StreamErrorCode(code),
				},
			}
			assert.Equal(t, text, err.Error(), "GroupErrorText and GroupError.Error() should return the same text for code %v", code)
		}
	})
}

// Test that all error codes return non-empty text
func TestErrorText_NonEmpty(t *testing.T) {
	t.Run("AnnounceErrorText returns non-empty for all defined codes", func(t *testing.T) {
		codes := []AnnounceErrorCode{
			InternalAnnounceErrorCode,
			DuplicatedAnnounceErrorCode,
			InvalidAnnounceStatusErrorCode,
			UninterestedErrorCode,
			BannedPrefixErrorCode,
			InvalidPrefixErrorCode,
		}

		for _, code := range codes {
			text := AnnounceErrorText(code)
			assert.NotEmpty(t, text, "AnnounceErrorText should return non-empty text for code %v", code)
		}
	})

	t.Run("SubscribeErrorText returns non-empty for all defined codes", func(t *testing.T) {
		codes := []SubscribeErrorCode{
			InternalSubscribeErrorCode,
			InvalidRangeErrorCode,
			DuplicateSubscribeIDErrorCode,
			TrackNotFoundErrorCode,
			UnauthorizedSubscribeErrorCode,
			SubscribeTimeoutErrorCode,
		}

		for _, code := range codes {
			text := SubscribeErrorText(code)
			assert.NotEmpty(t, text, "SubscribeErrorText should return non-empty text for code %v", code)
		}
	})

	t.Run("SessionErrorText returns non-empty for all defined codes", func(t *testing.T) {
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
			text := SessionErrorText(code)
			assert.NotEmpty(t, text, "SessionErrorText should return non-empty text for code %v", code)
		}
	})

	t.Run("GroupErrorText returns non-empty for all defined codes", func(t *testing.T) {
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
			text := GroupErrorText(code)
			assert.NotEmpty(t, text, "GroupErrorText should return non-empty text for code %v", code)
		}
	})
}
