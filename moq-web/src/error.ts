// =============================================================================
// Session Error Codes
// =============================================================================

import { StreamConnError, WebTransportStreamError } from "./internal/webtransport/error.ts";

/** Error codes for session-level termination as defined in MOQ Lite. */
export const SessionErrorCode = {
	/** No error occurred */
	NoError: 0x0,
	/** Internal error */
	InternalError: 0x1,
	/** Unauthorized */
	Unauthorized: 0x2,
	/** Protocol violation */
	ProtocolViolation: 0x3,
	/** Duplicate track alias */
	DuplicateTrackAlias: 0x4,
	/** Parameter length mismatch */
	ParameterLengthMismatch: 0x5,
	/** Too many subscribers */
	TooManySubscribers: 0x6,
	/** GOAWAY timeout */
	GoAwayTimeout: 0x10,
} as const;

/** Numeric session error code type. */
export type SessionErrorCode = number;

/** Error representing a session-level failure. */
export class SessionError extends StreamConnError {
	override get code(): SessionErrorCode {
		return super.code as SessionErrorCode;
	}

	static textOf(code: SessionErrorCode): string {
		switch (code) {
			case SessionErrorCode.NoError:
				return "no error";
			case SessionErrorCode.InternalError:
				return "internal error";
			case SessionErrorCode.Unauthorized:
				return "unauthorized";
			case SessionErrorCode.ProtocolViolation:
				return "protocol violation";
			case SessionErrorCode.DuplicateTrackAlias:
				return "duplicate track alias";
			case SessionErrorCode.ParameterLengthMismatch:
				return "parameter length mismatch";
			case SessionErrorCode.TooManySubscribers:
				return "too many subscribers";
			case SessionErrorCode.GoAwayTimeout:
				return "goaway timeout";
			default:
				return `unknown session error (${code})`;
		}
	}

	constructor(code: SessionErrorCode, remote: boolean) {
		super({ closeCode: code, reason: SessionError.textOf(code) }, remote);
		this.message = SessionError.textOf(code);
		this.name = "SessionError";
		Object.setPrototypeOf(this, SessionError.prototype);
	}
}

// =============================================================================
// Announce Error Codes
// =============================================================================

/** Error codes for announce stream failures. */
export const AnnounceErrorCode = {
	/** Internal error */
	InternalError: 0x00,
	/** Duplicated announcement */
	DuplicatedAnnounce: 0x01,
	/** Invalid announce status */
	InvalidAnnounceStatus: 0x02,
	/** Uninterested */
	Uninterested: 0x03,
	/** Banned prefix */
	BannedPrefix: 0x04,
	/** Invalid prefix */
	InvalidPrefix: 0x05,
} as const;

/** Numeric announce error code type. */
export type AnnounceErrorCode = number;

/** Error representing an announce stream failure. */
export class AnnounceError extends WebTransportStreamError {
	override get code(): AnnounceErrorCode {
		return super.code as AnnounceErrorCode;
	}

	static textOf(code: AnnounceErrorCode): string {
		switch (code) {
			case AnnounceErrorCode.InternalError:
				return "internal error";
			case AnnounceErrorCode.DuplicatedAnnounce:
				return "duplicated announce";
			case AnnounceErrorCode.InvalidAnnounceStatus:
				return "invalid announce status";
			case AnnounceErrorCode.Uninterested:
				return "uninterested";
			case AnnounceErrorCode.BannedPrefix:
				return "banned prefix";
			case AnnounceErrorCode.InvalidPrefix:
				return "invalid prefix";
			default:
				return `unknown announce error (${code})`;
		}
	}

	constructor(code: AnnounceErrorCode, remote: boolean) {
		super({ source: "stream", streamErrorCode: code }, remote);
		this.message = AnnounceError.textOf(code);
		this.name = "AnnounceError";
		Object.setPrototypeOf(this, AnnounceError.prototype);
	}
}

// =============================================================================
// Subscribe Error Codes
// =============================================================================

/** Error codes for subscribe stream failures. */
export const SubscribeErrorCode = {
	/** Internal error */
	InternalError: 0x00,
	/** Invalid range */
	InvalidRange: 0x01,
	/** Duplicate subscribe ID */
	DuplicateSubscribeID: 0x02,
	/** Track not found */
	TrackNotFound: 0x03,
	/** Unauthorized */
	Unauthorized: 0x04,
	/** Subscribe timeout */
	SubscribeTimeout: 0x05,
} as const;

/** Numeric subscribe error code type. */
export type SubscribeErrorCode = number;

/** Error representing a subscribe stream failure. */
export class SubscribeError extends WebTransportStreamError {
	override get code(): SubscribeErrorCode {
		return super.code as SubscribeErrorCode;
	}

