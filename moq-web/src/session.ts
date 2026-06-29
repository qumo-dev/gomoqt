import {
	AnnounceInterestMessage,
	FetchMessage,
	GoawayMessage,
	GroupMessage,
	ProbeMessage,
	readVarint,
	SubscribeMessage,
	SubscribeOkMessage,
	writeVarint,
} from "./internal/message/mod.ts";
import { EOFError } from "@okdaichi/golikejs/io";
import {
	ReceiveStream,
	Stream,
	StreamConn,
	StreamConnError,
	StreamConnErrorInfo,
} from "./internal/webtransport/mod.ts";
import { Channel } from "@okdaichi/golikejs";
import { background, withCancelCause } from "@okdaichi/golikejs/context";
import type { CancelCauseFunc, Context } from "@okdaichi/golikejs/context";
import { AnnouncementReader, AnnouncementWriter } from "./announce_stream.ts";
import type { TrackPrefix } from "./track_prefix.ts";
import { ReceiveSubscribeStream, SendSubscribeStream } from "./subscribe_stream.ts";
import type { TrackConfig } from "./subscribe_stream.ts";
import { type BroadcastPath, validateBroadcastPath } from "./broadcast_path.ts";
import { TrackReader } from "./track_reader.ts";
import { TrackWriter } from "./track_writer.ts";
import { GroupReader, GroupWriter } from "./group_stream.ts";
import type { TrackMux } from "./track_mux.ts";
import { DefaultTrackMux } from "./track_mux.ts";
import { BiStreamTypes, UniStreamTypes } from "./stream_type.ts";
import { Queue } from "./internal/queue.ts";
import type { SubscribeID, TrackName } from "./alias.ts";
import { FetchRequest } from "./fetch.ts";
import type { FetchHandler } from "./fetch.ts";
import { FetchErrorCode, GroupErrorCode, ProbeErrorCode, SessionErrorCode } from "./error.ts";
import type { MoqOptions } from "./options.ts";
import { defaultProbeIntervalMs, defaultProbeMaxAgeMs, defaultProbeMaxDelta } from "./options.ts";
import { ProbeResult } from "./probe.ts";

function cancelStreamWithError(stream: Stream, code: number): void {
	stream.readable.cancel(code).catch(() => {});
	stream.writable.cancel(code).catch(() => {});
}

type TransportStats = {
	estimatedSendRate?: number | null;
	smoothedRtt?: number;
	bytesSent?: number;
	bytesReceived?: number;
};

type TransportStatsCapable = {
	getStats?: () => Promise<TransportStats>;
};

/**
 * A snapshot of statistics for a {@link Session}.
 * Fields are 0 when not yet measured or not available.
 */
export interface SessionStats {
	/** Estimated outbound bitrate in bits per second (0 until measured via probe). */
	estimatedBitrate: number;
	/** Smoothed round-trip time in milliseconds (0 when not available). */
	rtt: number;
	/** Total bytes sent on the underlying connection (0 when not available). */
	bytesSent: number;
	/** Total bytes received on the underlying connection (0 when not available). */
	bytesReceived: number;
}

/**
 * Why a {@link Session} closed, in MOQ terms (not the underlying transport's
 * close object). Resolved by {@link Session.closed}.
 */
export interface MOQCloseInfo {
	/**
	 * The MOQ {@link SessionErrorCode}: `NoError` (0) on a graceful close,
	 * the application code on {@link Session.closeWithError}, or a best-effort
	 * code for a peer/transport-initiated close.
	 */
	code: SessionErrorCode;
	/** A short MOQ-side description of the close. */
	reason: string;
}

/** Options for constructing a {@link Session}. */
export interface SessionInit {
	/** The underlying WebTransport (or compatible) stream connection. */
	transport: StreamConn;

	/** {@link TrackMux} for incoming track routing. Defaults to {@link DefaultTrackMux}. */
	mux?: TrackMux;

	/** Handler invoked for incoming fetch requests. */
	fetchHandler?: FetchHandler;

