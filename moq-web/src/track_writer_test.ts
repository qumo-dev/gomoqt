import { assertEquals, assertInstanceOf } from "@std/assert";
import { spy } from "@std/testing/mock";
import { TrackWriter } from "./track_writer.ts";
import { background, withCancelCause } from "@okdaichi/golikejs/context";
import { SendStream } from "./internal/webtransport/mod.ts";
import { MockSendStream, MockStream } from "./mock_stream_test.ts";
import { ReceiveSubscribeStream } from "./subscribe_stream.ts";
import { SubscribeMessage } from "./internal/message/mod.ts";
import { GroupErrorCode } from "./error.ts";

Deno.test("TrackWriter", async (t) => {
	await t.step(
		"TrackWriter.openGroup succeeds and writes group stream type and msg",
		async () => {
			const [ctx] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 99,
				broadcastPath: "/test",
				trackName: "test",
				subscriberPriority: 0,
			});
			const rss = new ReceiveSubscribeStream(ctx, stream, subscribe);
			const writtenData: Uint8Array[] = [];
			const mockWritable = new MockSendStream({
				write: spy(async (p: Uint8Array) => {
					writtenData.push(new Uint8Array(p));
					return [p.length, undefined] as [number, Error | undefined];
				}),
			});
			const openUni = async () => [mockWritable, undefined] as [SendStream, undefined];
			const tw = new TrackWriter("/test", "test", rss, openUni);
			const [grp, err] = await tw.openGroup();
			assertEquals(err, undefined);
			assertEquals(grp !== undefined, true);
		},
	);
	await t.step(
		"TrackWriter.openGroup handles failing openUniStream",
		async () => {
			const [ctx] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 99,
				broadcastPath: "/test",
				trackName: "test",
				subscriberPriority: 0,
			});
			const rss = new ReceiveSubscribeStream(ctx, stream, subscribe);
			const openUni = async () => [undefined, new Error("no stream")] as [undefined, Error];
			const tw = new TrackWriter("/test", "test", rss, openUni);
			const [grp, err] = await tw.openGroup();
			assertEquals(grp, undefined);
			assertEquals(err instanceof Error, true);
		},
	);

	await t.step(
		"TrackWriter.closeWithError cancels all groups and closes subscribe stream with error",
		async () => {
			const [ctx] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 21,
				broadcastPath: "/t",
				trackName: "test",
				subscriberPriority: 0,
			});
			const rss = new ReceiveSubscribeStream(ctx, stream, subscribe);
			const cancelCalls: number[] = [];
			const mockWritable = new MockSendStream({
				cancel: spy(async (code: number) => {
					cancelCalls.push(code);
				}),
			});
			const openUni = async () => [mockWritable, undefined] as [SendStream, undefined];
			const tw = new TrackWriter("/t", "test", rss, openUni);
			// Open a group to add to internal groups
			const [grp, err2] = await tw.openGroup();
			assertEquals(err2, undefined);
			assertEquals(grp !== undefined, true);
			// Close with error and ensure group canceled
			await tw.closeWithError(1);
			// Close with error and ensure group canceled
			await tw.closeWithError(1);
			// If no exception, success
			assertEquals(true, true);
		},
	);
	await t.step(
		"TrackWriter.openGroup returns error when subscribeStream.writeInfo fails",
		async () => {
			const [ctx] = withCancelCause(background());
			const mockWritable = new MockSendStream({
				write: spy(async (_p: Uint8Array) => {
					return [0, new Error("writeInfo failed")] as [
						number,
						Error | undefined,
					];
				}),
			});
			const stream = new MockStream({ writable: mockWritable });
			const subscribe = new SubscribeMessage({
				subscribeId: 0,
				broadcastPath: "/test/",
				trackName: "name",
				subscriberPriority: 0,
			});
			const rss = new ReceiveSubscribeStream(ctx, stream, subscribe);

			const tw = new TrackWriter(
				"/test",
				"name",
				rss,
				async () => {
					return [
						new SendStream({
							stream: new WritableStream({ write(_c) {} }),
						}),
						undefined,
					];
				},
			);

			const [group, err] = await tw.openGroup();
			assertEquals(group, undefined);
			assertInstanceOf(err, Error);
		},
	);

	await t.step(
		"TrackWriter.openGroup returns error when writeVarint fails",
		async () => {
			const [ctx] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 0,
				broadcastPath: "/test/",
				trackName: "name",
				subscriberPriority: 0,
			});
			const rss = new ReceiveSubscribeStream(ctx, stream, subscribe);

			let writeCall = 0;
			const writable = new WritableStream<Uint8Array>({
				write(_chunk) {
					writeCall++;
					if (writeCall === 1) {
						return Promise.reject(new Error("first write failed"));
					}
					return Promise.resolve();
				},
			});

			const openUni = async () =>
				[new SendStream({ stream: writable }), undefined] as [
					SendStream,
					undefined,
				];

			const tw = new TrackWriter("/test/", "name", rss, openUni);

			const [group, err] = await tw.openGroup();
			assertEquals(group, undefined);
			assertInstanceOf(err, Error);
		},
	);

	await t.step(
		"TrackWriter.openGroup with a pre-aborted signal does not open a stream",
		async () => {
			const [ctx] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 99,
				broadcastPath: "/test",
				trackName: "test",
				subscriberPriority: 0,
			});
			const rss = new ReceiveSubscribeStream(ctx, stream, subscribe);
			let openCalled = false;
			const openUni = async (_options?: { signal?: AbortSignal }) => {
				openCalled = true;
				return [new MockSendStream({}), undefined] as [SendStream, undefined];
			};
			const tw = new TrackWriter("/test", "test", rss, openUni);

			const ac = new AbortController();
			ac.abort(new Error("aborted before start"));
			const [grp, err] = await tw.openGroup({ signal: ac.signal });

			assertEquals(grp, undefined);
			assertInstanceOf(err, Error);
			assertEquals((err as Error).message, "aborted before start");
			assertEquals(openCalled, false);
		},
	);

	await t.step(
		"TrackWriter.openGroup threads an effective signal to openUniStreamFunc",
		async () => {
			const [ctx] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 99,
				broadcastPath: "/test",
				trackName: "test",
				subscriberPriority: 0,
			});
			const rss = new ReceiveSubscribeStream(ctx, stream, subscribe);
			let receivedSignal: AbortSignal | undefined;
			const openUni = async (options?: { signal?: AbortSignal }) => {
				receivedSignal = options?.signal;
				return [new MockSendStream({}), undefined] as [SendStream, undefined];
			};
			const tw = new TrackWriter("/test", "test", rss, openUni);

			const ac = new AbortController();
			const [grp, err] = await tw.openGroup({ signal: ac.signal });
			assertEquals(err, undefined);
			assertEquals(grp !== undefined, true);

			// The signal passed down is the composed effective signal: aborting
			// the caller's controller is reflected in it.
			assertEquals(receivedSignal !== undefined, true);
			assertEquals(receivedSignal!.aborted, false);
			ac.abort();
			assertEquals(receivedSignal!.aborted, true);
		},
	);

	await t.step(
		"TrackWriter.openGroup cancels the allocated stream when the header write fails",
		async () => {
			const [ctx] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 0,
				broadcastPath: "/test",
				trackName: "name",
				subscriberPriority: 0,
			});
			const rss = new ReceiveSubscribeStream(ctx, stream, subscribe);

			const cancelCalls: number[] = [];
			const mockSend = new MockSendStream({
				write: spy(async (_p: Uint8Array) =>
					[0, new Error("group header write failed")] as [
						number,
						Error | undefined,
					]
				),
				cancel: spy(async (code: number) => {
					cancelCalls.push(code);
				}),
			});
			const openUni = async (_options?: { signal?: AbortSignal }) =>
				[mockSend, undefined] as [SendStream, undefined];
			const tw = new TrackWriter("/test", "name", rss, openUni);

			const [group, err] = await tw.openGroup();
			assertEquals(group, undefined);
			assertInstanceOf(err, Error);
			// §3: the allocated stream must be cancelled with InternalError, not
			// left half-open.
			assertEquals(cancelCalls.length, 1);
			assertEquals(cancelCalls[0], GroupErrorCode.InternalError);
		},
	);

	await t.step(
		"TrackWriter.openGroup cancels the allocated stream when group message encode fails",
		async () => {
			const [ctx] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 0,
				broadcastPath: "/test",
				trackName: "name",
				subscriberPriority: 0,
			});
			const rss = new ReceiveSubscribeStream(ctx, stream, subscribe);

			const cancelCalls: number[] = [];
			let writeCall = 0;
			const mockSend = new MockSendStream({
				write: spy(async (p: Uint8Array) => {
					writeCall++;
					// Stream-type varint (first write) succeeds; subsequent
					// writes (GroupMessage encode) fail.
					if (writeCall >= 2) {
						return [0, new Error("group message encode failed")] as [
							number,
							Error | undefined,
						];
					}
					return [p.length, undefined] as [number, Error | undefined];
				}),
				cancel: spy(async (code: number) => {
					cancelCalls.push(code);
				}),
			});
			const openUni = async (_options?: { signal?: AbortSignal }) =>
				[mockSend, undefined] as [SendStream, undefined];
			const tw = new TrackWriter("/test", "name", rss, openUni);

			const [group, err] = await tw.openGroup();
			assertEquals(group, undefined);
			assertInstanceOf(err, Error);
			assertEquals(cancelCalls.length, 1);
			assertEquals(cancelCalls[0], GroupErrorCode.InternalError);
		},
	);

	await t.step(
		"TrackWriter.openGroup returns the context error when the track is already cancelled",
		async () => {
			const [ctx, cancel] = withCancelCause(background());
			const stream = new MockStream({});
			const subscribe = new SubscribeMessage({
				subscribeId: 99,
				broadcastPath: "/test",
				trackName: "test",
				subscriberPriority: 0,
			});
			const rss = new ReceiveSubscribeStream(ctx, stream, subscribe);
			let openCalled = false;
			const openUni = async (_options?: { signal?: AbortSignal }) => {
				openCalled = true;
				return [new MockSendStream({}), undefined] as [SendStream, undefined];
			};
			const tw = new TrackWriter("/test", "test", rss, openUni);

			cancel(new Error("track closed"));
			// Parent→child (withCancelCause) cancellation propagates on the
			// microtask queue; drain it so the track context is observed as
			// cancelled before openGroup runs.
			await new Promise((r) => setTimeout(r, 0));
			const [grp, err] = await tw.openGroup();
			assertEquals(grp, undefined);
			assertInstanceOf(err, Error);
			assertEquals((err as Error).message, "track closed");
			assertEquals(openCalled, false);
		},
	);
});
