import type { Version } from "./version.ts";
import {
	AnnounceInitMessage,
	AnnouncePleaseMessage,
	GroupMessage,
	readVarint,
	SubscribeMessage,
	SubscribeOkMessage,
	writeVarint,
} from "./internal/message/mod.ts";
import {
	ReceiveStream,
	Stream,
	WebTransportSession,
	WebTransportSessionError,
	WebTransportSessionErrorInfo,
} from "./internal/webtransport/mod.ts";
import { Extensions } from "./extensions.ts";
import { background, withCancelCause } from "@okdaichi/golikejs/context";
import type { CancelCauseFunc, Context } from "@okdaichi/golikejs/context";
import { AnnouncementReader, AnnouncementWriter } from "./announce_stream.ts";
import type { TrackPrefix } from "./track_prefix.ts";
import { ReceiveSubscribeStream, SendSubscribeStream } from "./subscribe_stream.ts";
import type { TrackConfig } from "./subscribe_stream.ts";
import { type BroadcastPath, validateBroadcastPath } from "./broadcast_path.ts";
import { TrackReader } from "./track_reader.ts";
import { TrackWriter } from "./track_writer.ts";
import type { TrackMux } from "./track_mux.ts";
import { DefaultTrackMux } from "./track_mux.ts";
import { BiStreamTypes, UniStreamTypes } from "./stream_type.ts";
import { Queue } from "./internal/queue.ts";
import type { SubscribeID, TrackName } from "./alias.ts";

export interface SessionOptions {
	webtransport: WebTransportSession;

	versions?: Set<Version>;
	extensions?: Extensions;
	mux?: TrackMux;
}

export class Session {
	readonly ready: Promise<void>;
	#webtransport: WebTransportSession;
	#ctx: Context;
	#cancelFunc: CancelCauseFunc;

	#wg: Promise<void>[] = [];

	#subscribeIDCounter: number = 0;

	readonly mux: TrackMux;

	#queues: Map<
		SubscribeID,
		Queue<[ReceiveStream, GroupMessage]>
	> = new Map();