	/** Called when the server requests session migration via GOAWAY. */
	onGoaway?: (newSessionURI: string) => void;

	/** MOQ tuning options (probe intervals, thresholds, etc.). */
	options?: MoqOptions;
}

/**
 * A single MOQ session over a WebTransport connection.
 *
 * Provides methods for publishing, subscribing, announcing, fetching,
 * and probing. Created via {@link Client.dial} or directly with a
 * {@link SessionInit}.
 */
export class Session {
	/** Resolves when the underlying transport is ready. */
	readonly ready: Promise<void>;
	/**
	 * Resolves with a MOQ-level {@link MOQCloseInfo} when the session
	 * terminates — whether via {@link close}, {@link closeWithError}, a
	 * peer-initiated close, or the transport dropping. Never rejects.
	 *
	 * Mirrors Go's `Session.Context().Done()`, giving consumers a single
	 * primitive to await for reconnect/cleanup. The underlying transport's
	 * raw close object is intentionally not exposed.
	 */
	readonly closed: Promise<MOQCloseInfo>;
	#resolveClosed!: (info: MOQCloseInfo) => void;
	#webtransport: StreamConn;
	#ctx: Context;
	#cancelFunc: CancelCauseFunc;

	#wg: Promise<void>[] = [];
	#subscribeIDCounter: number = 0;

	/** The {@link TrackMux} used by this session for incoming track dispatch. */
	readonly mux: TrackMux;
	#fetchHandler?: FetchHandler;
	#onGoaway?: (newSessionURI: string) => void;

	#queues: Map<
		SubscribeID,
		Queue<[ReceiveStream, GroupMessage]>
	> = new Map();

	#outgoingProbeStream?: Stream;
	#outgoingProbeStreamClosed: boolean = false;
	#probeResponseChan: Channel<ProbeResult> = new Channel(1);

	#incomingProbeStream?: Stream;
	#probeTargetsChan: Channel<ProbeResult> = new Channel(1);

	#bitrateTracker: BitrateTracker;

