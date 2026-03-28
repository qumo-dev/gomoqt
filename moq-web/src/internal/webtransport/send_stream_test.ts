import { assertEquals, assertInstanceOf } from "@std/assert";
import { SendStream } from "./send_stream.ts";
import { WebTransportStreamError } from "./error.ts";

function setupWriter() {
	const writtenData: Uint8Array[] = [];
	const state = { streamClosed: false };
	const writableStream = new WritableStream<Uint8Array>({
		write(chunk) {
			writtenData.push(chunk.slice()); // Clone to preserve data
		},
		close() {
			state.streamClosed = true;
		},
	});
	const writer = new SendStream({ stream: writableStream });
	return { writer, writtenData, state };
}

Deno.test("SendStream", async (t) => {
	await t.step("write - should write data to stream", async () => {
		const { writer, writtenData, state } = setupWriter();
		try {
			const data = new Uint8Array([1, 2, 3, 4, 5]);
			const [n, err] = await writer.write(data);

			assertEquals(err, undefined);
			assertEquals(n, 5);
			assertEquals(writtenData.length, 1);
			assertEquals(writtenData[0], data);
		} finally {
			try {
				if (!state.streamClosed) await writer.close();
			} catch (_) { /* ignore cleanup errors */ }
		}
	});

	await t.step("write - should handle empty array", async () => {
		const { writer, writtenData, state } = setupWriter();
		try {
			const data = new Uint8Array([]);
			const [n, err] = await writer.write(data);

			assertEquals(err, undefined);
			assertEquals(n, 0);
			assertEquals(writtenData.length, 1);
			assertEquals(writtenData[0]!.length, 0);
		} finally {
			try {
				if (!state.streamClosed) await writer.close();
			} catch (_) { /* ignore cleanup errors */ }
		}
	});

	await t.step("write - should handle multiple writes", async () => {
		const { writer, writtenData, state } = setupWriter();
		try {
			const data1 = new Uint8Array([1, 2, 3]);
			const data2 = new Uint8Array([4, 5, 6, 7]);

			const [n1, err1] = await writer.write(data1);
			assertEquals(err1, undefined);
			assertEquals(n1, 3);

			const [n2, err2] = await writer.write(data2);
			assertEquals(err2, undefined);
			assertEquals(n2, 4);

			assertEquals(writtenData.length, 2);
			assertEquals(writtenData[0], data1);
			assertEquals(writtenData[1], data2);
		} finally {
			try {
				if (!state.streamClosed) await writer.close();
			} catch (_) { /* ignore cleanup errors */ }
		}
	});

	await t.step("close - should close the stream", async () => {
		const { writer, state } = setupWriter();
		await writer.close();
		assertEquals(state.streamClosed, true);
	});

	await t.step("closed() proxies the underlying stream closed promise", async () => {
		let resolveClosed!: () => void;
		const closed = new Promise<void>((resolve) => {
			resolveClosed = resolve;
		});
		const writableStream = new WritableStream<Uint8Array>({
			close() {
				resolveClosed();
			},
		});
		const writer = new SendStream({ stream: writableStream });

		const closedPromise = writer.closed();
		await writer.close();
		await closedPromise;
		await closed;
		assertEquals(true, true);
	});

	await t.step("cancel - should abort the stream with error", async () => {
		const writtenData: Uint8Array[] = [];
		let abortReason: unknown = undefined;
		const writableStream = new WritableStream<Uint8Array>({
			write(chunk) {
				writtenData.push(chunk);
			},
			abort(reason) {
				abortReason = reason;
			},
		});
		const writer = new SendStream({ stream: writableStream });

		const error = new WebTransportStreamError({
			source: "stream",
			streamErrorCode: 1,
		}, false);
		await writer.cancel(error.code);

		assertInstanceOf(
			abortReason,
			WebTransportStreamError as unknown as new (...args: any[]) => Error,
		);
		if (abortReason instanceof WebTransportStreamError) {
			assertEquals(abortReason.code, error.code);
			assertEquals(abortReason.message, error.message);
			assertEquals(abortReason.remote, error.remote);
		}
	});

	await t.step("write - should return error on stream failure", async () => {
		const writableStream = new WritableStream<Uint8Array>({
			write(_chunk) {
				throw new Error("Write failed");
			},
		});
		const writer = new SendStream({ stream: writableStream });

		const data = new Uint8Array([1, 2, 3]);
		const [n, err] = await writer.write(data);

		assertEquals(n, 0);
		assertEquals(err instanceof Error, true);
		assertEquals(err!.message, "Write failed");
	});

	await t.step(
		"write handles WebTransportError with null streamErrorCode as EOFError",
		async () => {
			const writable = new WritableStream<Uint8Array>({
				write(_chunk) {
					// Reject with WebTransportError-like object (non-Error)
					return Promise.reject({ source: "stream", streamErrorCode: null });
				},
			});
			const s = new SendStream({ stream: writable });

			const [n, err] = await s.write(new Uint8Array([1, 2, 3]));
			assertEquals(n, 0);
			// err should be instanceof Error (EOFError or Error)
			assertInstanceOf(err, Error);

			// Subsequent writes should return same error
			const [n2, err2] = await s.write(new Uint8Array([4, 5]));
			assertEquals(n2, 0);
			assertInstanceOf(err2, Error);
		},
	);

	await t.step(
		"write handles WebTransportError with streamErrorCode set as StreamError",
		async () => {
			const writable = new WritableStream<Uint8Array>({
				write(_chunk) {
					// Reject with WebTransportError-like object (non-Error)
					return Promise.reject({ source: "stream", streamErrorCode: 123 });
				},
			});
			const s = new SendStream({ stream: writable });

			const [n, err] = await s.write(new Uint8Array([1, 2, 3]));
			assertEquals(n, 0);
			assertInstanceOf(err, WebTransportStreamError);

			// Further writes should immediately return the same StreamError
			const [n2, err2] = await s.write(new Uint8Array([4, 5]));
			assertEquals(n2, 0);
			assertInstanceOf(err2, WebTransportStreamError);
		},
	);

	await t.step("cancel sets err and aborts", async () => {
		let aborted = false;
		const writable = new WritableStream<Uint8Array>({
			write(_chunk) {/* no-op */},
			abort(_reason) {
				aborted = true;
				return Promise.resolve();
			},
		});
		const s = new SendStream({ stream: writable });
		await s.cancel(1);
		const [n, err] = await s.write(new Uint8Array([1]));
		assertEquals(n, 0);
		// err should be a StreamError
		assertInstanceOf(err, WebTransportStreamError);
		assertEquals(aborted, true);
	});
});
