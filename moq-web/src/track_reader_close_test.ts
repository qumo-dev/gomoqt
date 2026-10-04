import { assertEquals, assertExists } from "@std/assert";
import { background } from "@okdaichi/golikejs/context";
import { EOFError } from "@okdaichi/golikejs/io";
import { GroupErrorCode } from "./error.ts";
import { GroupMessage, SubscribeMessage, SubscribeOkMessage } from "./internal/message/mod.ts";
import { Queue } from "./internal/queue.ts";
import type { ReceiveStream, SendStream } from "./internal/webtransport/mod.ts";
import { MockStream } from "./mock_stream_test.ts";
import { SendSubscribeStream } from "./subscribe_stream.ts";
import { TrackReader } from "./track_reader.ts";

class FakeSendStream implements SendStream {
	closeCount = 0;
	cancelCodes: number[] = [];
	async write(p: Uint8Array): Promise<[number, Error | undefined]> {
		return [p.length, undefined];
	}
	async close(): Promise<void> {
		this.closeCount++;
	}
	async cancel(code: number): Promise<void> {
		this.cancelCodes.push(code);
	}
	closed(): Promise<void> {
		return new Promise(() => {});
	}
}

class FakeReceiveStream implements ReceiveStream {
	cancelCodes: number[] = [];
	async read(_p: Uint8Array): Promise<[number, Error | undefined]> {
		return [0, new EOFError()];
	}
	async cancel(code: number): Promise<void> {
		this.cancelCodes.push(code);
	}
	closed(): Promise<void> {
		return new Promise(() => {});
	}
}

function readerWithGroups(writable: FakeSendStream) {
	const stream = new MockStream({ writable });
	const subscribe = new SubscribeMessage({
		subscribeId: 3,
		broadcastPath: "/test",
		trackName: "video",
		subscriberPriority: 0,
	});
	const subscribeStream = new SendSubscribeStream(
		background(),
		stream,
		subscribe,
		new SubscribeOkMessage({}),
	);
	const queue = new Queue<[ReceiveStream, GroupMessage]>();
	const track = new TrackReader("/test", "video", subscribeStream, queue, () => queue.close());
	return { track, queue };
}

Deno.test("TrackReader.close ends subscription and cancels queued and accepted groups", async () => {
	const writable = new FakeSendStream();
	const { track, queue } = readerWithGroups(writable);
	const accepted = new FakeReceiveStream();
	const queued = new FakeReceiveStream();
	await queue.enqueue([accepted, new GroupMessage({ subscribeId: 3, sequence: 1 })]);
	await queue.enqueue([queued, new GroupMessage({ subscribeId: 3, sequence: 2 })]);
	const [group, err] = await track.acceptGroup(new Promise(() => {}));
	assertEquals(err, undefined);
	assertExists(group);

	await track.close();

	assertEquals(writable.closeCount, 1);
	assertEquals(writable.cancelCodes, []);
	assertEquals(accepted.cancelCodes, [GroupErrorCode.SubscribeCanceled]);
	assertEquals(queued.cancelCodes, [GroupErrorCode.SubscribeCanceled]);
	await track.context.done();
	assertEquals(await queue.drain(), []);
	const [next, nextErr] = await track.acceptGroup(new Promise(() => {}));
	assertEquals(next, undefined);
	assertExists(nextErr);
});

Deno.test("TrackReader.close wakes a waiting acceptGroup", async () => {
	const { track } = readerWithGroups(new FakeSendStream());
	const waiting = track.acceptGroup(new Promise(() => {}));

	await track.close();

	const [group, err] = await waiting;
	assertEquals(group, undefined);
	assertExists(err);
});

Deno.test("TrackReader.closeWithError resets subscription and cancels groups", async () => {
	const writable = new FakeSendStream();
	const { track, queue } = readerWithGroups(writable);
	const queued = new FakeReceiveStream();
	await queue.enqueue([queued, new GroupMessage({ subscribeId: 3, sequence: 1 })]);

	await track.closeWithError(7);

	assertEquals(writable.closeCount, 0);
	assertEquals(writable.cancelCodes, [7]);
	assertEquals(queued.cancelCodes, [7]);
	assertExists(track.context.err());
});
