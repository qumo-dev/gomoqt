import { ReceiveStream } from "./receive_stream.ts";
import { Stream } from "./stream.ts";
import { SendStream } from "./send_stream.ts";
import { StreamConnError, StreamConnErrorInfo } from "./error.ts";
import { background, ContextCancelledError, watchSignal } from "@okdaichi/golikejs/context";

export interface StreamConn {
	openStream(
		options?: { signal?: AbortSignal },
	): Promise<[Stream, undefined] | [undefined, Error]>;
	openUniStream(
		options?: { signal?: AbortSignal },
	): Promise<[SendStream, undefined] | [undefined, Error]>;
	acceptStream(): Promise<[Stream, undefined] | [undefined, Error]>;
	acceptUniStream(): Promise<[ReceiveStream, undefined] | [undefined, Error]>;
	close(closeInfo?: WebTransportCloseInfo): void;
	ready: Promise<void>;
	closed: Promise<WebTransportCloseInfo>;
	protocol: string;
}

type WebTransportUnidirectionalStream = ReadableStream<Uint8Array>;
// TODO: Use proper WebTransport types when available

export class WebTransportSession implements StreamConn {
	#webtransport: WebTransport;

	#uniStreams: ReadableStreamDefaultReader<WebTransportUnidirectionalStream>;
	#biStreams: ReadableStreamDefaultReader<WebTransportBidirectionalStream>;

	constructor(url: string | URL, options?: WebTransportOptions);
	constructor(transport: WebTransport);
	constructor(arg1: string | URL | WebTransport, arg2?: WebTransportOptions) {
		let webtransport: WebTransport;
		if (typeof arg1 === "string" || arg1 instanceof URL) {
			webtransport = new WebTransport(arg1, arg2);
		} else {
			webtransport = arg1;
		}
		this.#webtransport = webtransport;
		this.#biStreams = this.#webtransport.incomingBidirectionalStreams
			.getReader();
		this.#uniStreams = this.#webtransport.incomingUnidirectionalStreams
			.getReader();
	}

	get protocol(): string {
		// @ts-expect-error WebTransport.protocol is not yet in TypeScript lib, but is supported in browsers
		return this.#webtransport.protocol || "";
	}

	async openStream(
		options?: { signal?: AbortSignal },
	): Promise<[Stream, undefined] | [undefined, Error]> {
		const signal = options?.signal;
		// §1 Abort before starting: no stream is opened.
		if (signal?.aborted) {
			return [undefined, abortError(signal)];
		}

		// Without a signal, preserve the original behavior exactly.
		if (!signal) {
			try {
				const wtStream = await this.#webtransport.createBidirectionalStream();
				return [new Stream({ stream: wtStream }), undefined];
			} catch (err) {
				return await this.#mapOpenStreamError(err);
			}
		}

		// Browser WebTransport createBidirectionalStream() accepts no signal, so
		// cancellation is external. Race the open against the signal, mirroring
		// Go's ctx.Done(). If the signal wins, the WT promise may still resolve
		// later — abandon any late stream so it is never orphaned.
		const ctx = watchSignal(background(), signal);
		const createPromise = this.#webtransport.createBidirectionalStream();
		await Promise.race([createPromise, ctx.done()]);
		if (ctx.err()) {
			createPromise.then((late) => abandonRawStream(late)).catch(() => {});
			return [undefined, ctx.err()!];
		}

		try {
			const wtStream = await createPromise;
			return [new Stream({ stream: wtStream }), undefined];
		} catch (err) {
			return await this.#mapOpenStreamError(err);
		}
	}

	async #mapOpenStreamError(err: unknown): Promise<[undefined, Error]> {
		if (err instanceof Error) {
			return [undefined, err];
		}
		const wtErr = err as WebTransportError;
		if (wtErr.source === "session") {
			const info = await this.#webtransport.closed;
			if (info.closeCode !== undefined && info.reason !== undefined) {
				return [
					undefined,
					new StreamConnError(
						info as StreamConnErrorInfo,
						true,
					),
				];
			}
		}
		return [undefined, err as Error];
	}

	async openUniStream(
		options?: { signal?: AbortSignal },
	): Promise<[SendStream, undefined] | [undefined, Error]> {
		const signal = options?.signal;
		// §1 Abort before starting: no stream is opened.
		if (signal?.aborted) {
			return [undefined, abortError(signal)];
		}

		// Without a signal, preserve the original behavior exactly.
		if (!signal) {
			try {
				const wtStream = await this.#webtransport.createUnidirectionalStream();
				return [new SendStream({ stream: wtStream }), undefined];
			} catch (e) {
				return [undefined, e as Error];
			}
		}

		// Browser WebTransport createUnidirectionalStream() accepts no signal, so
		// cancellation is external. Race the open against the signal, mirroring
		// Go's ctx.Done(). If the signal wins, the WT promise may still resolve
		// later — abandon any late stream so it is never orphaned.
		const ctx = watchSignal(background(), signal);
		const createPromise = this.#webtransport.createUnidirectionalStream();
		await Promise.race([createPromise, ctx.done()]);
		if (ctx.err()) {
			createPromise.then((late) => abandonRawStream(late)).catch(() => {});
			return [undefined, ctx.err()!];
		}

		return [new SendStream({ stream: await createPromise }), undefined];
	}

	async acceptStream(): Promise<[Stream, undefined] | [undefined, Error]> {
		const { done, value: wtStream } = await this.#biStreams.read();
		if (done) {
			return [undefined, new Error("Failed to accept stream")];
		}
		const stream = new Stream({
			stream: wtStream,
		});
		return [stream, undefined];
	}

	async acceptUniStream(): Promise<
		[ReceiveStream, undefined] | [undefined, Error]
	> {
		const { done, value: wtStream } = await this.#uniStreams.read();
		if (done) {
			return [undefined, new Error("Failed to accept unidirectional stream")];
		}
		const stream = new ReceiveStream({
			stream: wtStream,
		});
		return [stream, undefined];
	}

	close(closeInfo?: WebTransportCloseInfo): void {
		this.#webtransport.close(closeInfo);
		// Cancel readers to resolve any pending read() calls
		this.#biStreams.cancel().catch(() => {});
		this.#uniStreams.cancel().catch(() => {});
	}

	get ready(): Promise<void> {
		return this.#webtransport.ready;
	}

	get closed(): Promise<WebTransportCloseInfo> {
		return this.#webtransport.closed;
	}
}

/**
 * Build the error returned when a caller aborts a stream open. Mirrors
 * golikejs watchSignal's reason extraction: signal.reason when it is an Error,
 * otherwise a ContextCancelledError.
 */
function abortError(signal: AbortSignal): Error {
	const reason = (signal as unknown as { reason?: unknown }).reason;
	return reason instanceof Error ? reason : new ContextCancelledError();
}

/**
 * Best-effort cleanup of a WebTransport stream that resolves after its open was
 * already aborted. Aborts (RESET_STREAM) rather than closing, since a clean
 * close would flush a FIN for a stream nobody wants and may block on flow
 * control. Never throws — cleanup must not mask the original abort error.
 */
function abandonRawStream(
	stream: WebTransportBidirectionalStream | WritableStream<Uint8Array>,
): void {
	try {
		if (stream instanceof WritableStream) {
			stream.abort(new ContextCancelledError()).catch(() => {});
			return;
		}
		const bidi = stream as WebTransportBidirectionalStream;
		bidi.writable.abort(new ContextCancelledError()).catch(() => {});
		bidi.readable.cancel().catch(() => {});
	} catch {
		// Best-effort cleanup of an orphaned allocation.
	}
}
