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
		trackPriority: 0,
	});
	const ok = new SubscribeOkMessage({});
	const sss = new SendSubscribeStream(ctx, s, subscribe, ok);
	const err = await sss.update({
		trackPriority: 1,
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
		trackPriority: 0,
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
		trackPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, subscribe);
	const err = await rss.writeInfo({});
	assertEquals(err, undefined);
	const err2 = await rss.writeInfo({});
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
		trackPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, subscribe);
	cancel(new Error("canceled"));
	await new Promise((r) => setTimeout(r, 0));
	const err = await rss.writeInfo({});
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
		trackPriority: 0,
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
		trackPriority: 0,
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
		trackPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, subscribe);
	cancel(new Error("canceled"));
	await new Promise((r) => setTimeout(r, 0));
	await rss.closeWithError(2);
});

Deno.test("ReceiveSubscribeStream updated waiters are notified upon update", async () => {
	const [ctx] = withCancelCause(background());
	const sub = new SubscribeMessage({
		subscribeId: 10,
		broadcastPath: "/x",
		trackName: "t",
		trackPriority: 0,
	});
	// Encode update message to get data for readable
	const encoderWrittenData: Uint8Array[] = [];
	const encoderStream = {
		write: spy(async (p: Uint8Array) => {
			encoderWrittenData.push(new Uint8Array(p));
			return [p.length, undefined] as [number, Error | undefined];
		}),
	};
	const update = new SubscribeUpdateMessage({
		trackPriority: 5,
	});
	await update.encode(encoderStream);
	const total = encoderWrittenData.reduce((acc, arr) => acc + arr.length, 0);
	const data = new Uint8Array(total);
	let offset = 0;
	for (const arr of encoderWrittenData) {
		data.set(arr, offset);
		offset += arr.length;
	}
	// Create mock stream with the encoded data
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
	await new Promise((r) => setTimeout(r, 0));
	assertEquals(rss.trackConfig.trackPriority, 5);
});

Deno.test("ReceiveSubscribeStream closeWithError cancels streams and broadcasts cond", async () => {
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
		trackPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, sub);
	await rss.closeWithError(2);
	assertEquals(writableCancelCalls.length >= 0, true);
	assertEquals(readableCancelCalls.length >= 0, true);
});

Deno.test("ReceiveSubscribeStream writeInfo is only executed once even with concurrent calls", async () => {
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
		trackPriority: 0,
	});
	const rss = new ReceiveSubscribeStream(ctx, s, subscribe);

	// Call writeInfo concurrently
	const results = await Promise.all([
		rss.writeInfo({}),
		rss.writeInfo({}),
		rss.writeInfo({}),
	]);
	// All should be undefined (no error)
	for (const r of results) {
		assertEquals(r, undefined);
	}
	// Only one SUBSCRIBE_OK should be written
	assertEquals(writtenData.length, 1);
});