	constructor(options: SessionOptions) {
		this.#webtransport = options.webtransport;
		this.mux = options.mux ?? DefaultTrackMux;
		const [ctx, cancel] = withCancelCause(background());
		this.#webtransport.closed.then((info) => {
			if (this.#ctx.err()) {
				return;
			}

			if (info.closeCode === undefined && info.reason === undefined) {
				// This means the establishment of the connection failed
				cancel(new Error("webtransport: connection closed unexpectedly"));
				return;
			}

			cancel(
				new WebTransportSessionError(
					info as WebTransportSessionErrorInfo,
					true,
				),
			);
		}).catch((info) => {
			if (this.#ctx.err()) {
				// Session was already closed
				return;
			}

			// Some error occurred while establishing the connection or waiting for the connection to close
			// The caught error here is likely not defined in general. So we wrap it in a generic Error.
			cancel(new Error(info));
		});
		this.#ctx = ctx;
		this.#cancelFunc = cancel;
		this.ready = this.#setup();
	}

	async #setup(): Promise<void> {
		await this.#webtransport.ready;

		// Start listening for incoming streams
		this.#wg.push(this.#listenBiStreams());
		this.#wg.push(this.#listenUniStreams());

		return;
	}

	async acceptAnnounce(
		prefix: TrackPrefix,
	): Promise<[AnnouncementReader, undefined] | [undefined, Error]> {
		const [stream, openErr] = await this.#webtransport.openStream();
		if (openErr) {
			console.error("moq: failed to open announce stream:", openErr);
			return [undefined, openErr];
		}
		// Send STREAM_TYPE
		let [, err] = await writeVarint(
			stream.writable,
			BiStreamTypes.AnnounceStreamType,
		);
		if (err) {
			console.error("moq: failed to open announce stream:", err);
			return [undefined, err];
		}

		// Send AnnouncePleaseMessage
		const req = new AnnouncePleaseMessage({ prefix });
		err = await req.encode(stream.writable);
		if (err) {
			console.error("moq: failed to send ANNOUNCE_PLEASE message:", err);
			return [undefined, err];
		}

		// debug log removed

		// Receive AnnounceInitMessage
		const rsp = new AnnounceInitMessage({});
		err = await rsp.decode(stream.readable);
		if (err) {
			console.error("moq: failed to receive ANNOUNCE_INIT message:", err);
			return [undefined, err];
		}

		// debug log removed

		return [new AnnouncementReader(this.#ctx, stream, req, rsp), undefined];
	}

	async subscribe(
		path: BroadcastPath,
		name: TrackName,
		config?: TrackConfig,
	): Promise<[TrackReader, undefined] | [undefined, Error]> {
		const subscribeId = this.#subscribeIDCounter++;
		// Check for subscribe ID collision
		if (this.#queues.has(subscribeId)) {
			// Subscribe ID collision, should not happen
			// This is handled as a panic

			throw new Error(
				`moq: subscribe ID duplicate for subscribe ID ${subscribeId}`,
			);
		}
		const [stream, openErr] = await this.#webtransport.openStream();
		if (openErr) {
			console.error("moq: failed to open subscribe stream:", openErr);
			return [undefined, openErr];
		}
		// Send STREAM_TYPE
		let [, err] = await writeVarint(
			stream.writable,
			BiStreamTypes.SubscribeStreamType,
		);
		if (err) {
			console.error("moq: failed to open subscribe stream:", err);
			return [undefined, err];
		}

		// Send SUBSCRIBE message
		const req = new SubscribeMessage({
			subscribeId: subscribeId,
			broadcastPath: path,
			trackName: name,
			trackPriority: config?.trackPriority ?? 0,
		});
		err = await req.encode(stream.writable);
		if (err) {
			console.error("moq: failed to send SUBSCRIBE message:", err);
			return [undefined, err];
		}

		// Add queue for incoming group streams
		const queue = new Queue<[ReceiveStream, GroupMessage]>();
		this.#queues.set(subscribeId, queue);

		const rsp = new SubscribeOkMessage({});
		err = await rsp.decode(stream.readable);
		if (err) {
			console.error("moq: failed to receive SUBSCRIBE_OK message:", err);
			return [undefined, err];
		}

		const subscribeStream = new SendSubscribeStream(
			this.#ctx,
			stream,
			req,
			rsp,
		);

		const track = new TrackReader(
			path,
			name,
			subscribeStream,
			queue,
			() => {
				this.#queues.delete(req.subscribeId);
				queue.close();
			},
		);

		return [track, undefined];
	}

	async #handleGroupStream(reader: ReceiveStream): Promise<void> {
		const req = new GroupMessage({});
		const err = await req.decode(reader);
		if (err) {
			console.error("Failed to decode GroupMessage:", err);
			return;
		}

		// debug log removed

		const queue = this.#queues.get(req.subscribeId);
		if (!queue) {
			// No enqueue function yet.
			// This can happen if the subscribe call is not completed yet.
			return;
		}
		try {
			await queue.enqueue([reader, req]);
		} catch (e) {
			console.error(
				`moq: failed to enqueue group for subscribe ID ${req.subscribeId}:`,
				e,
			);
		}
	}

	async #handleSubscribeStream(stream: Stream): Promise<void> {
		const req = new SubscribeMessage({});
		const reqErr = await req.decode(stream.readable);
		if (reqErr) {
			console.error("Failed to decode SubscribeMessage:", reqErr);
			return;
		}

		const subscribeStream = new ReceiveSubscribeStream(this.#ctx, stream, req);

		const trackWriter = new TrackWriter(
			validateBroadcastPath(req.broadcastPath),
			req.trackName,
			subscribeStream,
			this.#webtransport.openUniStream.bind(this.#webtransport),
		);

		await this.mux.serveTrack(trackWriter);
	}

	async #handleAnnounceStream(stream: Stream): Promise<void> {
		const req = new AnnouncePleaseMessage({});
		const err = await req.decode(stream.readable);
		if (err) {
			console.error("Failed to decode AnnouncePleaseMessage:", err);
			return;
		}

		// debug log removed

		const aw = new AnnouncementWriter(this.#ctx, stream, req);

		await this.mux.serveAnnouncement(aw, aw.prefix);
	}

	async #listenBiStreams(): Promise<void> {
		const pendingHandles: Promise<void>[] = [];
		try {
			// Handle incoming streams
			let num: number;
			let err: Error | undefined;
			while (true) {
				const [stream, acceptErr] = await this.#webtransport.acceptStream();
				// biStreams.releaseLock(); // Release the lock after reading
				if (acceptErr) {
					// Only log as error if session is not closing
					if (!this.#ctx.err()) {
						console.error("Bidirectional stream closed", acceptErr);
					} else {
						// debug log removed
					}
					break;
				}
				[num, , err] = await readVarint(stream.readable);
				if (err) {
					console.error("Failed to read from bidirectional stream:", err);
					continue;
				}

				switch (num) {
					case BiStreamTypes.SubscribeStreamType:
						pendingHandles.push(this.#handleSubscribeStream(stream));
						break;
					case BiStreamTypes.AnnounceStreamType:
						pendingHandles.push(this.#handleAnnounceStream(stream));
						break;
					default:
						console.warn(`Unknown bidirectional stream type: ${num}`);
						break; // Ignore unknown stream types
				}
			}
		} catch (error) {
			// "timed out" errors during connection close are expected
			if (error instanceof Error && error.message === "timed out") {
				// console.debug("listenBiStreams: connection closed (timed out)");
			} else {
				console.error("Error in listenBiStreams:", error);
			}
			return;
		} finally {
			// Wait for all pending handle operations to complete
			if (pendingHandles.length > 0) {
				await Promise.allSettled(pendingHandles);
			}
		}
	}

	async #listenUniStreams(): Promise<void> {
		const pendingHandles: Promise<void>[] = [];
		try {
			let num: number;
			let err: Error | undefined;
			while (true) {
				const [stream, acceptErr] = await this.#webtransport.acceptUniStream();
				if (acceptErr) {
					// Only log as error if session is not closing
					if (!this.#ctx.err()) {
						console.error("Unidirectional stream closed", acceptErr);
					} else {
						// debug log removed
					}
					break;
				}

				// Read the first byte to determine the stream type
				[num, , err] = await readVarint(stream);
				if (err) {
					console.error("Failed to read from unidirectional stream:", err);
					return;
				}

				switch (num) {
					case UniStreamTypes.GroupStreamType:
						pendingHandles.push(this.#handleGroupStream(stream));
						break;
					default:
						console.warn(`Unknown unidirectional stream type: ${num}`);
						break; // Ignore unknown stream types
				}
			}
		} catch (error) {
			// "timed out" errors during connection close are expected
			if (error instanceof Error && error.message === "timed out") {
				// console.debug("listenUniStreams: connection closed (timed out)");
			} else {
				console.error("Error in listenUniStreams:", error);
			}
			return;
		} finally {
			// Wait for all pending handle operations to complete
			if (pendingHandles.length > 0) {
				await Promise.allSettled(pendingHandles);
			}
		}
	}

	async close(): Promise<void> {
		if (this.#ctx.err()) {
			return;
		}

		// Cancel context first to signal shutdown to all listeners
		this.#cancelFunc(new Error("session closing"));

		this.#webtransport.close({
			closeCode: 0x0, // Normal closure
			reason: "No Error",
		});

		try {
			console.log(
				`Session.close: waiting for ${this.#wg.length} background tasks`,
			);
			await Promise.allSettled(this.#wg);
			console.log(`Session.close: background tasks settled`);
		} catch (_e) {
			// ignore
		}
		this.#wg = [];
	}

	async closeWithError(code: number, message: string): Promise<void> {
		if (this.#ctx.err()) {
			return;
		}

		// Cancel context first to signal shutdown to all listeners
		this.#cancelFunc(new Error(message));

		this.#webtransport.close({
			closeCode: code,
			reason: message,
		});

		try {
			console.log(
				`Session.closeWithError: waiting for ${this.#wg.length} background tasks`,
			);
			await Promise.allSettled(this.#wg);
			console.log(`Session.closeWithError: background tasks settled`);
		} catch (_e) {
			// ignore
		}
		this.#wg = [];
	}
}
