import { ReceiveStream } from "./receive_stream.ts";
import { Stream } from "./stream.ts";
import { SendStream } from "./send_stream.ts";
import { StreamConnError, StreamConnErrorInfo } from "./error.ts";

export interface StreamConn {
	openStream(): Promise<[Stream, undefined] | [undefined, Error]>;
	openUniStream(): Promise<[SendStream, undefined] | [undefined, Error]>;
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

	async openStream(): Promise<[Stream, undefined] | [undefined, Error]> {
		try {
			const wtStream = await this.#webtransport.createBidirectionalStream();
			const stream = new Stream({
				stream: wtStream,
			});
			return [stream, undefined];
		} catch (err) {
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
	}

	async openUniStream(): Promise<[SendStream, undefined] | [undefined, Error]> {
		try {
			const wtStream = await this.#webtransport.createUnidirectionalStream();
			const stream = new SendStream({
				stream: wtStream,
			});
			return [stream, undefined];
		} catch (e) {
			return [undefined, e as Error];
		}
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
