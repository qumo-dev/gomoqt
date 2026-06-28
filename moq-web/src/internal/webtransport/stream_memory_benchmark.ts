/**
 * Memory efficiency benchmarks for SendStream and ReceiveStream.
 *
 * These benchmarks measure:
 * 1. ReceiveStream buffer retention patterns
 * 2. Subarray vs reallocation memory usage
 * 3. Small buffer allocation costs
 */

import { SendStream } from "./send_stream.ts";
import { ReceiveStream } from "./receive_stream.ts";

// Helper to create a mock WritableStream
function createMockWritableStream(): WritableStream<Uint8Array> {
	return new WritableStream<Uint8Array>({
		write(_chunk) {
			// Discard data
		},
	});
}

// Helper to create a mock ReadableStream
function createMockReadableStream(chunks: Uint8Array[]): ReadableStream<Uint8Array> {
	let index = 0;
	return new ReadableStream<Uint8Array>({
		pull(controller) {
			if (index < chunks.length) {
				controller.enqueue(chunks[index++]!);
			} else {
				controller.close();
			}
		},
	});
}

Deno.bench({
	name: "ReceiveStream: read large chunk, consume small (current behavior)",
	group: "receivestream-memory",
	baseline: true,
	fn: async () => {
		// Simulate: WebTransport sends 16KB chunk, we read 64 bytes
		const largeChunk = new Uint8Array(16384);
		const stream = createMockReadableStream([largeChunk]);
		const reader = new ReceiveStream({ stream });

		// Read only 64 bytes - rest stays in buffer
		const buf = new Uint8Array(64);
		await reader.read(buf);

		// In current implementation, #buf holds subarray referencing 16KB
	},
});

Deno.bench({
	name: "ReceiveStream: read large chunk, consume small (optimized)",
	group: "receivestream-memory-optimized",
	fn: async () => {
		// After optimization: buffer resets to new Uint8Array(0) when exhausted
		const largeChunk = new Uint8Array(16384);
		const stream = createMockReadableStream([largeChunk]);
		const reader = new ReceiveStream({ stream });

		// Read only 64 bytes
		const buf = new Uint8Array(64);
		await reader.read(buf);

		// With optimization, subsequent full consumption releases backing buffer
		const rest = new Uint8Array(16384 - 64);
		await reader.read(rest);
		// Now #buf should be new Uint8Array(0), not holding 16KB reference
	},
});

Deno.bench({
	name: "ReceiveStream: read and fully consume buffer",
	group: "receivestream-memory",
	fn: async () => {
		const chunk = new Uint8Array(1024);
		const stream = createMockReadableStream([chunk]);
		const reader = new ReceiveStream({ stream });

		// Read entire chunk
		const buf = new Uint8Array(1024);
		await reader.read(buf);

		// Buffer should be empty (but currently holds zero-length subarray)
	},
});

Deno.bench({
	name: "ReceiveStream: multiple small reads from large chunk",
	group: "receivestream-memory",
	fn: async () => {
		const largeChunk = new Uint8Array(4096);
		const stream = createMockReadableStream([largeChunk]);
		const reader = new ReceiveStream({ stream });

		// Simulate reading varint (1-8 bytes) + small header
		for (let i = 0; i < 10; i++) {
			const buf = new Uint8Array(8);
			await reader.read(buf);
		}
	},
});

Deno.bench({
	name: "Uint8Array allocation: 64 bytes (small)",
	group: "allocation-cost",
	baseline: true,
	fn: () => {
		const buf = new Uint8Array(64);
		buf[0] = 1; // Prevent optimization
	},
});

Deno.bench({
	name: "Uint8Array allocation: 256 bytes",
	group: "allocation-cost",
	fn: () => {
		const buf = new Uint8Array(256);
		buf[0] = 1;
	},
});

Deno.bench({
	name: "Uint8Array allocation: 512 bytes",
	group: "allocation-cost",
	fn: () => {
		const buf = new Uint8Array(512);
		buf[0] = 1;
	},
});

Deno.bench({
	name: "Uint8Array allocation: 1KB",
	group: "allocation-cost",
	fn: () => {
		const buf = new Uint8Array(1024);
		buf[0] = 1;
	},
});

Deno.bench({
	name: "Uint8Array allocation: 4KB",
	group: "allocation-cost",
	fn: () => {
		const buf = new Uint8Array(4096);
		buf[0] = 1;
	},
});

Deno.bench({
	name: "Subarray: keep reference (no copy)",
	group: "subarray-vs-copy",
	baseline: true,
	fn: () => {
		const large = new Uint8Array(4096);
		const small = large.subarray(0, 256);
		small[0] = 1;
	},
});

Deno.bench({
	name: "Reallocate: copy to new array",
	group: "subarray-vs-copy",
	fn: () => {
		const large = new Uint8Array(4096);
		const small = new Uint8Array(256);
		small.set(large.subarray(0, 256));
		small[0] = 1;
	},
});

Deno.bench({
	name: "SendStream: single write (1KB)",
	group: "sendstream-write",
	baseline: true,
	fn: async () => {
		const stream = createMockWritableStream();
		const writer = new SendStream({ stream });

		const data = new Uint8Array(1024);
		await writer.write(data);
	},
});

Deno.bench({
	name: "SendStream: two small writes (8 bytes + 1KB)",
	group: "sendstream-write",
	fn: async () => {
		const stream = createMockWritableStream();
		const writer = new SendStream({ stream });

		// Simulate: varint length + data
		const header = new Uint8Array(8);
		const data = new Uint8Array(1024);
		await writer.write(header);
		await writer.write(data);
	},
});

Deno.bench({
	name: "SendStream: combined write (8 bytes + 1KB in one buffer)",
	group: "sendstream-write",
	fn: async () => {
		const stream = createMockWritableStream();
		const writer = new SendStream({ stream });

		// Combine into single write
		const combined = new Uint8Array(1032);
		combined.set(new Uint8Array(8), 0);
		combined.set(new Uint8Array(1024), 8);
		await writer.write(combined);
	},
});