	constructor(options: SessionInit) {
		this.#webtransport = options.transport;
		this.mux = options.mux ?? DefaultTrackMux;
		this.#fetchHandler = options.fetchHandler;
		this.#onGoaway = options.onGoaway;

		this.#bitrateTracker = new BitrateTracker({
			intervalMs: options.options?.probeIntervalMs ?? defaultProbeIntervalMs,
			maxAgeMs: options.options?.probeMaxAgeMs ?? defaultProbeMaxAgeMs,
			maxDelta: options.options?.probeMaxDelta ?? defaultProbeMaxDelta,
		});

		const [ctx, cancel] = withCancelCause(background());
		this.#ctx = ctx;
		this.#cancelFunc = cancel;
		this.closed = new Promise<MOQCloseInfo>((resolve) => {
			this.#resolveClosed = resolve;
		});
		this.ready = this.#setup();

		// Cancel the session context on an involuntary (peer/transport) close
		// and resolve `closed` with a MOQ-level description. A local close()/
		// closeWithError() resolves `closed` first, so its info wins; a later
		// resolve here is a no-op.
		const onTransportClosed = (info: WebTransportCloseInfo): void => {
			const unexpected = info.closeCode === undefined && info.reason === undefined;
			if (!this.#ctx.err()) {
				cancel(
					unexpected
						? new Error("webtransport: connection closed unexpectedly")
						: new StreamConnError(info as StreamConnErrorInfo, true),
				);
			}
			this.#resolveClosed({
				code: unexpected ? SessionErrorCode.InternalError : (info.closeCode ?? 0),
				reason: unexpected ? "connection closed unexpectedly" : "session closed",
			});
		};
		this.#webtransport.closed.then(onTransportClosed, (reason) => {
			if (!this.#ctx.err()) {
				cancel(new Error(String(reason)));
			}
			this.#resolveClosed({
				code: SessionErrorCode.InternalError,
				reason: "connection closed unexpectedly",
			});
		});
	}

	async #setup(): Promise<void> {
		await this.#webtransport.ready;

		// Initialize bitrate tracker baseline after transport is ready
		const transport = this.#webtransport as unknown as TransportStatsCapable;
		if (transport.getStats) {
			const stats = await transport.getStats();
			this.#bitrateTracker.init(stats, Date.now());
		}

		// Start listening for incoming streams
		this.#wg.push(this.#listenBiStreams());
		this.#wg.push(this.#listenUniStreams());

		return;
	}

	/**
	 * Send a target bitrate hint to the publisher and return a channel that
	 * receives measured bitrates reported by the publisher.
	 * Calling `probe` again on the same session updates the target bitrate;
	 * the same {@link AsyncGenerator} is returned on subsequent calls.
	 * The generator ends when the session terminates.
	 *
	 * Mirrors Go's `Session.Probe(targetBitrate uint64) (<-chan ProbeResult, error)`.
	 *
	 * @param targetBitrate - Target bitrate hint in bits per second.
	 * @returns The shared result channel, or an Error if the stream cannot be opened.
	 */
	async probe(
		targetBitrate: number,
	): Promise<[AsyncGenerator<ProbeResult>, undefined] | [undefined, Error]> {
		if (this.#ctx.err()) {
			return [undefined, new Error("session is closing")];
		}

		if (!this.#outgoingProbeStream || this.#outgoingProbeStreamClosed) {
			const [stream, openErr] = await this.#webtransport.openStream();
			if (openErr) {
				console.error("moq: failed to open probe stream:", openErr);
				return [undefined, openErr];
			}

			const [, err] = await writeVarint(stream.writable, BiStreamTypes.ProbeStreamType);
			if (err) {
				console.error("moq: failed to open probe stream:", err);
				cancelStreamWithError(stream, ProbeErrorCode.Internal);
				return [undefined, err];
			}

			this.#outgoingProbeStream = stream;
			this.#outgoingProbeStreamClosed = false;
			this.#readProbeResponses(stream).catch((err) => {
				console.warn("moq: probe stream reader failed:", err);
			});
		}

		const stream = this.#outgoingProbeStream!;
		const req = new ProbeMessage({ bitrate: targetBitrate });
		const err = await req.encode(stream.writable);
		if (err) {
			console.error("moq: failed to send PROBE message:", err);
			cancelStreamWithError(stream, ProbeErrorCode.Internal);
			return [undefined, err];
		}

		return [
			this.#probeResponseChan[Symbol.asyncIterator]() as AsyncGenerator<ProbeResult>,
			undefined,
		];
	}

	/**
	 * Returns a channel that yields the latest target bitrate hints sent by
	 * the subscriber via PROBE messages.
	 * The generator ends when the session terminates.
	 *
	 * Mirrors Go's `Session.ProbeTargets() <-chan ProbeResult`.
	 */
	probeTargets(): AsyncGenerator<ProbeResult> {
		return this.#probeTargetsChan[Symbol.asyncIterator]() as AsyncGenerator<ProbeResult>;
	}

	async #readProbeResponses(stream: Stream): Promise<void> {
		try {
			for (;;) {
				const rsp = new ProbeMessage({});
				const err = await rsp.decode(stream.readable);
				if (err) {
					if (err instanceof EOFError) {
						return;
					}
					throw err;
				}

				this.#bitrateTracker.record(rsp.bitrate, Date.now());

				// Notify any active probe() calls of the new measurement result.
				this.#probeResponseChan.tryReceive(); // drop old
				this.#probeResponseChan.trySend({ bitrate: rsp.bitrate });
			}
		} catch (err) {
			if (!this.#ctx.err()) {
				console.warn(`moq: probe stream error: ${err}`);
				cancelStreamWithError(stream, ProbeErrorCode.Internal);
			}
		} finally {
			this.#outgoingProbeStreamClosed = true;
			if (this.#outgoingProbeStream === stream) {
				this.#outgoingProbeStream = undefined;
			}
		}
	}

	/**
	 * Request announcements matching the given prefix.
	 * @param prefix - Track prefix to filter announcements (e.g. `"/"`)
	 * @returns An {@link AnnouncementReader} that yields matching announcements.
	 */
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

		// Send ANNOUNCE_INTEREST message
		const req = new AnnounceInterestMessage({ prefix });
		err = await req.encode(stream.writable);
		if (err) {
			console.error("moq: failed to send ANNOUNCE_INTEREST message:", err);
			return [undefined, err];
		}

		// debug log removed

		return [new AnnouncementReader(this.#ctx, stream, req), undefined];
	}

	/**
	 * Subscribe to a track and receive its groups.
	 * @param path - Broadcast path (e.g. `"/broadcast"`).
	 * @param name - Track name within the broadcast.
	 * @param config - Optional subscriber-side configuration.
	 * @returns A {@link TrackReader} for consuming groups.
	 */
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
			subscriberPriority: config?.priority ?? 0,
			subscriberOrdered: config?.ordered ? 1 : 0,
			subscriberMaxLatency: config?.maxLatency ?? 0,
			startGroup: config?.startGroup ? config.startGroup + 1 : 0,
			endGroup: config?.endGroup ? config.endGroup + 1 : 0,
		});
		err = await req.encode(stream.writable);
		if (err) {
			console.error("moq: failed to send SUBSCRIBE message:", err);
			return [undefined, err];
		}

		// Add queue for incoming group streams
		const queue = new Queue<[ReceiveStream, GroupMessage]>();
		this.#queues.set(subscribeId, queue);

		// Read the type byte for the first response
		const [msgType, , typeErr] = await readVarint(stream.readable);
		if (typeErr) {
			console.error("moq: failed to read SUBSCRIBE response type:", typeErr);
			return [undefined, typeErr];
		}
		if (msgType !== 0x0) {
			const respErr = new Error(`moq: unexpected first SUBSCRIBE response type: ${msgType}`);
			console.error(respErr.message);
			return [undefined, respErr];
		}

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

		// Start background reading of subscribe responses (Ok updates, Drops)
		subscribeStream.readSubscribeResponses();

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

	async fetch(
		req: FetchRequest,
	): Promise<[GroupReader, undefined] | [undefined, Error]> {
		const [stream, openErr] = await this.#webtransport.openStream();
		if (openErr) {
			console.error("moq: failed to open fetch stream:", openErr);
			return [undefined, openErr];
		}

		// Send STREAM_TYPE
		let [, err] = await writeVarint(
			stream.writable,
			BiStreamTypes.FetchStreamType,
		);
		if (err) {
			console.error("moq: failed to write fetch stream type:", err);
			return [undefined, err];
		}

		// Send FETCH message
		const msg = new FetchMessage({
			broadcastPath: req.broadcastPath,
			trackName: req.trackName,
			priority: req.priority,
			groupSequence: req.groupSequence,
		});
		err = await msg.encode(stream.writable);
		if (err) {
			console.error("moq: failed to encode FETCH message:", err);
			return [undefined, err];
		}

		const group = new GroupReader(
			this.#ctx,
			stream.readable,
			new GroupMessage({ sequence: req.groupSequence }),
		);

		// Cancel the group when the request is done
		req.done().then(() => {
			group.cancel(GroupErrorCode.ExpiredGroup);
		}).catch(() => {});

		return [group, undefined];
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
		const req = new AnnounceInterestMessage({});
		const err = await req.decode(stream.readable);
		if (err) {
			console.error("Failed to decode AnnounceInterestMessage:", err);
			return;
		}

		// debug log removed

		const aw = new AnnouncementWriter(this.#ctx, stream, req);

		await this.mux.serveAnnouncement(aw, aw.prefix);
	}

	async #handleProbeStream(stream: Stream): Promise<void> {
		const quic = this.#webtransport as unknown as TransportStatsCapable;

		if (this.#incomingProbeStream && this.#incomingProbeStream !== stream) {
			cancelStreamWithError(this.#incomingProbeStream, ProbeErrorCode.Internal);
		}
		this.#incomingProbeStream = stream;

		if (quic.getStats) {
			this.#bitrateTracker.monitor(this.#ctx, quic, async (bitrate, rtt) => {
				const rsp = new ProbeMessage({ bitrate, rtt });
				const err = await rsp.encode(stream.writable);
				if (err) {
					cancelStreamWithError(stream, ProbeErrorCode.Internal);
					return;
				}
			}).catch((err) => {
				console.warn(`moq: probe detection failed: ${err}`);
			});
		}

		try {
			for (;;) {
				const req = new ProbeMessage({});
				const err = await req.decode(stream.readable);
				if (err) {
					if (err instanceof EOFError) {
						return;
					}
					throw err;
				}

				// Notify publisher-side consumers of the new target bitrate.
				this.#probeTargetsChan.tryReceive(); // drop old
				this.#probeTargetsChan.trySend({ bitrate: req.bitrate });

				let bitrate = 0;
				if (quic.getStats) {
					const stats = await quic.getStats();
					bitrate = stats.estimatedSendRate ?? 0;
				}

				const rsp = new ProbeMessage({ bitrate, rtt: req.rtt });
				const encErr = await rsp.encode(stream.writable);
				if (encErr) {
					throw encErr;
				}
			}
		} catch (err) {
			if (!this.#ctx.err()) {
				console.warn(`moq: probe stream error: ${err}`);
				cancelStreamWithError(stream, ProbeErrorCode.Internal);
			}
		} finally {
			if (this.#incomingProbeStream === stream) {
				this.#incomingProbeStream = undefined;
			}
		}
	}

	async #handleFetchStream(stream: Stream): Promise<void> {
		const handler = this.#fetchHandler;
		if (!handler) {
			cancelStreamWithError(stream, FetchErrorCode.InternalError);
			return;
		}

		const fm = new FetchMessage({});
		const err = await fm.decode(stream.readable);
		if (err) {
			console.error("Failed to decode FetchMessage:", err);
			cancelStreamWithError(stream, FetchErrorCode.InternalError);
			return;
		}

		const [fetchCtx, cancelFetch] = withCancelCause(this.#ctx);

		const req = new FetchRequest({
			broadcastPath: validateBroadcastPath(fm.broadcastPath),
			trackName: fm.trackName,
			priority: fm.priority,
			groupSequence: fm.groupSequence,
			done: fetchCtx.done(),
		});

		const group = new GroupWriter(
			fetchCtx,
			stream.writable,
			new GroupMessage({ sequence: fm.groupSequence }),
		);

		try {
			await handler.serveFetch(group, req);
		} catch (e) {
			console.error("moq: fetch handler error:", e);
			await group.cancel(FetchErrorCode.InternalError).catch(() => {});
		} finally {
			cancelFetch(undefined);
		}
	}

	async #handleGoawayStream(stream: Stream): Promise<void> {
		const gm = new GoawayMessage({});
		const err = await gm.decode(stream.readable);
		if (err) {
			console.error("Failed to decode GoawayMessage:", err);
			return;
		}

		if (this.#onGoaway) {
			this.#onGoaway(gm.newSessionURI);
		}
	}

	async #listenBiStreams(): Promise<void> {
		const pendingHandles: Promise<void>[] = [];
		try {
			// Handle incoming streams
			let num: number;
			let err: Error | undefined;
			while (true) {
				const [stream, acceptErr] = await this.#webtransport.acceptStream();
				if (acceptErr) {
					// Only log as error if session is not closing
					if (!this.#ctx.err()) {
						console.error("Bidirectional stream closed", acceptErr);
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
					case BiStreamTypes.FetchStreamType:
						pendingHandles.push(this.#handleFetchStream(stream));
						break;
					case BiStreamTypes.ProbeStreamType:
						pendingHandles.push(this.#handleProbeStream(stream));
						break;
					case BiStreamTypes.GoawayStreamType:
						pendingHandles.push(this.#handleGoawayStream(stream));
						break;
					default:
						cancelStreamWithError(stream, SessionErrorCode.InternalError);
						break;
				}
			}
		} catch (error) {
			if (error instanceof Error && error.message === "timed out") {
				// expected
			} else {
				console.error("Error in listenBiStreams:", error);
			}
			return;
		} finally {
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
					if (!this.#ctx.err()) {
						console.error("Unidirectional stream closed", acceptErr);
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
						stream.cancel(SessionErrorCode.InternalError).catch(() => {});
						break;
				}
			}
		} catch (error) {
			if (error instanceof Error && error.message === "timed out") {
				// expected
			} else {
				console.error("Error in listenUniStreams:", error);
			}
			return;
		} finally {
			if (pendingHandles.length > 0) {
				await Promise.allSettled(pendingHandles);
			}
		}
	}

	/**
	 * Returns a snapshot of current session statistics.
	 *
	 * Mirrors Go's `Session.Stats() SessionStats`.
	 * RTT, bytes sent/received are populated from the underlying transport's
	 * `getStats()` when available (standard WebTransport API); all fields
	 * default to `0` when not yet measured or not supported.
	 *
	 * @returns A {@link SessionStats} snapshot.
	 */
	async getStats(): Promise<SessionStats> {
		const stats: SessionStats = {
			estimatedBitrate: this.#bitrateTracker.estimatedBitrate,
			rtt: 0,
			bytesSent: 0,
			bytesReceived: 0,
		};

		const transport = this.#webtransport as unknown as TransportStatsCapable;
		if (transport.getStats) {
			const wtStats = await transport.getStats();
			stats.rtt = wtStats.smoothedRtt ?? 0;
			stats.bytesSent = wtStats.bytesSent ?? 0;
			stats.bytesReceived = wtStats.bytesReceived ?? 0;
		}

		return stats;
	}

	/** Gracefully close the session. */
	async close(): Promise<void> {
		if (this.#ctx.err()) {
			return;
		}

		// Cancel context first to signal shutdown to all listeners
		this.#cancelFunc(new Error("session closing"));
		this.#resolveClosed({ code: SessionErrorCode.NoError, reason: "No Error" });

		this.#webtransport.close({
			closeCode: 0x0, // Normal closure
			reason: "No Error",
		});

		if (this.#incomingProbeStream) {
			cancelStreamWithError(this.#incomingProbeStream, ProbeErrorCode.Internal);
		}
		if (this.#outgoingProbeStream) {
			cancelStreamWithError(this.#outgoingProbeStream, ProbeErrorCode.Internal);
		}

		this.#probeResponseChan.close();
		this.#probeTargetsChan.close();

		try {
			await Promise.allSettled(this.#wg);
		} catch (_e) {
			// ignore
		}
		this.#wg = [];
	}

	/**
	 * Close the session with an application-level error.
	 * @param code - Error code sent to the peer.
	 * @param message - Human-readable reason.
	 */
	async closeWithError(code: number, message: string): Promise<void> {
		if (this.#ctx.err()) {
			return;
		}

		// Cancel context first to signal shutdown to all listeners
		this.#cancelFunc(new Error(message));
		this.#resolveClosed({ code, reason: message });

		this.#webtransport.close({
			closeCode: code,
			reason: message,
		});

		if (this.#incomingProbeStream) {
			cancelStreamWithError(this.#incomingProbeStream, ProbeErrorCode.Internal);
		}
		if (this.#outgoingProbeStream) {
			cancelStreamWithError(this.#outgoingProbeStream, ProbeErrorCode.Internal);
		}

		this.#probeResponseChan.close();
		this.#probeTargetsChan.close();

		try {
			await Promise.allSettled(this.#wg);
		} catch (_e) {
			// ignore
		}
		this.#wg = [];
	}
}

export interface BitrateTrackerConfig {
	intervalMs: number;
	maxAgeMs: number;
	maxDelta: number;
}

class BitrateTracker {
	#intervalMs: number;
	#maxAgeMs: number;
	#maxDelta: number;

	#initialized = false;
	#bytesSent = 0;
	#sampleTime = 0;
	#estimatedBitrate = 0;
	#lastSentBitrate = 0;

	#lastSentAt = 0;

	constructor(config: BitrateTrackerConfig) {
		this.#intervalMs = config.intervalMs;
		this.#maxAgeMs = config.maxAgeMs;
		this.#maxDelta = config.maxDelta;
	}

	get estimatedBitrate(): number {
		return this.#estimatedBitrate;
	}

	set estimatedBitrate(value: number) {
		this.#estimatedBitrate = value;
	}

	init(stats: TransportStats, now: number): void {
		this.#initialized = true;
		this.#bytesSent = stats.bytesSent ?? 0;
		this.#sampleTime = now;
		if (stats.estimatedSendRate != null) {
			this.#estimatedBitrate = stats.estimatedSendRate;
		}
	}

	record(bitrate: number, now: number): void {
		this.#estimatedBitrate = bitrate;
		this.#lastSentBitrate = bitrate;
		this.#lastSentAt = now;
	}

	async monitor(
		ctx: Context,
		quic: TransportStatsCapable,
		onProbe: (bitrate: number, rtt: number) => Promise<void>,
	): Promise<void> {
		if (!quic.getStats) return;

		while (true) {
			if (ctx.err()) {
				return;
			}

			const stats = await quic.getStats();
			const now = Date.now();
			const [bitrate, ok] = this.next(stats, now);

			if (ok) {
				await onProbe(bitrate, stats.smoothedRtt ? Math.floor(stats.smoothedRtt) : 0);
			}

			await new Promise((resolve) => setTimeout(resolve, this.#intervalMs));
		}
	}

	next(stats: TransportStats, now: number): [number, boolean] {
		const bitrate = this.measureBitrate(stats, now);

		if (this.#lastSentAt === 0) {
			this.record(bitrate, now);
			return [bitrate, true];
		}

		if (
			now - this.#lastSentAt >= this.#maxAgeMs ||
			this.#hasDelta(this.#lastSentBitrate, bitrate, this.#maxDelta)
		) {
			this.record(bitrate, now);
			return [bitrate, true];
		}

		return [bitrate, false];
	}

	measureBitrate(stats: TransportStats, now: number): number {
		// Prefer estimatedSendRate if provided (e.g. standard WebTransport)
		if (stats.estimatedSendRate != null) {
			this.#estimatedBitrate = stats.estimatedSendRate;
		}

		if (stats.bytesSent === undefined) {
			return this.#estimatedBitrate;
		}

		if (!this.#initialized) {
			this.init(stats, now);
			return this.#estimatedBitrate;
		}

		const elapsed = (now - this.#sampleTime) / 1000;
		if (elapsed <= 0) {
			return this.#estimatedBitrate;
		}

		const bytesSent = stats.bytesSent;
		let bytesDelta = 0;
		if (bytesSent >= this.#bytesSent) {
			bytesDelta = bytesSent - this.#bytesSent;
		}
		this.#bytesSent = bytesSent;
		this.#sampleTime = now;

		// Only update #estimatedBitrate from bytes delta if estimatedSendRate was NOT provided
		if (stats.estimatedSendRate == null) {
			this.#estimatedBitrate = Math.floor((bytesDelta * 8) / elapsed);
		}
		return this.#estimatedBitrate;
	}

	#hasDelta(oldVal: number, newVal: number, maxDelta: number): boolean {
		if (oldVal === 0) {
			return newVal !== 0;
		}
		const diff = Math.abs(newVal - oldVal);
		return diff / oldVal >= maxDelta;
	}
}