	static textOf(code: SubscribeErrorCode): string {
		switch (code) {
			case SubscribeErrorCode.InternalError:
				return "internal error";
			case SubscribeErrorCode.InvalidRange:
				return "invalid range";
			case SubscribeErrorCode.DuplicateSubscribeID:
				return "duplicate subscribe id";
			case SubscribeErrorCode.TrackNotFound:
				return "track not found";
			case SubscribeErrorCode.Unauthorized:
				return "unauthorized";
			case SubscribeErrorCode.SubscribeTimeout:
				return "subscribe timeout";
			default:
				return `unknown subscribe error (${code})`;
		}
	}

	constructor(code: SubscribeErrorCode, remote: boolean) {
		super({ source: "stream", streamErrorCode: code }, remote);
		this.message = SubscribeError.textOf(code);
		this.name = "SubscribeError";
		Object.setPrototypeOf(this, SubscribeError.prototype);
	}
}

// =============================================================================
// Fetch Error Codes
// =============================================================================

/** Error codes for fetch stream failures. */
export const FetchErrorCode = {
	/** Internal error */
	InternalError: 0x00,
	/** Timeout */
	Timeout: 0x01,
} as const;

/** Numeric fetch error code type. */
export type FetchErrorCode = number;

/** Error representing a fetch stream failure. */
export class FetchError extends WebTransportStreamError {
	override get code(): FetchErrorCode {
		return super.code as FetchErrorCode;
	}

	static textOf(code: FetchErrorCode): string {
		switch (code) {
			case FetchErrorCode.InternalError:
				return "internal error";
			case FetchErrorCode.Timeout:
				return "timeout";
			default:
				return `unknown fetch error (${code})`;
		}
	}

	constructor(code: FetchErrorCode, remote: boolean) {
		super({ source: "stream", streamErrorCode: code }, remote);
		this.message = FetchError.textOf(code);
		this.name = "FetchError";
		Object.setPrototypeOf(this, FetchError.prototype);
	}
}

// =============================================================================
// Group Error Codes
// =============================================================================

/** Error codes for group stream failures. */
export const GroupErrorCode = {
	/** Internal error */
	InternalError: 0x00,
	/** Out of range */
	OutOfRange: 0x02,
	/** Expired group */
	ExpiredGroup: 0x03,
	/** Subscribe canceled */
	SubscribeCanceled: 0x04,
	/** Publish aborted */
	PublishAborted: 0x05,
	/** Closed session */
	ClosedSession: 0x06,
	/** Invalid subscribe ID */
	InvalidSubscribeID: 0x07,
} as const;

/** Numeric group error code type. */
export type GroupErrorCode = number;

/** Error representing a group stream failure. */
export class GroupError extends WebTransportStreamError {
	override get code(): GroupErrorCode {
		return super.code as GroupErrorCode;
	}

	static textOf(code: GroupErrorCode): string {
		switch (code) {
			case GroupErrorCode.InternalError:
				return "internal error";
			case GroupErrorCode.OutOfRange:
				return "out of range";
			case GroupErrorCode.ExpiredGroup:
				return "expired group";
			case GroupErrorCode.SubscribeCanceled:
				return "subscribe canceled";
			case GroupErrorCode.PublishAborted:
				return "publish aborted";
			case GroupErrorCode.ClosedSession:
				return "closed session";
			case GroupErrorCode.InvalidSubscribeID:
				return "invalid subscribe id";
			default:
				return `unknown group error (${code})`;
		}
	}

	constructor(code: GroupErrorCode, remote: boolean) {
		super({ source: "stream", streamErrorCode: code }, remote);
		this.message = GroupError.textOf(code);
		this.name = "GroupError";
		Object.setPrototypeOf(this, GroupError.prototype);
	}
}

// =============================================================================
// Probe Error Codes
// =============================================================================

/** Error codes for probe stream failures. Mirrors Go's `moqt.ProbeErrorCode`. */
export const ProbeErrorCode = {
	/** Internal error */
	Internal: 0x00,
	/** Timeout */
	Timeout: 0x01,
	/** Not supported */
	NotSupported: 0x02,
} as const;

/** Numeric probe error code type. */
export type ProbeErrorCode = number;

/** Error representing a probe stream failure. */
export class ProbeError extends WebTransportStreamError {
	override get code(): ProbeErrorCode {
		return super.code as ProbeErrorCode;
	}

	static textOf(code: ProbeErrorCode): string {
		switch (code) {
			case ProbeErrorCode.Internal:
				return "internal error";
			case ProbeErrorCode.Timeout:
				return "timeout";
			case ProbeErrorCode.NotSupported:
				return "not supported";
			default:
				return `unknown probe error (${code})`;
		}
	}

	constructor(code: ProbeErrorCode, remote: boolean) {
		super({ source: "stream", streamErrorCode: code }, remote);
		this.message = ProbeError.textOf(code);
		this.name = "ProbeError";
		Object.setPrototypeOf(this, ProbeError.prototype);
	}
}
