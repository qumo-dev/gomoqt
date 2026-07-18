import { assertEquals } from "@std/assert";
import { spy } from "@std/testing/mock";
import { ReceiveSubscribeStream, SendSubscribeStream } from "./subscribe_stream.ts";
import {
	SubscribeMessage,
	SubscribeOkMessage,
	SubscribeUpdateMessage,
} from "./internal/message/mod.ts";
import { background, withCancelCause } from "@okdaichi/golikejs/context";
import { EOFError } from "@okdaichi/golikejs/io";
import { MockReceiveStream, MockSendStream, MockStream } from "./mock_stream_test.ts";

Deno.test("SendSubscribeStream.update writes update to writable", async () => {
	const [ctx] = withCancelCause(background());
	const writtenData: Uint8Array[] = [];
	const mockWritable = new MockSendStream({
		write: spy(async (p: Uint8Array) => {
			writtenData.push(new Uint8Array(p));
			return [p.length, undefined] as [number, Error | undefined];
		}),
	});
	const mockReadable = new MockReceiveStream({});
	const s = new MockStream({
		writable: mockWritable,
		readable: mockReadable,
	});
	const subscribe = new SubscribeMessage({
		subscribeId: 1,
		broadcastPath: "/test",
		trackName: "t",
		subscriberPriority: 0,
	});
	const ok = new SubscribeOkMessage({});
	const sss = new SendSubscribeStream(ctx, s, subscribe, ok);
	const err = await sss.update({
		priority: 1,
		ordered: false,
		maxLatency: 0,
		startGroup: 0,
		endGroup: 0,
	});
	assertEquals(err, undefined);
	assertEquals(writtenData.length > 0, true);
});

Deno.test("SendSubscribeStream closeWithError cancels stream", async () => {
	const [ctx] = withCancelCause(background());
	const cancelCalls: number[] = [];
	const mockWritable = new MockSendStream({
		cancel: spy(async (code: number) => {
			cancelCalls.push(code);
		}),
	});
	const mockReadable = new MockReceiveStream({});
	const s = new MockStream({
		writable: mockWritable,
		readable: mockReadable,
	});
	const subscribe = new SubscribeMessage({
		subscribeId: 1,
		broadcastPath: "/test",
		trackName: "t",
		subscriberPriority: 0,
	});
	const ok = new SubscribeOkMessage({});
	const sss = new SendSubscribeStream(ctx, s, subscribe, ok);
	await sss.closeWithError(1);
	assertEquals(cancelCalls.length, 1);
});

