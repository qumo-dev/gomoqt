import { assertEquals } from "@std/assert";
import {
	AnnounceError,
	AnnounceErrorCode,
	FetchError,
	FetchErrorCode,
	GroupError,
	GroupErrorCode,
	SessionError,
	SessionErrorCode,
	SubscribeError,
	SubscribeErrorCode,
} from "./error.ts";

Deno.test("Error", async (t) => {
	await t.step("SessionError textOf known codes", () => {
		assertEquals(SessionError.textOf(SessionErrorCode.NoError), "no error");
		assertEquals(
			SessionError.textOf(SessionErrorCode.InternalError),
			"internal error",
		);
		assertEquals(
			SessionError.textOf(SessionErrorCode.Unauthorized),
			"unauthorized",
		);
		assertEquals(
			SessionError.textOf(SessionErrorCode.ProtocolViolation),
			"protocol violation",
		);
		assertEquals(
			SessionError.textOf(SessionErrorCode.DuplicateTrackAlias),
			"duplicate track alias",
		);
		assertEquals(
			SessionError.textOf(SessionErrorCode.ParameterLengthMismatch),
			"parameter length mismatch",
		);
		assertEquals(
			SessionError.textOf(SessionErrorCode.TooManySubscribers),
			"too many subscribers",
		);
		assertEquals(
			SessionError.textOf(SessionErrorCode.GoAwayTimeout),
			"goaway timeout",
		);
		assertEquals(SessionError.textOf(9999), "unknown session error (9999)");
	});

	await t.step("AnnounceError textOf known codes", () => {
		assertEquals(
			AnnounceError.textOf(AnnounceErrorCode.InternalError),
			"internal error",
		);
		assertEquals(
			AnnounceError.textOf(AnnounceErrorCode.DuplicatedAnnounce),
			"duplicated announce",
		);
		assertEquals(
			AnnounceError.textOf(AnnounceErrorCode.InvalidAnnounceStatus),
			"invalid announce status",
		);
		assertEquals(
			AnnounceError.textOf(AnnounceErrorCode.Uninterested),
			"uninterested",
		);
		assertEquals(
			AnnounceError.textOf(AnnounceErrorCode.BannedPrefix),
			"banned prefix",
		);
		assertEquals(
			AnnounceError.textOf(AnnounceErrorCode.InvalidPrefix),
			"invalid prefix",
		);
		assertEquals(AnnounceError.textOf(9999), "unknown announce error (9999)");
	});

	await t.step("SubscribeError textOf known codes", () => {
		assertEquals(
			SubscribeError.textOf(SubscribeErrorCode.InternalError),
			"internal error",
		);
		assertEquals(
			SubscribeError.textOf(SubscribeErrorCode.InvalidRange),
			"invalid range",
		);
		assertEquals(
			SubscribeError.textOf(SubscribeErrorCode.DuplicateSubscribeID),
			"duplicate subscribe id",
		);
		assertEquals(
			SubscribeError.textOf(SubscribeErrorCode.TrackNotFound),
			"track not found",
		);
		assertEquals(
			SubscribeError.textOf(SubscribeErrorCode.Unauthorized),
			"unauthorized",
		);
		assertEquals(
			SubscribeError.textOf(SubscribeErrorCode.SubscribeTimeout),
			"subscribe timeout",
		);
		assertEquals(SubscribeError.textOf(9999), "unknown subscribe error (9999)");
	});

	await t.step("GroupError textOf known codes", () => {
		assertEquals(
			GroupError.textOf(GroupErrorCode.InternalError),
			"internal error",
		);
		assertEquals(GroupError.textOf(GroupErrorCode.OutOfRange), "out of range");
		assertEquals(
			GroupError.textOf(GroupErrorCode.ExpiredGroup),
			"expired group",
		);
		assertEquals(
			GroupError.textOf(GroupErrorCode.SubscribeCanceled),
			"subscribe canceled",
		);
		assertEquals(
			GroupError.textOf(GroupErrorCode.PublishAborted),
			"publish aborted",
		);
		assertEquals(
			GroupError.textOf(GroupErrorCode.ClosedSession),
			"closed session",
		);
		assertEquals(
			GroupError.textOf(GroupErrorCode.InvalidSubscribeID),
			"invalid subscribe id",
		);
		assertEquals(GroupError.textOf(9999), "unknown group error (9999)");
	});

	await t.step(
		"SessionError/AnnounceError constructors set proper fields",
		() => {
			const sErr = new SessionError(SessionErrorCode.Unauthorized, true);
			assertEquals(sErr.code, SessionErrorCode.Unauthorized);
			assertEquals(
				sErr.message,
				SessionError.textOf(SessionErrorCode.Unauthorized),
			);

			const aErr = new AnnounceError(AnnounceErrorCode.BannedPrefix, false);
			assertEquals(aErr.code, AnnounceErrorCode.BannedPrefix);
			assertEquals(
				aErr.message,
				AnnounceError.textOf(AnnounceErrorCode.BannedPrefix),
			);
		},
	);

	await t.step("SubscribeError constructor sets proper fields", () => {
		const subErr = new SubscribeError(SubscribeErrorCode.TrackNotFound, true);
		assertEquals(subErr.code, SubscribeErrorCode.TrackNotFound);
		assertEquals(
			subErr.message,
			SubscribeError.textOf(SubscribeErrorCode.TrackNotFound),
		);
	});

	await t.step("GroupError constructor sets proper fields", () => {
		const gErr = new GroupError(GroupErrorCode.ExpiredGroup, false);
		assertEquals(gErr.code, GroupErrorCode.ExpiredGroup);
		assertEquals(gErr.message, GroupError.textOf(GroupErrorCode.ExpiredGroup));
	});

	await t.step("FetchError textOf known codes", () => {
		assertEquals(
			FetchError.textOf(FetchErrorCode.InternalError),
			"internal error",
		);
		assertEquals(FetchError.textOf(FetchErrorCode.Timeout), "timeout");
		assertEquals(FetchError.textOf(9999), "unknown fetch error (9999)");
	});

	await t.step("FetchError constructor sets proper fields", () => {
		const fErr = new FetchError(FetchErrorCode.Timeout, true);
		assertEquals(fErr.code, FetchErrorCode.Timeout);
		assertEquals(fErr.message, FetchError.textOf(FetchErrorCode.Timeout));
		assertEquals(fErr.name, "FetchError");
	});

	await t.step("Error textOf returns default for unknown codes", () => {
		const code = 0xff;
		assertEquals(SessionError.textOf(code), `unknown session error (${code})`);
		assertEquals(
			AnnounceError.textOf(code),
			`unknown announce error (${code})`,
		);
		assertEquals(
			SubscribeError.textOf(code),
			`unknown subscribe error (${code})`,
		);
		assertEquals(GroupError.textOf(code), `unknown group error (${code})`);
		assertEquals(FetchError.textOf(code), `unknown fetch error (${code})`);
	});
});
