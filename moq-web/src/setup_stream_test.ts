import { assertEquals } from "@std/assert";
import { Session } from "./session.ts";
import { MockWebTransportSession } from "./session_test.ts";
import { ProbeLevels, SetupMessage, writeVarint } from "./internal/message/mod.ts";
import { UniStreamTypes } from "./stream_type.ts";
import { SessionErrorCode } from "./error.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";

// Build an incoming Setup Stream item: [SetupStreamType][SETUP body].
async function setupUniStream(setup: (sm: SetupMessage) => void): Promise<{
	type: number;
	data: Uint8Array;
}> {
	const sm = new SetupMessage({});
	setup(sm);
	const buf = Buffer.make(64);
	await writeVarint(buf, UniStreamTypes.SetupStreamType);
	await sm.encode(buf);
	return { type: UniStreamTypes.SetupStreamType, data: buf.bytes() };
}

Deno.test("Session SETUP handshake: peer Probe capability is recorded", async () => {
	const setup = await setupUniStream((sm) => sm.addProbe(ProbeLevels.Report));
	const mock = new MockWebTransportSession({ acceptUniStreamData: [setup] });
	const session = new Session({ transport: mock });
	await session.ready;

	// probe() awaits peer SETUP; with Report advertised it must NOT return the
	// not-supported error.
	const [, err] = await session.probe(1234);
	assertEquals(err, undefined);

	await session.close();
});

Deno.test("Session SETUP handshake: peer None capability blocks probe", async () => {
	const setup = await setupUniStream((sm) => sm.addProbe(ProbeLevels.None));
	const mock = new MockWebTransportSession({ acceptUniStreamData: [setup] });
	const session = new Session({ transport: mock });
	await session.ready;

	const [, err] = await session.probe(1234);
	assertEquals(err instanceof Error, true);
	assertEquals(err?.message.includes("does not support probing"), true);

	await session.close();
});

Deno.test("Session SETUP handshake: duplicate Setup Stream terminates the session", async () => {
	const one = await setupUniStream((sm) => sm.addProbe(ProbeLevels.Report));
	const mock = new MockWebTransportSession({ acceptUniStreamData: [one, one] });
	const session = new Session({ transport: mock });
	await session.ready;

	const info = await session.closed;
	assertEquals(info.code, SessionErrorCode.ProtocolViolation);
});

Deno.test("Session SETUP handshake: server-sent Path parameter terminates the session", async () => {
	const setup = await setupUniStream((sm) => sm.addPath("/forbidden"));
	const mock = new MockWebTransportSession({ acceptUniStreamData: [setup] });
	const session = new Session({ transport: mock });
	await session.ready;

	const info = await session.closed;
	assertEquals(info.code, SessionErrorCode.ProtocolViolation);
});
