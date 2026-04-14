/** A readable source of bytes that knows its length and can copy into a buffer. */
export interface ByteSource {
	/** Total number of bytes available. */
	readonly byteLength: number;
	/** Copy the contents into `target`. */
	copyTo(target: ArrayBuffer | ArrayBufferView): void;
}

/** A writable destination for bytes. */
export interface ByteSink {
	write(p: Uint8Array): void | Promise<void>;
}

/** Function equivalent of {@link ByteSink.write}. */
export type ByteSinkFunc = (p: Uint8Array) => void | Promise<void>;

/**
 * In-memory buffer implementing both {@link ByteSource} and {@link ByteSink}.
 *
 * Automatically resizes on write. Use {@link bytes} to read back the data.
 */
export class BytesBuffer implements ByteSource, ByteSink {
	#buf: ArrayBuffer; // Internal buffer (full capacity)

	#len: number = 0; // Actual data length

	get byteLength(): number {
		return this.#len;
	}

	get bytes(): Uint8Array {
		return new Uint8Array(this.#buf, 0, this.#len);
	}

	constructor(buffer?: ArrayBuffer | Uint8Array) {
		if (buffer instanceof Uint8Array) {
			const slice = buffer.buffer.slice(
				buffer.byteOffset,
				buffer.byteOffset + buffer.byteLength,
			);
			this.#buf = slice instanceof SharedArrayBuffer
				? new ArrayBuffer(buffer.byteLength)
				: slice;
			if (slice instanceof SharedArrayBuffer) {
				new Uint8Array(this.#buf).set(buffer);
			}
			this.#len = buffer.byteLength;
		} else {
			this.#buf = buffer ?? new ArrayBuffer(0);
		}
	}

	write(p: Uint8Array): void {
		if (this.#buf.byteLength < p.byteLength) {
			// Resize buffer if necessary
			this.#buf = new ArrayBuffer(p.byteLength);
		}

		const target = new Uint8Array(this.#buf, 0, p.byteLength);
		target.set(p);
		this.#len = p.byteLength;
	}

	copyTo(dest: AllowSharedBufferSource): void {
		let target: Uint8Array;
		if (dest instanceof Uint8Array) {
			target = dest;
		} else if (dest instanceof ArrayBuffer || dest instanceof SharedArrayBuffer) {
			target = new Uint8Array(dest as ArrayBuffer); // Handle both ArrayBuffer and SharedArrayBuffer
		} else {
			throw new Error("Unsupported destination type");
		}

		if (target.byteLength < this.#len) {
			throw new Error(
				`Destination buffer too small: ${target.byteLength} < ${this.#len}`,
			);
		}

		// Ensure we don't exceed the buffer bounds
		const copyLen = Math.min(this.#len, this.#buf.byteLength);
		target.set(new Uint8Array(this.#buf, 0, copyLen));
	}
}

/**
 * A {@link Frame} combines readable ({@link ByteSource}) and writable ({@link ByteSink})
 * semantics with a `bytes` view of the underlying data.
 */
export interface Frame extends ByteSource, ByteSink {
	/** View of the frame's raw bytes. */
	readonly bytes: Uint8Array;
}

/**
 * Construct a new {@link Frame} backed by a {@link BytesBuffer}.
 *
 * @example
 * ```ts
 * const frame = new Frame(new ArrayBuffer(1024));
 * frame.write(new Uint8Array([1, 2, 3]));
 * console.log(frame.bytes); // Uint8Array [1, 2, 3]
 * ```
 */
export const Frame: {
	new (buffer: ArrayBuffer | Uint8Array): Frame;
} = BytesBuffer;
