import { assertEquals, assertInstanceOf } from "@std/assert";
import { spy } from "@std/testing/mock";
import { GroupReader, GroupWriter } from "./group_stream.ts";
import { GroupMessage, writeVarint } from "./internal/message/mod.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";
import { Frame } from "./frame.ts";
import { background, withCancelCause } from "@okdaichi/golikejs/context";
import { GroupErrorCode } from "./error.ts";
import { SendStream } from "./internal/webtransport/mod.ts";
import { ReceiveStream } from "./internal/webtransport/mod.ts";
import { EOFError } from "@okdaichi/golikejs/io";

Deno.test("GroupWriter", async (t) => {
	await t.step(
		"writeFrame writes correct bytes and returns undefined",
		async () => {
			const [ctx] = withCancelCause(background());
			const buf = new Buffer(new ArrayBuffer(0));
			const writeSpy = spy(buf.write.bind(buf));
			const writer: SendStream = {
				write: writeSpy,
				close: async () => {},
				cancel: async (_code: number) => {},
				closed: () => new Promise<void>(() => {}),
			};
			// track each write by spying; data is stored in buf
			const msg = new GroupMessage({ sequence: 1, subscribeId: 0 });
			const gw = new GroupWriter(ctx, writer, msg);
			const data = new Uint8Array([1, 2, 3]);
			const frame = new Frame(data.buffer);
			frame.write(data);
			const err = await gw.writeFrame(frame);
			assertEquals(err, undefined);
			// two writes: length prefix + payload
			assertEquals(writeSpy.calls.length, 2);
			const allData = buf.bytes();
			assertEquals(
				allData.subarray(allData.length - 3),
				new Uint8Array([1, 2, 3]),
			);
		},
	);

	await t.step("writeFrame returns an error if write fails", async () => {
		const [ctx] = withCancelCause(background());
		// failure writer that always returns an error
		const writer: SendStream = {
			write: spy(async (_p: Uint8Array) => [0, new Error("fail")]),
			close: async () => {},
			cancel: async (_code: number) => {},
			closed: () => new Promise<void>(() => {}),
		};
		const msg = new GroupMessage({ sequence: 1, subscribeId: 0 });
		const gw = new GroupWriter(ctx, writer, msg);
		const data = new Uint8Array([1]);
		const frame = new Frame(data.buffer);
		frame.write(data);
		const err = await gw.writeFrame(frame);
		assertEquals(err instanceof Error, true);
	});

	await t.step(
		"close increments close calls and cancel does not panic when already cancelled",
		async () => {
			const [ctx] = withCancelCause(background());
			let closeCalls = 0;
			const writer: SendStream = {
				write: async (p: Uint8Array) => [p.length, undefined],
				close: spy(async () => {
					closeCalls++;
				}),
				cancel: async (_code: number) => {},
				closed: () => new Promise<void>(() => {}),
			};
			const msg = new GroupMessage({ sequence: 1, subscribeId: 0 });
			const gw = new GroupWriter(ctx, writer, msg);
			await gw.close();
			assertEquals(closeCalls, 1);
			await gw.cancel(GroupErrorCode.PublishAborted);
		},
	);

	await t.step("cancel doesn't panic when already cancelled", async () => {
		let canceled = false;
		const writer = new SendStream({
			stream: new WritableStream({
				write(_c) {},
				abort(_e) {
					canceled = true;
					return Promise.resolve();
				},
			}),
		});
		const groupMsg = new GroupMessage({ sequence: 1 });
		const gw = new GroupWriter(background(), writer, groupMsg);
		await gw.cancel(GroupErrorCode.SubscribeCanceled);
		await gw.cancel(GroupErrorCode.SubscribeCanceled);
		assertEquals(canceled, true);
	});

	await t.step(
		"close does nothing when context already has error",
		async () => {
			const [ctx, cancelFunc] = withCancelCause(background());
			cancelFunc(new Error("already canceled"));
			await new Promise((r) => setTimeout(r, 0));
			let closeCalls = 0;
			const writer: SendStream = {
				write: async (p: Uint8Array) => [p.length, undefined],
				close: spy(async () => {
					closeCalls++;
				}),
				cancel: async (_code: number) => {},
				closed: () => new Promise<void>(() => {}),
			};
			const msg = new GroupMessage({ sequence: 1, subscribeId: 0 });
			const gw = new GroupWriter(ctx, writer, msg);
			await gw.close();
			assertEquals(closeCalls, 0);
		},
	);

	await t.step(
		"cancel does nothing when context already has error",
		async () => {
			const [ctx, cancelFunc] = withCancelCause(background());
			cancelFunc(new Error("already canceled"));
			await new Promise((r) => setTimeout(r, 0));
			const cancelCalls: number[] = [];
			const writer: SendStream = {
				write: async (p: Uint8Array) => [p.length, undefined],
				close: async () => {},
				cancel: spy(async (code: number) => {
					cancelCalls.push(code);
				}),
				closed: () => new Promise<void>(() => {}),
			};
			const msg = new GroupMessage({ sequence: 1, subscribeId: 0 });
			const gw = new GroupWriter(ctx, writer, msg);
			await gw.cancel(GroupErrorCode.SubscribeCanceled);
			assertEquals(cancelCalls.length, 0);
		},
	);
});

