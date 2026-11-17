// =============================================================================
// Session Error Codes
// =============================================================================

import { StreamConnError, WebTransportStreamError } from "./internal/webtransport/error.ts";

/**
 * Error codes for session-level termination (moq-lite-06 §4.4.1). The session
 * and stream code spaces are independent: the same number means different
 * things in each. Codes from 0x40 up are the application's.
 */
export const SessionErrorCode = {
	/** No error occurred */
	NoError: 0x0,
	/** Internal error */
	InternalError: 0x1,
	/** Unauthorized */
	Unauthorized: 0x2,
	/** Protocol violation */
	ProtocolViolation: 0x3,
	/** A key-value pair was malformed, or repeated more than allowed */
	KeyValueFormattingError: 0x6,
	/** GOAWAY timeout */
	GoAwayTimeout: 0x10,
	/** The peer took too long to respond to a control message */
	ControlMessageTimeout: 0x11,
	/** No version could be negotiated */
	VersionNegotiationFailed: 0x15,
	/**
	 * The peer does not verify tokens on an Auth Stream. The Auth Stream is a
	 * gomoqt extension, so the code sits in the application range.
	 */
	NotSupported: 0x40,
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
			case SessionErrorCode.KeyValueFormattingError:
				return "key-value formatting error";
			case SessionErrorCode.GoAwayTimeout:
				return "goaway timeout";
			case SessionErrorCode.ControlMessageTimeout:
				return "control message timeout";
			case SessionErrorCode.VersionNegotiationFailed:
				return "version negotiation failed";
			case SessionErrorCode.NotSupported:
				return "not supported";
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
// Stream Error Codes (moq-lite-06 §4.4.2)
// =============================================================================

/**
 * The one stream error code space. The typed tables below
 * ({@link AnnounceErrorCode}, {@link SubscribeErrorCode}, ...) are views of it:
 * where two kinds share a meaning they share the value. Codes 0x30-0x39 are
 * moq-lite's own; codes from 0x40 up are the application's, and gomoqt assigns
 * the few it needs there (0x40-0x45), which a non-gomoqt peer treats as
 * unspecified errors.
 */
const StreamCode = {
	InternalError: 0x0,
	Cancelled: 0x1,
	DeliveryTimeout: 0x2,
	SessionClosed: 0x3,
	GoingAway: 0x4,
	TooFarBehind: 0x5,
	MalformedTrack: 0x12,
	NoCapacity: 0x30,
	ControlTimeout: 0x31,
	GroupTooLarge: 0x32,
	NotFound: 0x33,
	Old: 0x34,
	Evicted: 0x35,
	Unroutable: 0x36,
	WrongSize: 0x37,
	FrameTooLarge: 0x38,
	TimestampMismatch: 0x39,
	// gomoqt, application range
	Unauthorized: 0x40,
	ProtocolViolation: 0x41,
	DuplicateID: 0x42,
	InvalidPrefix: 0x43,
	BannedPrefix: 0x44,
	NotSupported: 0x45,
} as const;

const streamCodeText: ReadonlyMap<number, string> = new Map<number, string>([
	[StreamCode.InternalError, "internal error"],
	[StreamCode.Cancelled, "cancelled"],
	[StreamCode.DeliveryTimeout, "delivery timeout"],
	[StreamCode.SessionClosed, "session closed"],
	[StreamCode.GoingAway, "going away"],
	[StreamCode.TooFarBehind, "too far behind"],
	[StreamCode.MalformedTrack, "malformed track"],
	[StreamCode.NoCapacity, "no capacity"],
	[StreamCode.ControlTimeout, "control timeout"],
	[StreamCode.GroupTooLarge, "group too large"],
	[StreamCode.NotFound, "not found"],
	[StreamCode.Old, "old group"],
	[StreamCode.Evicted, "evicted"],
	[StreamCode.Unroutable, "unroutable"],
	[StreamCode.WrongSize, "wrong size"],
	[StreamCode.FrameTooLarge, "frame too large"],
	[StreamCode.TimestampMismatch, "timestamp mismatch"],
	[StreamCode.Unauthorized, "unauthorized"],
	[StreamCode.ProtocolViolation, "protocol violation"],
	[StreamCode.DuplicateID, "duplicate id"],
	[StreamCode.InvalidPrefix, "invalid prefix"],
	[StreamCode.BannedPrefix, "banned prefix"],
	[StreamCode.NotSupported, "not supported"],
]);

/** Text for a stream code, with kind-specific overrides first. */
function streamText(
	code: number,
	kind: string,
	overrides: ReadonlyMap<number, string>,
): string {
	return overrides.get(code) ?? streamCodeText.get(code) ?? `unknown ${kind} error (${code})`;
}

// =============================================================================
// Announce Error Codes
// =============================================================================

/** Error codes for announce stream failures. */
export const AnnounceErrorCode = {
	/** Internal error */
	InternalError: StreamCode.InternalError,
	/** The subscriber no longer wants the stream: moq-lite's CANCELLED */
	Uninterested: StreamCode.Cancelled,
	/** Banned prefix (gomoqt) */
	BannedPrefix: StreamCode.BannedPrefix,
	/** Invalid prefix (gomoqt) */
	InvalidPrefix: StreamCode.InvalidPrefix,
	/** The prefix lies outside the root of every grant in force (gomoqt) */
	Unauthorized: StreamCode.Unauthorized,
} as const;

/** Numeric announce error code type. */
export type AnnounceErrorCode = number;

/** Error representing an announce stream failure. */
export class AnnounceError extends WebTransportStreamError {
	override get code(): AnnounceErrorCode {
		return super.code as AnnounceErrorCode;
	}

	static textOf(code: AnnounceErrorCode): string {
		return streamText(
			code,
			"announce",
			new Map<number, string>([[AnnounceErrorCode.Uninterested, "uninterested"]]),
		);
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
	InternalError: StreamCode.InternalError,
	/** A routine cancellation, such as giving up on an unanswered request */
	Canceled: StreamCode.Cancelled,
	/** Track not found: moq-lite's NOT_FOUND */
	TrackNotFound: StreamCode.NotFound,
	/** The only source is excluded by the subscriber's Hop ID: UNROUTABLE */
	Unroutable: StreamCode.Unroutable,
	/** The peer never answered: moq-lite's CONTROL_TIMEOUT */
	SubscribeTimeout: StreamCode.ControlTimeout,
	/** Unauthorized (gomoqt) */
	Unauthorized: StreamCode.Unauthorized,
	/** A range breaking a moq-lite rule, a protocol violation (gomoqt) */
	InvalidRange: StreamCode.ProtocolViolation,
	/** Duplicate subscribe ID (gomoqt) */
	DuplicateSubscribeID: StreamCode.DuplicateID,
} as const;

/** Numeric subscribe error code type. */
export type SubscribeErrorCode = number;

/** Error representing a subscribe stream failure. */
export class SubscribeError extends WebTransportStreamError {
	override get code(): SubscribeErrorCode {
		return super.code as SubscribeErrorCode;
	}

	static textOf(code: SubscribeErrorCode): string {
		return streamText(
			code,
			"subscribe",
			new Map<number, string>([
				[SubscribeErrorCode.TrackNotFound, "track not found"],
				[SubscribeErrorCode.InvalidRange, "invalid range"],
				[SubscribeErrorCode.SubscribeTimeout, "subscribe timeout"],
				[SubscribeErrorCode.DuplicateSubscribeID, "duplicate subscribe id"],
			]),
		);
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
	InternalError: StreamCode.InternalError,
	/** The peer never answered: moq-lite's CONTROL_TIMEOUT */
	Timeout: StreamCode.ControlTimeout,
} as const;

/** Numeric fetch error code type. */
export type FetchErrorCode = number;

/** Error representing a fetch stream failure. */
export class FetchError extends WebTransportStreamError {
	override get code(): FetchErrorCode {
		return super.code as FetchErrorCode;
	}

	static textOf(code: FetchErrorCode): string {
		return streamText(
			code,
			"fetch",
			new Map<number, string>([[FetchErrorCode.Timeout, "timeout"]]),
		);
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
	InternalError: StreamCode.InternalError,
	/** The group is no longer wanted, by either end: CANCELLED */
	SubscribeCanceled: StreamCode.Cancelled,
	/** The group aged out of the subscriber's Max Age: DELIVERY_TIMEOUT */
	ExpiredGroup: StreamCode.DeliveryTimeout,
	/** The session is closing: SESSION_CLOSED */
	ClosedSession: StreamCode.SessionClosed,
	/** The group grew past the publisher's cache budget */
	GroupTooLarge: StreamCode.GroupTooLarge,
	/** The group was never here */
	NotFound: StreamCode.NotFound,
	/** The group was superseded by a newer one and dropped */
	Old: StreamCode.Old,
	/** The group was dropped under memory pressure */
	Evicted: StreamCode.Evicted,
	/** A frame declared a payload larger than the receiver accepts */
	FrameTooLarge: StreamCode.FrameTooLarge,
	/** A fetch frame range breaking a moq-lite rule (gomoqt) */
	InvalidRange: StreamCode.ProtocolViolation,
	/** The session's scope does not cover the group, as for a refused fetch (gomoqt) */
	Unauthorized: StreamCode.Unauthorized,
} as const;

/** Numeric group error code type. */
export type GroupErrorCode = number;

/** Error representing a group stream failure. */
export class GroupError extends WebTransportStreamError {
	override get code(): GroupErrorCode {
		return super.code as GroupErrorCode;
	}

	static textOf(code: GroupErrorCode): string {
		return streamText(
			code,
			"group",
			new Map<number, string>([
				[GroupErrorCode.SubscribeCanceled, "subscribe canceled"],
				[GroupErrorCode.ExpiredGroup, "expired group"],
				[GroupErrorCode.ClosedSession, "closed session"],
				[GroupErrorCode.InvalidRange, "invalid range"],
			]),
		);
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
	Internal: StreamCode.InternalError,
	/** The peer never answered: moq-lite's CONTROL_TIMEOUT */
	Timeout: StreamCode.ControlTimeout,
	/** A Probe Stream at Probe level None (gomoqt) */
	NotSupported: StreamCode.NotSupported,
} as const;

/** Numeric probe error code type. */
export type ProbeErrorCode = number;

/** Error representing a probe stream failure. */
export class ProbeError extends WebTransportStreamError {
	override get code(): ProbeErrorCode {
		return super.code as ProbeErrorCode;
	}

	static textOf(code: ProbeErrorCode): string {
		return streamText(
			code,
			"probe",
			new Map<number, string>([[ProbeErrorCode.Timeout, "timeout"]]),
		);
	}

	constructor(code: ProbeErrorCode, remote: boolean) {
		super({ source: "stream", streamErrorCode: code }, remote);
		this.message = ProbeError.textOf(code);
		this.name = "ProbeError";
		Object.setPrototypeOf(this, ProbeError.prototype);
	}
}

// =============================================================================
// Auth Stream errors
// =============================================================================

/** A token refused, or a grant revoked, by the peer (AUTH_ERROR). */
export class AuthError extends Error {
	/** A session error code, such as {@link SessionErrorCode.Unauthorized}. */
	readonly code: SessionErrorCode;
	/** The peer's reason phrase. */
	readonly reason: string;

	constructor(code: SessionErrorCode, reason: string) {
		const text = SessionError.textOf(code);
		super(reason === "" ? text : `${text}: ${reason}`);
		this.code = code;
		this.reason = reason;
		this.name = "AuthError";
		Object.setPrototypeOf(this, AuthError.prototype);
	}
}

/** The peer reset the Auth Stream before answering, as a peer without it does. */
export class AuthNotSupportedError extends Error {
	constructor() {
		super("moq: peer does not support the auth stream");
		this.name = "AuthNotSupportedError";
		Object.setPrototypeOf(this, AuthNotSupportedError.prototype);
	}
}

// =============================================================================
// Redirects (SUBSCRIBE_REDIRECT, a gomoqt extension)
// =============================================================================

/** Where a publisher sent a subscription instead of serving it. */
export interface Redirect {
	/** The session to subscribe on, such as `"moqt://relay-b.example/"`. Empty means the current one. */
	uri: string;
	/** The broadcast to subscribe to instead. */
	broadcastPath: string;
	/** The track to subscribe to instead. */
	trackName: string;
}

/**
 * Returned by {@link Session.subscribe} when the publisher redirected the
 * subscription somewhere the session does not go by itself: to another
 * session, which the caller connects to, or past the redirects it follows.
 * `cause` says why it was not followed, when a reason other than another
 * session applies: a {@link TooManyRedirectsError}, or the error
 * `checkRedirect` returned.
 */
export class RedirectError extends Error {
	/** Where the subscription was redirected. */
	readonly redirect: Redirect;

	constructor(redirect: Redirect, cause?: Error) {
		let where = `${redirect.broadcastPath} ${redirect.trackName}`;
		if (redirect.uri !== "") {
			where = `${redirect.uri} ${where}`;
		}
		const text = `moq: subscription redirected to ${where}`;
		super(cause ? `${text}: ${cause.message}` : text, cause ? { cause } : undefined);
		this.redirect = redirect;
		this.name = "RedirectError";
		Object.setPrototypeOf(this, RedirectError.prototype);
	}
}

/** The cause of a {@link RedirectError} when a subscription was redirected more than the session follows. */
export class TooManyRedirectsError extends Error {
	constructor() {
		super("moq: too many redirects");
		this.name = "TooManyRedirectsError";
		Object.setPrototypeOf(this, TooManyRedirectsError.prototype);
	}
}

/**
 * Returned by a `checkRedirect` option to make {@link Session.subscribe}
 * return the redirect as a {@link RedirectError} without following it.
 */
export const StopRedirect: Error = new Error("moq: redirect not followed");

/**
 * Returned by {@link TrackWriter.redirect} when the subscriber did not
 * advertise that it follows redirects. The subscription is left as it was, so
 * the publisher may serve it or refuse it instead.
 */
export class RedirectNotSupportedError extends Error {
	constructor(detail?: string) {
		super(
			detail
				? `moq: peer does not follow redirects: ${detail}`
				: "moq: peer does not follow redirects",
		);
		this.name = "RedirectNotSupportedError";
		Object.setPrototypeOf(this, RedirectNotSupportedError.prototype);
	}
}

/**
 * Returned by {@link TrackWriter.redirect} when the subscription was already
 * answered: SUBSCRIBE_OK, SUBSCRIBE_END or a redirect went out first.
 */
export class SubscriptionAnsweredError extends Error {
	constructor() {
		super("moq: subscription already answered");
		this.name = "SubscriptionAnsweredError";
		Object.setPrototypeOf(this, SubscriptionAnsweredError.prototype);
	}
}

/** Returned for a broadcast path that does not start with "/". */
export class InvalidBroadcastPathError extends Error {
	/** The path that was refused. */
	readonly path: string;

	constructor(path: string) {
		super(`moq: invalid broadcast path: ${JSON.stringify(path)}`);
		this.path = path;
		this.name = "InvalidBroadcastPathError";
		Object.setPrototypeOf(this, InvalidBroadcastPathError.prototype);
	}
}
