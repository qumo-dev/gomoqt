import { assertEquals, assertExists } from "@std/assert";
import { TrackReader } from "./track_reader.ts";
import { Queue } from "./internal/queue.ts";
import { background, withCancelCause } from "@okdaichi/golikejs/context";
import { MockReceiveStream, MockStream } from "./mock_stream_test.ts";
import { SendSubscribeStream } from "./subscribe_stream.ts";
import { GroupMessage, SubscribeMessage, SubscribeOkMessage } from "./internal/message/mod.ts";

Deno.test("TrackReader", async (t) => {
	await t.step(
		"TrackReader.acceptGroup returns a group or error on empty dequeue",
		async () => {
			const [ctx] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 33,
				broadcastPath: "/test",
				trackName: "name",
				subscriberPriority: 1,
			});
			const ok = new SubscribeOkMessage({});
			const sss = new SendSubscribeStream(ctx, stream, subscribe, ok);
			// Use empty queue which causes acceptGroup to return error due to resolved signal
			const queue = new Queue<[any, any]>();
			const onClose = () => {};
			const tr = new TrackReader("/test", "name", sss, queue, onClose);
			const [grp, err] = await tr.acceptGroup(Promise.resolve());
			assertEquals(grp, undefined);
			assertEquals(err instanceof Error, true);
		},
	);

	await t.step(
		"TrackReader.update proxies to subscribeStream update and readInfo returns info",
		async () => {
			const [ctx] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 21,
				broadcastPath: "/test",
				trackName: "name",
				subscriberPriority: 0,
			});
			const ok = new SubscribeOkMessage({});
			const sss = new SendSubscribeStream(ctx, stream, subscribe, ok);
			const queue = new Queue<[any, any]>();
			const onClose = () => {};
			const tr = new TrackReader("/test", "name", sss, queue, onClose);
			const err = await tr.update({
				priority: 5,
				ordered: false,
				maxLatency: 0,
				startGroup: 0,
				endGroup: 0,
			});
			assertEquals(err, undefined);
			assertEquals(tr.readInfo(), sss.info);
		},
	);

	await t.step(
		"TrackReader.acceptGroup returns error when context is cancelled",
		async () => {
			const [ctx, cancel] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 44,
				broadcastPath: "/test",
				trackName: "name",
				subscriberPriority: 1,
			});
			const ok = new SubscribeOkMessage({});
			const sss = new SendSubscribeStream(ctx, stream, subscribe, ok);
			const queue = new Queue<[any, any]>();
			const onClose = () => {};
			const tr = new TrackReader("/test", "name", sss, queue, onClose);

			// Cancel context before calling acceptGroup
			cancel(new Error("context cancelled"));
			await new Promise((r) => setTimeout(r, 0));

			const [grp, err] = await tr.acceptGroup(new Promise(() => {}));
			assertEquals(grp, undefined);
			assertEquals(err instanceof Error, true);
		},
	);

	await t.step(
		"TrackReader.acceptGroup returns GroupReader when group is available",
		async () => {
			const [ctx] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 55,
				broadcastPath: "/test",
				trackName: "name",
				subscriberPriority: 1,
			});
			const ok = new SubscribeOkMessage({});
			const sss = new SendSubscribeStream(ctx, stream, subscribe, ok);
			const queue = new Queue<[any, any]>();
			const onClose = () => {};
			const tr = new TrackReader("/test", "name", sss, queue, onClose);

			// Add a group to the queue
			const mockReceiveStream = new MockReceiveStream({});
			const groupMsg = new GroupMessage({
				subscribeId: 55,
				sequence: 1,
			});
			queue.enqueue([mockReceiveStream, groupMsg]);

			const [grp, err] = await tr.acceptGroup(new Promise(() => {}));
			assertEquals(err, undefined);
			assertExists(grp);
		},
	);

	await t.step("TrackReader.closeWithError calls onCloseFunc", async () => {
		const [ctx] = withCancelCause(background());
		const stream = new MockStream({});
		const subscribe = new SubscribeMessage({
			subscribeId: 66,
			broadcastPath: "/test",
			trackName: "name",
			subscriberPriority: 1,
		});
		const ok = new SubscribeOkMessage({});
		const sss = new SendSubscribeStream(ctx, stream, subscribe, ok);
		const queue = new Queue<[any, any]>();
		let closeWithErrorCalled = false;
		const onClose = () => {
			closeWithErrorCalled = true;
		};
		const tr = new TrackReader("/test", "name", sss, queue, onClose);

		await tr.closeWithError(1);
		assertEquals(closeWithErrorCalled, true);
	});

	await t.step("TrackReader.trackConfig returns correct config", () => {
		const [ctx] = withCancelCause(background());
		const stream = new MockStream({});
		const subscribe = new SubscribeMessage({
			subscribeId: 77,
			broadcastPath: "/test",
			trackName: "name",
			subscriberPriority: 5,
		});
		const ok = new SubscribeOkMessage({});
		const sss = new SendSubscribeStream(ctx, stream, subscribe, ok);
		const queue = new Queue<[any, any]>();
		const tr = new TrackReader("/test", "name", sss, queue, () => {});

		const config = tr.trackConfig;
		assertEquals(config.priority, 5);
	});

	await t.step("TrackReader.context returns subscribe stream context", () => {
		const [ctx] = withCancelCause(background());
		const stream = new MockStream({});
		const subscribe = new SubscribeMessage({
			subscribeId: 88,
			broadcastPath: "/test",
			trackName: "name",
			subscriberPriority: 1,
		});
		const ok = new SubscribeOkMessage({});
		const sss = new SendSubscribeStream(ctx, stream, subscribe, ok);
		const queue = new Queue<[any, any]>();
		const tr = new TrackReader("/test", "name", sss, queue, () => {});

		assertExists(tr.context);
		assertEquals(tr.context.err(), undefined);
	});

	await t.step("TrackReader.close calls onCloseFunc", async () => {
		const [ctx] = withCancelCause(background());
		const stream = new MockStream({});
		const subscribe = new SubscribeMessage({
			subscribeId: 90,
			broadcastPath: "/test",
			trackName: "name",
			subscriberPriority: 1,
		});
		const ok = new SubscribeOkMessage({});
		const sss = new SendSubscribeStream(ctx, stream, subscribe, ok);
		const queue = new Queue<[any, any]>();
		let closeCalled = false;
		const tr = new TrackReader("/test", "name", sss, queue, () => {
			closeCalled = true;
		});

		await tr.close();
		assertEquals(closeCalled, true);
	});

	await t.step("TrackReader.subscribeId returns subscribe stream ID", () => {
		const [ctx] = withCancelCause(background());
		const stream = new MockStream({});
		const subscribe = new SubscribeMessage({
			subscribeId: 42,
			broadcastPath: "/test",
			trackName: "name",
			subscriberPriority: 1,
		});
		const ok = new SubscribeOkMessage({});
		const sss = new SendSubscribeStream(ctx, stream, subscribe, ok);
		const queue = new Queue<[any, any]>();
		const tr = new TrackReader("/test", "name", sss, queue, () => {});

		assertEquals(tr.subscribeId, 42);
	});

	await t.step(
		"TrackReader.drops yields drops and exits on context cancel",
		async () => {
			const [ctx, cancel] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 100,
				broadcastPath: "/test",
				trackName: "name",
				subscriberPriority: 1,
			});
			const ok = new SubscribeOkMessage({});
			const sss = new SendSubscribeStream(ctx, stream, subscribe, ok);
			const queue = new Queue<[any, any]>();
			const tr = new TrackReader("/test", "name", sss, queue, () => {});

			// Append a drop before iterating
			sss.appendDrop({ startGroup: 1, endGroup: 5, errorCode: 0x03 });

			const collected: Array<{ startGroup: number; endGroup: number; errorCode: number }> = [];

			// Cancel context after first yield to exit the generator
			const signal = new Promise<void>(() => {});
			for await (const drop of tr.drops(signal)) {
				collected.push(drop);
				// Cancel context to stop the generator
				cancel(new Error("done"));
				break;
			}

			assertEquals(collected.length, 1);
			assertEquals(collected[0]!.startGroup, 1);
			assertEquals(collected[0]!.endGroup, 5);
			assertEquals(collected[0]!.errorCode, 0x03);
		},
	);
});
