import { EOFError, type Writer } from "@okdaichi/golikejs/io";
import { WebTransportStreamError, WebTransportStreamErrorInfo } from "./error.ts";
import { WebTransportStreamErrorCode } from "./mod.ts";

export interface SendStream extends Writer {
	close(): Promise<void>;
	cancel(code: WebTransportStreamErrorCode): Promise<void>;

	closed(): Promise<void>;
}

export interface SendStreamInit {
	stream: WritableStream<Uint8Array>;
}

/**
 * SendStream wraps a WebTransport WritableStream and implements the io.Writer interface.
 * This is a thin wrapper that passes data directly to the underlying stream without internal buffering.
 */
class SendStreamClass implements SendStream {
	#writer: WritableStreamDefaultWriter<Uint8Array>;

	#err?: Error;

	constructor(init: SendStreamInit) {
		this.#writer = init.stream.getWriter();
	}

	/**
	 * Writes p to the underlying stream.
	 * Returns the number of bytes written and any error encountered.
	 * Implements io.Writer interface.
	 */
	async write(p: Uint8Array): Promise<[number, Error | undefined]> {
		if (this.#err) {
			return [0, this.#err];
		}
		try {
			await this.#writer.write(p);
			return [p.length, undefined];
		} catch (err) {
			if (this.#err) {
				return [0, this.#err];
			}

			if (err instanceof Error) {
				return [0, err];
			}
			const wtErr = err as WebTransportError;
			if (wtErr.source === "stream") {
				if (wtErr.streamErrorCode !== null) {
					this.#err = new WebTransportStreamError(
						wtErr as WebTransportStreamErrorInfo,
						true,
					);
				} else {
					this.#err = new EOFError();
				}
			}
			return [0, this.#err];
		}
	}

	async close(): Promise<void> {
		await this.#writer.close();
	}

	async cancel(code: WebTransportStreamErrorCode): Promise<void> {
		if (this.#err) {
			return;
		}
		const wtErr: WebTransportStreamErrorInfo = {
			source: "stream",
			streamErrorCode: code,
		};
		const err = new WebTransportStreamError(wtErr, false);
		this.#err = err;
		await this.#writer.abort(err);
	}

	closed(): Promise<void> {
		return this.#writer.closed;
	}
}

export const SendStream: {
	new (init: SendStreamInit): SendStream;
} = SendStreamClass;
