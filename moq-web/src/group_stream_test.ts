import { assertEquals, assertInstanceOf } from "@std/assert";
import { spy } from "@std/testing/mock";
import { GroupReader, GroupWriter } from "./group_stream.ts";
import { GroupMessage, writeVarint } from "./internal/message/mod.ts";
import { Frame } from "./frame.ts";
import { background, withCancelCause } from "@okdaichi/golikejs/context";
import { GroupErrorCode } from "./error.ts";
import { SendStream } from "./internal/webtransport/mod.ts";
import { ReceiveStream } from "./internal/webtransport/mod.ts";
import { EOFError } from "@okdaichi/golikejs/io";
import { MockReceiveStream, MockSendStream } from "./mock_stream_test.ts";

Deno.test("GroupWriter", async (t) => {
	await t.step(
		"writeFrame writes correct bytes and returns undefined",
		async () => {
			const [ctx] = withCancelCause(background());
			const writtenData: Uint8Array[] = [];
			const writer = new MockSendStream({
				id: 1n,
				write: spy(async (p: Uint8Array) => {
					writtenData.push(new Uint8Array(p));
					return [p.length, undefined] as [number, Error | undefined];
				}),
			});
			const msg = new GroupMessage({ sequence: 1, subscribeId: 0 });
			const gw = new GroupWriter(ctx, writer, msg);
			const data = new Uint8Array([1, 2, 3]);
			const frame = new Frame(data.buffer);
			frame.write(data);
			const err = await gw.writeFrame(frame);
			assertEquals(err, undefined);
			assertEquals(writtenData.length, 2);
			const allData = new Uint8Array(
				writtenData.reduce((a, b) => a + b.length, 0),
			);
			let offset = 0;
			for (const d of writtenData) {
				allData.set(d, offset);
				offset += d.length;
			}
			assertEquals(
				allData.subarray(allData.length - 3),
				new Uint8Array([1, 2, 3]),
			);
		},
	);

	await t.step("writeFrame returns an error if write fails", async () => {
		const [ctx] = withCancelCause(background());
		const writer = new MockSendStream({
			id: 1n,
			write: spy(async (_p: Uint8Array) => {
				return [0, new Error("fail")] as [number, Error | undefined];
			}),
		});
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
			const writer = new MockSendStream({
				id: 2n,
				close: spy(async () => {
					closeCalls++;
				}),
			});
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
			streamId: 1n,
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
			const writer = new MockSendStream({
				id: 5n,
				close: spy(async () => {
					closeCalls++;
				}),
			});
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
			const writer = new MockSendStream({
				id: 6n,
				cancel: spy(async (code: number) => {
					cancelCalls.push(code);
				}),
			});
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
			const encoderWrittenData: Uint8Array[] = [];
			const ms = {
				write: spy(
					async (p: Uint8Array): Promise<[number, Error | undefined]> => {
						encoderWrittenData.push(new Uint8Array(p));
						return [p.length, undefined];
					},
				),
			};
			await writeVarint(ms, payload.length);
			await ms.write(payload);
			const total = encoderWrittenData.reduce((a, b) => a + b.length, 0);
			const data = new Uint8Array(total);
			let off = 0;
			for (const d of encoderWrittenData) {
				data.set(d, off);
				off += d.length;
			}
			let readOffset = 0;
			const rs = new MockReceiveStream({
				id: 8n,
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
		const rs = new MockReceiveStream({
			id: 4n,
			cancel: spy(async (code: number) => {
				cancelCalls.push(code);
			}),
		});
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
			const rs = new MockReceiveStream({
				id: 7n,
				cancel: spy(async (code: number) => {
					cancelCalls.push(code);
				}),
			});
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

		const reader = new ReceiveStream({ stream: readable, streamId: 1n });
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

			const reader = new ReceiveStream({ stream: readable, streamId: 1n });
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

			const encoderWrittenData: Uint8Array[] = [];
			const ms = {
				write: spy(
					async (p: Uint8Array): Promise<[number, Error | undefined]> => {
						encoderWrittenData.push(new Uint8Array(p));
						return [p.length, undefined];
					},
				),
			};

			// Encode both frames
			for (const payload of payloads) {
				await writeVarint(ms, payload.length);
				await ms.write(payload);
			}

			const total = encoderWrittenData.reduce((a, b) => a + b.length, 0);
			const data = new Uint8Array(total);
			let off = 0;
			for (const d of encoderWrittenData) {
				data.set(d, off);
				off += d.length;
			}

			let readOffset = 0;
			const rs = new MockReceiveStream({
				id: 9n,
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
		const rs = new MockReceiveStream({
			id: 42n,
			read: spy(async () => [0, new EOFError()]),
		});
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
		// encode buffers
		const encoderWritten: Uint8Array[] = [];
		const ms = { write: spy(async (p: Uint8Array): Promise<[number, Error | undefined]> => { encoderWritten.push(new Uint8Array(p)); return [p.length, undefined]; }) };
		for (const pl of payloads) {
			await writeVarint(ms, pl.length);
			await ms.write(pl);
		}
		const total = encoderWritten.reduce((a, b) => a + b.length, 0);
		const data = new Uint8Array(total);
		let off = 0;
		for (const d of encoderWritten) { data.set(d, off); off += d.length; }

		let readOffset = 0;
		const rs = new MockReceiveStream({
			id: 99n,
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
