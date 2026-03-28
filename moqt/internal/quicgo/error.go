package quicgo

// func wrapError(err error) error {
// 	if err == nil {
// 		return nil
// 	}

// 	switch e := err.(type) {
// 	case *quicgo_quicgo.StreamError:
// 		return &transport.StreamError{
// 			StreamID:  transport.StreamID(e.StreamID),
// 			ErrorCode: transport.StreamErrorCode(e.ErrorCode),
// 			Remote:    e.Remote,
// 			Err:       e,
// 		}
// 	case *quicgo_quicgo.TransportError:
// 		return &quic.TransportError{
// 			Remote:       e.Remote,
// 			FrameType:    e.FrameType,
// 			ErrorCode:    quic.TransportErrorCode(e.ErrorCode),
// 			ErrorMessage: e.ErrorMessage,
// 			Err:          e,
// 		}
// 	case *quicgo_quicgo.ApplicationError:
// 		return &quic.ApplicationError{
// 			Remote:       e.Remote,
// 			ErrorCode:    quic.ApplicationErrorCode(e.ErrorCode),
// 			ErrorMessage: e.ErrorMessage,
// 			Err:          e,
// 		}
// 	case *quicgo_quicgo.VersionNegotiationError:
// 		ours := make([]quic.Version, len(e.Ours))
// 		for i, v := range e.Ours {
// 			ours[i] = quic.Version(v)
// 		}
// 		theirs := make([]quic.Version, len(e.Theirs))
// 		for i, v := range e.Theirs {
// 			theirs[i] = quic.Version(v)
// 		}
// 		return &quic.VersionNegotiationError{
// 			Ours:   ours,
// 			Theirs: theirs,
// 			Err:    e,
// 		}
// 	case *quicgo_quicgo.StatelessResetError:
// 		return &quic.StatelessResetError{
// 			Err: e,
// 		}

// 	case *quicgo_quicgo.IdleTimeoutError:
// 		return &quic.IdleTimeoutError{
// 			Err: e,
// 		}
// 	case *quicgo_quicgo.HandshakeTimeoutError:
// 		return &quic.HandshakeTimeoutError{
// 			Err: e,
// 		}
// 	default:
// 		// If the error is not recognized, return it as is
// 		return err
// 	}
// }

// func WrapTransportError(qerr quicgo_quicgo.TransportError) *quic.TransportError {
// 	return &quic.TransportError{
// 		Remote:       qerr.Remote,
// 		FrameType:    qerr.FrameType,
// 		ErrorCode:    quic.TransportErrorCode(qerr.ErrorCode),
// 		ErrorMessage: qerr.ErrorMessage,
// 	}
// }