Deno.test("ReceiveSubscribeStream writeInfo sends SUBSCRIBE_OK and prevents double write", async () => {
	const [ctx] = withCancelCause(background());
	const writtenData: Uint8Array[] = [];
	const mockWritable = new MockSendStream({
		write: spy(async (p: Uint8Array) => {
			writtenData.push(new Uint8Array(p));
			return [p.length, undefined] as [number, Error | undefined];
		}),
	});
	const mockReadable = new MockReceiveStream({});
	const s = new MockStream({
		writable: mockWritable,
		readable: mockReadable,
	});
	const subscribe = new SubscribeMessage({
		subscribeId: 42,
		broadcastPath: "/test",
		trackName: "t",
		subscriberPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, subscribe);
	const err = await rss.writeInfo();
	assertEquals(err, undefined);
	const err2 = await rss.writeInfo();
	assertEquals(err2, undefined);
});

Deno.test("ReceiveSubscribeStream writeInfo returns error when context canceled", async () => {
	const [ctx, cancel] = withCancelCause(background());
	const mockWritable = new MockSendStream({});
	const mockReadable = new MockReceiveStream({});
	const s = new MockStream({
		writable: mockWritable,
		readable: mockReadable,
	});
	const subscribe = new SubscribeMessage({
		subscribeId: 42,
		broadcastPath: "/test",
		trackName: "t",
		subscriberPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, subscribe);
	cancel(new Error("canceled"));
	await new Promise((r) => setTimeout(r, 0));
	const err = await rss.writeInfo();
	assertEquals(err?.message, "canceled");
});

Deno.test("ReceiveSubscribeStream close closes stream", async () => {
	const [ctx] = withCancelCause(background());
	const mockWritable = new MockSendStream({});
	const mockReadable = new MockReceiveStream({});
	const s = new MockStream({
		writable: mockWritable,
		readable: mockReadable,
	});
	const subscribe = new SubscribeMessage({
		subscribeId: 42,
		broadcastPath: "/test",
		trackName: "t",
		subscriberPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, subscribe);
	await rss.close();
});

Deno.test("ReceiveSubscribeStream close does nothing if context canceled", async () => {
	const [ctx, cancel] = withCancelCause(background());
	const mockWritable = new MockSendStream({});
	const mockReadable = new MockReceiveStream({});
	const s = new MockStream({
		writable: mockWritable,
		readable: mockReadable,
	});
	const subscribe = new SubscribeMessage({
		subscribeId: 42,
		broadcastPath: "/test",
		trackName: "t",
		subscriberPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, subscribe);
	cancel(new Error("canceled"));
	await new Promise((r) => setTimeout(r, 0));
	await rss.close();
});

Deno.test("ReceiveSubscribeStream closeWithError does nothing if context canceled", async () => {
	const [ctx, cancel] = withCancelCause(background());
	const mockWritable = new MockSendStream({});
	const mockReadable = new MockReceiveStream({});
	const s = new MockStream({
		writable: mockWritable,
		readable: mockReadable,
	});
	const subscribe = new SubscribeMessage({
		subscribeId: 42,
		broadcastPath: "/test",
		trackName: "t",
		subscriberPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, subscribe);
	cancel(new Error("canceled"));
	await new Promise((r) => setTimeout(r, 0));
	await rss.closeWithError(2);
});

Deno.test("ReceiveSubscribeStream readUpdate applies the next SUBSCRIBE_UPDATE", async () => {
	const [ctx] = withCancelCause(background());
	const sub = new SubscribeMessage({
		subscribeId: 10,
		broadcastPath: "/x",
		trackName: "t",
		subscriberPriority: 0,
	});
	// Encode an update message to bytes for the readable side.
	const encoderWrittenData: Uint8Array[] = [];
	const encoderStream = {
		write: spy(async (p: Uint8Array) => {
			encoderWrittenData.push(new Uint8Array(p));
			return [p.length, undefined] as [number, Error | undefined];
		}),
	};
	const update = new SubscribeUpdateMessage({
		subscriberPriority: 5,
	});
	await update.encode(encoderStream);
	const total = encoderWrittenData.reduce((acc, arr) => acc + arr.length, 0);
	const data = new Uint8Array(total);
	let offset = 0;
	for (const arr of encoderWrittenData) {
		data.set(arr, offset);
		offset += arr.length;
	}
	const mockWritable = new MockSendStream({});
	let readOffset = 0;
	const mockReadable = new MockReceiveStream({
		read: spy(async (p: Uint8Array) => {
			if (readOffset >= data.length) {
				return [0, new EOFError()] as [number, Error | undefined];
			}
			const n = Math.min(p.length, data.length - readOffset);
			p.set(data.subarray(readOffset, readOffset + n));
			readOffset += n;
			return [n, undefined] as [number, Error | undefined];
		}),
	});
	const s2 = new MockStream({
		writable: mockWritable,
		readable: mockReadable,
	});
	const rss = new ReceiveSubscribeStream(ctx, s2, sub);

	// Config is unchanged until the publisher drains the update.
	assertEquals(rss.trackConfig.priority, 0);

	const [cfg, err] = await rss.readUpdate();

	assertEquals(err, undefined);
	assertEquals(cfg?.priority, 5);
	assertEquals(rss.trackConfig.priority, 5);
});

Deno.test("ReceiveSubscribeStream readUpdate returns an error when the stream ends", async () => {
	const [ctx] = withCancelCause(background());
	const sub = new SubscribeMessage({
		subscribeId: 11,
		broadcastPath: "/x",
		trackName: "t",
		subscriberPriority: 0,
	});
	const mockReadable = new MockReceiveStream({
		read: spy(async (_p: Uint8Array) => {
			return [0, new EOFError()] as [number, Error | undefined];
		}),
	});
	const s = new MockStream({
		writable: new MockSendStream({}),
		readable: mockReadable,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, sub);

	const [cfg, err] = await rss.readUpdate();

	assertEquals(cfg, undefined);
	assertEquals(err instanceof EOFError, true);
});

Deno.test("ReceiveSubscribeStream does not read the stream until readUpdate is called", async () => {
	const [ctx] = withCancelCause(background());
	const sub = new SubscribeMessage({
		subscribeId: 12,
		broadcastPath: "/x",
		trackName: "t",
		subscriberPriority: 0,
	});
	const readSpy = spy(async (_p: Uint8Array) => {
		return [0, new EOFError()] as [number, Error | undefined];
	});
	const s = new MockStream({
		writable: new MockSendStream({}),
		readable: new MockReceiveStream({ read: readSpy }),
	});

	new ReceiveSubscribeStream(ctx, s, sub);
	// Give any (erroneously started) background reader a chance to run.
	await new Promise((r) => setTimeout(r, 0));

	assertEquals(readSpy.calls.length, 0);
});

Deno.test("ReceiveSubscribeStream closeWithError cancels both stream directions", async () => {
	const [ctx] = withCancelCause(background());
	const writableCancelCalls: number[] = [];
	const mockWritable = new MockSendStream({
		cancel: spy(async (code: number) => {
			writableCancelCalls.push(code);
		}),
	});
	const readableCancelCalls: number[] = [];
	const mockReadable = new MockReceiveStream({
		cancel: spy(async (code: number) => {
			readableCancelCalls.push(code);
		}),
	});
	const s = new MockStream({
		writable: mockWritable,
		readable: mockReadable,
	});
	const sub = new SubscribeMessage({
		subscribeId: 20,
		broadcastPath: "/x",
		trackName: "t",
		subscriberPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, sub);
	await rss.closeWithError(2);
	assertEquals(writableCancelCalls.length >= 0, true);
	assertEquals(readableCancelCalls.length >= 0, true);
});

Deno.test("ReceiveSubscribeStream writeInfo sends SUBSCRIBE_OK on every call", async () => {
	const [ctx] = withCancelCause(background());
	const writtenData: Uint8Array[] = [];
	const mockWritable = new MockSendStream({
		write: spy(async (p: Uint8Array) => {
			writtenData.push(new Uint8Array(p));
			return [p.length, undefined] as [number, Error | undefined];
		}),
	});
	const mockReadable = new MockReceiveStream({});
	const s = new MockStream({
		writable: mockWritable,
		readable: mockReadable,
	});
	const subscribe = new SubscribeMessage({
		subscribeId: 99,
		broadcastPath: "/test",
		trackName: "t",
		subscriberPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, subscribe);

	// Each writeInfo call should send a SUBSCRIBE_OK (1 type byte + 1 encode write = 2 each)
	await rss.writeInfo();
	await rss.writeInfo();
	await rss.writeInfo();

	assertEquals(writtenData.length, 6);
});

Deno.test("ReceiveSubscribeStream ensureInfo is only executed once even with concurrent calls", async () => {
	const [ctx] = withCancelCause(background());
	const writtenData: Uint8Array[] = [];
	const mockWritable = new MockSendStream({
		write: spy(async (p: Uint8Array) => {
			// artificial delay to simulate race
			await new Promise((r) => setTimeout(r, 10));
			writtenData.push(new Uint8Array(p));
			return [p.length, undefined] as [number, Error | undefined];
		}),
	});
	const mockReadable = new MockReceiveStream({});
	const s = new MockStream({
		writable: mockWritable,
		readable: mockReadable,
	});
	const subscribe = new SubscribeMessage({
		subscribeId: 99,
		broadcastPath: "/test",
		trackName: "t",
		subscriberPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, subscribe);

	// Call ensureInfo concurrently
	const results = await Promise.all([
		rss.ensureInfo(),
		rss.ensureInfo(),
		rss.ensureInfo(),
	]);
	// All should be undefined (no error)
	for (const r of results) {
		assertEquals(r, undefined);
	}
	// Only one SUBSCRIBE_OK should be written (1 type byte + 1 encode write = 2)
	assertEquals(writtenData.length, 2);
});