Deno.test("GroupReader", async (t) => {
	await t.step(
		"readFrame reads data without growing buffer when sufficient",
		async () => {
			const [ctx] = withCancelCause(background());
			const payload = new Uint8Array([10, 20, 30]);
			// use Buffer from golikejs/bytes to collect encoded bytes
			const buf = new Buffer(new ArrayBuffer(0));
			await writeVarint(buf, payload.length);
			buf.write(payload);
			const data = buf.bytes();
			// use Buffer as a simple receive stream
			const bufSrc = new Buffer(new ArrayBuffer(0));
			bufSrc.write(data);
			const rs: ReceiveStream = {
				read: spy(bufSrc.read.bind(bufSrc)),
				cancel: async (_code: number) => {},
				closed: () => new Promise<void>(() => {}),
			};
			const msg = new GroupMessage({ sequence: 1, subscribeId: 0 });
			const gr = new GroupReader(ctx, rs, msg);
			const frame = new Frame(new ArrayBuffer(10));
			const err = await gr.readFrame(frame);
			assertEquals(err, undefined);
			// readFrame writes the exact data read to the frame
			assertEquals(frame.byteLength, payload.length);
			const result = new Uint8Array(frame.byteLength);
			frame.copyTo(result);
			assertEquals(result, payload);
		},
	);

	await t.step("cancel cancels underlying stream", async () => {
		const [ctx] = withCancelCause(background());
		const cancelCalls: number[] = [];
		const rs: ReceiveStream = {
			read: async () => [0, new EOFError()],
			cancel: spy(async (code: number) => {
				cancelCalls.push(code);
			}),
			closed: () => new Promise<void>(() => {}),
		};
		const msg = new GroupMessage({ sequence: 1, subscribeId: 0 });
		const gr = new GroupReader(ctx, rs, msg);
		await gr.cancel(GroupErrorCode.ExpiredGroup);
		assertEquals(cancelCalls.length, 1);
	});

	await t.step(
		"cancel does nothing when context already has error",
		async () => {
			const [ctx, cancelFunc] = withCancelCause(background());
			cancelFunc(new Error("already canceled"));
			const cancelCalls: number[] = [];
			const rs: ReceiveStream = {
				read: async () => [0, new EOFError()],
				cancel: spy(async (code: number) => {
					cancelCalls.push(code);
				}),
				closed: () => new Promise<void>(() => {}),
			};
			const msg = new GroupMessage({ sequence: 1, subscribeId: 0 });
			const gr = new GroupReader(ctx, rs, msg);
			await gr.cancel(GroupErrorCode.ExpiredGroup);
			assertEquals(cancelCalls.length, 0);
		},
	);

	await t.step("readFrame returns error when varint too large", async () => {
		const bytes = new Uint8Array([
			0xff,
			0xff,
			0xff,
			0xff,
			0xff,
			0xff,
			0xff,
			0xff,
			0x01,
		]);
		const readable = new ReadableStream<Uint8Array>({
			start(c) {
				c.enqueue(bytes);
				c.close();
			},
		});

		const reader = new ReceiveStream({ stream: readable });
		const gr = new GroupReader(
			background(),
			reader,
			new GroupMessage({ sequence: 1 }),
		);

		const fr = new Frame(new ArrayBuffer(1));
		const errRes = await gr.readFrame(fr);
		assertInstanceOf(errRes, Error);
	});

	await t.step(
		"readFrame returns error when readFull returns EOFError due to insufficient data",
		async () => {
			const lenBuf = new Uint8Array([0x04]);
			const dataBuf = new Uint8Array([1, 2]);
			const total = new Uint8Array([...lenBuf, ...dataBuf]);
			const readable = new ReadableStream<Uint8Array>({
				start(c) {
					c.enqueue(total);
					c.close();
				},
			});

			const reader = new ReceiveStream({ stream: readable });
			const gr = new GroupReader(
				background(),
				reader,
				new GroupMessage({ sequence: 1, subscribeId: 0 }),
			);

			const fr = new Frame(new ArrayBuffer(8));
			const err = await gr.readFrame(fr);
			assertInstanceOf(err, Error);
		},
	);

	await t.step(
		"readFrame preserves buffer capacity across multiple reads",
		async () => {
			const [ctx] = withCancelCause(background());

			// Simulate two reads: first 256 bytes, then 512 bytes
			const payloads = [
				new Uint8Array(256).fill(1), // First frame
				new Uint8Array(512).fill(2), // Second frame (larger)
			];

			// use Buffer to accumulate two encoded frames
			const buf = new Buffer(new ArrayBuffer(0));

			// Encode both frames
			for (const payload of payloads) {
				await writeVarint(buf, payload.length);
				buf.write(payload);
			}

			const data = buf.bytes();

			const bufSrc = new Buffer(new ArrayBuffer(0));
			bufSrc.write(data);
			const rs: ReceiveStream = {
				read: spy(bufSrc.read.bind(bufSrc)),
				cancel: async (_code: number) => {},
				closed: () => new Promise<void>(() => {}),
			};

			const msg = new GroupMessage({ sequence: 1, subscribeId: 0 });
			const gr = new GroupReader(ctx, rs, msg);

			// Create frame with 1024 byte capacity
			const frame = new Frame(new ArrayBuffer(1024));

			// First read: 256 bytes
			const err1 = await gr.readFrame(frame);
			assertEquals(err1, undefined);
			assertEquals(frame.byteLength, 256, "First read should be 256 bytes");
			const result1 = new Uint8Array(frame.byteLength);
			frame.copyTo(result1);
			assertEquals(result1, payloads[0]);

			// Second read: 512 bytes (larger, but still fits in capacity)
			const err2 = await gr.readFrame(frame);
			assertEquals(err2, undefined);
			assertEquals(frame.byteLength, 512, "Second read should be 512 bytes");
			const result2 = new Uint8Array(frame.byteLength);
			frame.copyTo(result2);
			assertEquals(result2, payloads[1]);
		},
	);

	await t.step("readFrame returns EOFError when stream closes immediately", async () => {
		const rs: ReceiveStream = {
			read: spy(async () => [0, new EOFError()]),
			cancel: async (_code: number) => {},
			closed: () => new Promise<void>(() => {}),
		};
		const gr = new GroupReader(background(), rs, new GroupMessage({ sequence: 1 }));
		const fr = new Frame(new ArrayBuffer(1));
		const err = await gr.readFrame(fr);
		assertInstanceOf(err, EOFError);
	});

	await t.step("frames() async iterator yields frames then terminates", async () => {
		const payloads = [
			new Uint8Array([1, 2, 3]),
			new Uint8Array([4, 5]),
		];
		// encode buffers using Buffer
		const buf = new Buffer(new ArrayBuffer(0));
		for (const pl of payloads) {
			await writeVarint(buf, pl.length);
			buf.write(pl);
		}
		const data = buf.bytes();

		const bufSrc = new Buffer(new ArrayBuffer(0));
		bufSrc.write(data);
		const rs: ReceiveStream = {
			read: spy(bufSrc.read.bind(bufSrc)),
			cancel: async (_code: number) => {},
			closed: () => new Promise<void>(() => {}),
		};
		const gr = new GroupReader(background(), rs, new GroupMessage({ sequence: 1 }));
		const got: Uint8Array[] = [];
		for await (const f of gr.frames()) {
			const arr = new Uint8Array(f.byteLength);
			f.copyTo(arr);
			got.push(arr);
		}
		assertEquals(got.length, payloads.length);
		for (let i = 0; i < payloads.length; i++) {
			assertEquals(got[i], payloads[i]);
		}
	});
});
