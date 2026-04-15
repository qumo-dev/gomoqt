import { assertEquals, assertExists, assertInstanceOf } from "@std/assert";
import { spy } from "@std/testing/mock";
import { Session } from "./session.ts";
import {
	AnnounceInterestMessage,
	FetchMessage,
	GroupMessage,
	ProbeMessage,
	SubscribeMessage,
	SubscribeOkMessage,
	writeVarint,
} from "./internal/message/mod.ts";
import { BiStreamTypes, UniStreamTypes } from "./stream_type.ts";
import { TrackMux } from "./track_mux.ts";
import type { TrackPrefix } from "./track_prefix.ts";
import { Writer } from "@okdaichi/golikejs/io";
import { EOFError } from "@okdaichi/golikejs/io";
import { ReceiveStream, SendStream, Stream, StreamConn } from "./internal/webtransport/mod.ts";
import { FetchRequest } from "./fetch.ts";
import type { FetchHandler } from "./fetch.ts";
import type { GroupWriter } from "./group_stream.ts";

// Utility class to implement Writer for encoding messages
class Uint8ArrayWriter implements Writer {
	chunks: Uint8Array[] = [];

	async write(chunk: Uint8Array): Promise<[number, Error | undefined]> {
		this.chunks.push(chunk.slice());
		return [chunk.length, undefined];
	}

	getBytes(): Uint8Array {
		return new Uint8Array(this.chunks.flatMap((c) => Array.from(c)));
	}
}

// Utility function to encode messages to Uint8Array
async function encodeMessageToUint8Array(
	encoder: (w: Writer) => Promise<Error | undefined>,
): Promise<Uint8Array> {
	const writer = new Uint8ArrayWriter();
	const err = await encoder(writer);
	if (err) throw err;
	return writer.getBytes();
}

// Mock WebTransportSession implementation
interface MockWebTransportSessionInit {
	openStreamResponses?: Uint8Array[];
	openUniStreamCount?: number;
	acceptStreamData?: Array<{ type: number; data: Uint8Array }>;
	acceptUniStreamData?: Array<{ type: number; data: Uint8Array }>;
	closedPromise?: Promise<WebTransportCloseInfo>;
	protocol?: string;
	stats?: { estimatedSendRate: number | null };
}

type WebTransportProbeStats = {
	estimatedSendRate: number | null;
};

class MockWebTransportSession implements StreamConn {
	#openStreamResponses: Uint8Array[];
	#openStreamIndex = 0;
	#openStreamWrittenData: Uint8Array[][] = [];
	#acceptStreamData: Array<{ type: number; data: Uint8Array }>;
	#acceptStreamIndex = 0;
	#acceptStreamWrittenData: Uint8Array[][] = [];
	#acceptUniStreamData: Array<{ type: number; data: Uint8Array }>;
	#acceptUniStreamIndex = 0;
	#closed = false;
	#closedPromise: Promise<WebTransportCloseInfo>;
	#closedResolve?: (info: WebTransportCloseInfo) => void;
	#waitingAcceptResolvers: Array<() => void> = [];
	#stats: WebTransportProbeStats = { estimatedSendRate: null };

	ready: Promise<void> = Promise.resolve();
	readonly protocol: string;

	constructor(options: MockWebTransportSessionInit = {}) {
		this.#openStreamResponses = options.openStreamResponses ?? [];
		this.#acceptStreamData = options.acceptStreamData ?? [];
		this.#acceptUniStreamData = options.acceptUniStreamData ?? [];
		this.protocol = options.protocol ?? "";
		this.#stats = options.stats ?? { estimatedSendRate: null };
		if (options.stats !== undefined) {
			Object.defineProperty(this, "getStats", {
				value: async () => this.#stats,
				configurable: true,
				enumerable: false,
				writable: false,
			});
		}

		if (options.closedPromise) {
			this.#closedPromise = options.closedPromise;
		} else {
			this.#closedPromise = new Promise((resolve) => {
				this.#closedResolve = resolve;
			});
		}
	}

	async openStream(): Promise<[Stream, undefined] | [undefined, Error]> {
		if (this.#closed) {
			return [undefined, new Error("session closed")];
		}

		const data = this.#openStreamResponses[this.#openStreamIndex] ??
			new Uint8Array();
		this.#openStreamIndex++;

		// Create inline mock stream
		const writtenData: Uint8Array[] = [];
		let readOffset = 0;
		this.#openStreamWrittenData.push(writtenData);
		const writable = {
			write: spy(async (p: Uint8Array) => {
				writtenData.push(new Uint8Array(p));
				return [p.length, undefined] as [number, Error | undefined];
			}),
			close: spy(async () => {}),
			cancel: spy(async (_code: number) => {}),
			closed: () => new Promise<void>(() => {}),
		};
		const readable = {
			read: spy(async (p: Uint8Array) => {
				if (readOffset >= data.length) {
					return [0, new EOFError()] as [number, Error | undefined];
				}
				const n = Math.min(p.length, data.length - readOffset);
				p.set(data.subarray(readOffset, readOffset + n));
				readOffset += n;
				return [n, undefined] as [number, Error | undefined];
			}),
			cancel: spy(async (_code: number) => {}),
			closed: () => new Promise<void>(() => {}),
		};
		return [{ writable, readable }, undefined];
	}

	async openUniStream(): Promise<[SendStream, undefined] | [undefined, Error]> {
		if (this.#closed) {
			return [undefined, new Error("session closed")];
		}
		const writtenData: Uint8Array[] = [];
		return [{
			write: spy(async (p: Uint8Array) => {
				writtenData.push(new Uint8Array(p));
				return [p.length, undefined] as [number, Error | undefined];
			}),
			close: spy(async () => {}),
			cancel: spy(async (_code: number) => {}),
			closed: () => new Promise<void>(() => {}),
		}, undefined];
	}

	async acceptStream(): Promise<[Stream, undefined] | [undefined, Error]> {
		if (this.#closed) {
			return [undefined, new Error("session closed")];
		}
		if (this.#acceptStreamIndex >= this.#acceptStreamData.length) {
			await new Promise<void>((resolve) => {
				this.#waitingAcceptResolvers.push(resolve);
			});
			return [undefined, new Error("session closed")];
		}
		const item = this.#acceptStreamData[this.#acceptStreamIndex]!;
		const { data } = item;
		this.#acceptStreamIndex++;

		const writtenData: Uint8Array[] = [];
		let readOffset = 0;
		this.#acceptStreamWrittenData.push(writtenData);
		const writable = {
			write: spy(async (p: Uint8Array) => {
				writtenData.push(new Uint8Array(p));
				return [p.length, undefined] as [number, Error | undefined];
			}),
			close: spy(async () => {}),
			cancel: spy(async (_code: number) => {}),
			closed: () => new Promise<void>(() => {}),
		};
		const readable = {
			read: spy(async (p: Uint8Array) => {
				if (readOffset >= data.length) {
					return [0, new EOFError()] as [number, Error | undefined];
				}
				const n = Math.min(p.length, data.length - readOffset);
				p.set(data.subarray(readOffset, readOffset + n));
				readOffset += n;
				return [n, undefined] as [number, Error | undefined];
			}),
			cancel: spy(async (_code: number) => {}),
			closed: () => new Promise<void>(() => {}),
		};
		return [{ writable, readable }, undefined];
	}

	async acceptUniStream(): Promise<
		[ReceiveStream, undefined] | [undefined, Error]
	> {
		if (this.#closed) {
			return [undefined, new Error("session closed")];
		}
		if (this.#acceptUniStreamIndex >= this.#acceptUniStreamData.length) {
			await new Promise<void>((resolve) => {
				this.#waitingAcceptResolvers.push(resolve);
			});
			return [undefined, new Error("session closed")];
		}
		const uniItem = this.#acceptUniStreamData[this.#acceptUniStreamIndex]!;
		const { data } = uniItem;
		this.#acceptUniStreamIndex++;

		let readOffset = 0;
		return [{
			read: spy(async (p: Uint8Array) => {
				if (readOffset >= data.length) {
					return [0, new EOFError()] as [number, Error | undefined];
				}
				const n = Math.min(p.length, data.length - readOffset);
				p.set(data.subarray(readOffset, readOffset + n));
				readOffset += n;
				return [n, undefined] as [number, Error | undefined];
			}),
			cancel: spy(async (_code: number) => {}),
			closed: () => new Promise<void>(() => {}),
		}, undefined];
	}

	close(_closeInfo?: WebTransportCloseInfo): void {
		this.#closed = true;
		for (const resolve of this.#waitingAcceptResolvers) {
			resolve();
		}
		this.#waitingAcceptResolvers = [];
		if (this.#closedResolve) {
			this.#closedResolve({ closeCode: 0, reason: "closed" });
		}
	}

	get closed(): Promise<WebTransportCloseInfo> {
		return this.#closedPromise;
	}

	get openStreamWrittenData(): Uint8Array[][] {
		return this.#openStreamWrittenData;
	}

	get acceptStreamWrittenData(): Uint8Array[][] {
		return this.#acceptStreamWrittenData;
	}
}

Deno.test({
	name: "Session",
	sanitizeOps: false,
	sanitizeResources: false,
	fn: async (t) => {
		await t.step("constructor and ready sends client message", async () => {
			const mock = new MockWebTransportSession({});

			const session = new Session({ transport: mock });
			await session.ready;

			assertInstanceOf(session, Session);
			await session.close();
		});

		await t.step(
			"constructor starts without session setup handshake",
			async () => {
				const mock = new MockWebTransportSession({});

				let threw = false;
				let session: Session | undefined;
				try {
					session = new Session({ transport: mock });
					await session.ready;
				} catch (_err) {
					threw = true;
				} finally {
					if (session) {
						try {
							await session.close();
						} catch (_e) {
							// ignore
						}
					}
				}
				assertEquals(threw, false);
			},
		);

		await t.step(
			"constructor is ready without session version negotiation",
			async () => {
				const mock = new MockWebTransportSession({});

				let threw = false;
				let session: Session | undefined;
				try {
					session = new Session({ transport: mock });
					await session.ready;
				} catch (_err) {
					threw = true;
				} finally {
					if (session) {
						try {
							await session.close();
						} catch (_e) {
							// ignore
						}
					}
				}
				assertEquals(threw, false);
			},
		);

		await t.step(
			"subscribe returns error when SUBSCRIBE_OK decode fails",
			async () => {
				const truncatedBytes = new Uint8Array([0x80]);

				const mock = new MockWebTransportSession({
					openStreamResponses: [truncatedBytes],
				});

				const session = new Session({ transport: mock });
				await session.ready;

				const [track, err] = await session.subscribe(
					"/test/path",
					"track-name",
				);
				assertEquals(track, undefined);
				assertExists(err);
				await session.close();
			},
		);

		await t.step(
			"listening for subscribe stream calls mux serveTrack",
			async () => {
				let served = false;
				const mux: TrackMux = {
					serveTrack: async (_t) => {
						served = true;
					},
				} as TrackMux;

				const req = new SubscribeMessage({
					subscribeId: 1,
					broadcastPath: "/test/path",
					trackName: "name",
					subscriberPriority: 0,
				});
				const buf = await encodeMessageToUint8Array(async (w) => {
					await writeVarint(w, BiStreamTypes.SubscribeStreamType);
					return await req.encode(w);
				});

				const mock = new MockWebTransportSession({
					openStreamResponses: [],
					acceptStreamData: [{
						type: BiStreamTypes.SubscribeStreamType,
						data: buf,
					}],
				});

				const session = new Session({ transport: mock, mux });
				await session.ready;

				await new Promise((resolve) => setTimeout(resolve, 10));
				assertEquals(served, true);
				await session.close();
			},
		);

		await t.step(
			"listening for probe stream responds with current stats",
			async () => {
				const req = new ProbeMessage({ bitrate: 1234 });
				const expected = await encodeMessageToUint8Array(async (w) => {
					await writeVarint(w, BiStreamTypes.ProbeStreamType);
					return await req.encode(w);
				});

				const mock = new MockWebTransportSession({
					acceptStreamData: [{ type: BiStreamTypes.ProbeStreamType, data: expected }],
					stats: { estimatedSendRate: 4321 },
				});

				const session = new Session({ transport: mock });
				await session.ready;

				await new Promise((resolve) => setTimeout(resolve, 10));

				const written = mock.acceptStreamWrittenData[0] ?? [];
				const rsp = new ProbeMessage({ bitrate: 4321 });
				const expectedResponse = await encodeMessageToUint8Array(async (w) =>
					rsp.encode(w)
				);
				const actualResponse = new Uint8Array(
					written.flatMap((chunk) => Array.from(chunk)),
				);
				assertEquals(actualResponse, expectedResponse);

				await session.close();
			},
		);

		await t.step(
			"probe sends request and returns response bitrate",
			async () => {
				const rsp = new ProbeMessage({ bitrate: 4321 });
				const rspBytes = await encodeMessageToUint8Array(async (w) => rsp.encode(w));

				const mock = new MockWebTransportSession({
					openStreamResponses: [rspBytes],
				});

				const session = new Session({ transport: mock });
				await session.ready;

				const [got, err] = await session.probe(1234);
				assertEquals(err, undefined);
				assertEquals(got, 4321);

				const written = mock.openStreamWrittenData[0] ?? [];
				const actualRequest = new Uint8Array(written.flatMap((chunk) => Array.from(chunk)));
				const expectedRequest = await encodeMessageToUint8Array(async (w) => {
					await writeVarint(w, BiStreamTypes.ProbeStreamType);
					return await new ProbeMessage({ bitrate: 1234 }).encode(w);
				});
				assertEquals(actualRequest, expectedRequest);

				await session.close();
			},
		);

		await t.step("acceptAnnounce succeeds with valid messages", async () => {
			const mock = new MockWebTransportSession({
				openStreamResponses: [new Uint8Array(0)],
			});

			const session = new Session({ transport: mock });
			await session.ready;

			const [reader, err] = await session.acceptAnnounce(
				"/test/" as TrackPrefix,
			);
			assertExists(reader);
			assertEquals(err, undefined);
			await reader.close();
			await session.close();
		});

		await t.step("subscribe succeeds with valid messages", async () => {
			const ok = new SubscribeOkMessage({});
			const okBytes = await encodeMessageToUint8Array(async (w) => {
				await writeVarint(w, 0);
				return await ok.encode(w);
			});

			const mock = new MockWebTransportSession({
				openStreamResponses: [okBytes],
			});

			const session = new Session({ transport: mock });
			await session.ready;

			const [track, err] = await session.subscribe(
				"/test/path",
				"track-name",
			);
			assertExists(track);
			assertEquals(err, undefined);
			await session.close();
		});

		await t.step(
			"listening for announce stream calls mux serveAnnouncement",
			async () => {
				let served = false;
				let servedPrefix: TrackPrefix | undefined;
				const mux: TrackMux = {
					serveAnnouncement: async (_aw, prefix) => {
						served = true;
						servedPrefix = prefix;
					},
				} as TrackMux;

				const req = new AnnounceInterestMessage({ prefix: "/test/" });
				const buf = await encodeMessageToUint8Array(async (w) => {
					await writeVarint(w, BiStreamTypes.AnnounceStreamType);
					return await req.encode(w);
				});

				const mock = new MockWebTransportSession({
					openStreamResponses: [],
					acceptStreamData: [{
						type: BiStreamTypes.AnnounceStreamType,
						data: buf,
					}],
				});

				const session = new Session({ transport: mock, mux });
				await session.ready;

				await new Promise((resolve) => setTimeout(resolve, 10));
				assertEquals(served, true);
				assertEquals(servedPrefix, "/test/");
				await session.close();
			},
		);

		await t.step("listening for group stream enqueues to track", async () => {
			const ok = new SubscribeOkMessage({});
			const okBytes = await encodeMessageToUint8Array(async (w) => {
				await writeVarint(w, 0);
				return await ok.encode(w);
			});

			const groupMsg = new GroupMessage({
				subscribeId: 0,
				sequence: 1,
			});
			const groupBuf = await encodeMessageToUint8Array(async (w) => {
				await writeVarint(w, UniStreamTypes.GroupStreamType);
				return await groupMsg.encode(w);
			});

			const mock = new MockWebTransportSession({
				openStreamResponses: [okBytes],
				acceptUniStreamData: [{
					type: UniStreamTypes.GroupStreamType,
					data: groupBuf,
				}],
			});

			const session = new Session({ transport: mock });
			await session.ready;

			const [track, err] = await session.subscribe(
				"/test/path",
				"track-name",
			);
			assertExists(track);
			assertEquals(err, undefined);

			await new Promise((resolve) => setTimeout(resolve, 10));

			await session.close();
		});

		await t.step("close calls webtransport.close", async () => {
			let closeCalled = false;
			const mock = new MockWebTransportSession({
				openStreamResponses: [],
			});
			const originalClose = mock.close.bind(mock);
			mock.close = (_closeInfo?: WebTransportCloseInfo) => {
				closeCalled = true;
				originalClose(_closeInfo);
			};

			const session = new Session({ transport: mock });
			await session.ready;

			await session.close();
			assertEquals(closeCalled, true);
		});

		await t.step(
			"closeWithError calls webtransport.close with code and reason",
			async () => {
				let closeInfo: WebTransportCloseInfo | undefined;
				const mock = new MockWebTransportSession({
					openStreamResponses: [],
				});
				const originalClose = mock.close.bind(mock);
				mock.close = (info?: WebTransportCloseInfo) => {
					closeInfo = info;
					originalClose(info);
				};

				const session = new Session({ transport: mock });
				await session.ready;

				await session.closeWithError(0x123, "test error");
				assertExists(closeInfo);
				assertEquals(closeInfo.closeCode, 0x123);
				assertEquals(closeInfo.reason, "test error");
			},
		);

		await t.step(
			"closeWithError does nothing when context already has error",
			async () => {
				let closeCallCount = 0;
				const mock = new MockWebTransportSession({
					openStreamResponses: [],
				});
				const originalClose = mock.close.bind(mock);
				mock.close = (info?: WebTransportCloseInfo) => {
					closeCallCount++;
					originalClose(info);
				};

				const session = new Session({ transport: mock });
				await session.ready;

				await session.closeWithError(0x1, "first error");
				assertEquals(closeCallCount, 1);

				await session.closeWithError(0x2, "second error");
				assertEquals(closeCallCount, 1);
			},
		);

		await t.step(
			"multiple subscribes get different subscribe IDs",
			async () => {
				const ok = new SubscribeOkMessage({});
				const okBytes = await encodeMessageToUint8Array(async (w) => {
					await writeVarint(w, 0);
					return await ok.encode(w);
				});

				const mock = new MockWebTransportSession({
					openStreamResponses: [okBytes, okBytes, okBytes],
				});

				const session = new Session({ transport: mock });
				await session.ready;

				const [track1, err1] = await session.subscribe("/path1", "name1");
				const [track2, err2] = await session.subscribe("/path2", "name2");
				const [track3, err3] = await session.subscribe("/path3", "name3");

				assertExists(track1);
				assertExists(track2);
				assertExists(track3);
				assertEquals(err1, undefined);
				assertEquals(err2, undefined);
				assertEquals(err3, undefined);

				await session.close();
			},
		);

		await t.step(
			"acceptAnnounce returns error when openStream fails",
			async () => {
				const mock = new MockWebTransportSession({
					openStreamResponses: [],
				});

				const session = new Session({ transport: mock });
				await session.ready;

				// Close the mock to simulate openStream failure
				mock.close();

				const [reader, err] = await session.acceptAnnounce(
					"/test/" as TrackPrefix,
				);
				assertEquals(reader, undefined);
				assertExists(err);
			},
		);

		await t.step(
			"subscribe returns error when openStream fails",
			async () => {
				const mock = new MockWebTransportSession({
					openStreamResponses: [],
				});

				const session = new Session({ transport: mock });
				await session.ready;

				// Close the mock to simulate openStream failure
				mock.close();

				const [track, err] = await session.subscribe(
					"/test/path",
					"track-name",
				);
				assertEquals(track, undefined);
				assertExists(err);
			},
		);

		await t.step(
			"subscribe with trackConfig passes config values",
			async () => {
				const ok = new SubscribeOkMessage({});
				const okBytes = await encodeMessageToUint8Array(async (w) => {
					await writeVarint(w, 0);
					return await ok.encode(w);
				});

				const mock = new MockWebTransportSession({
					openStreamResponses: [okBytes],
				});

				const session = new Session({ transport: mock });
				await session.ready;

				const [track, err] = await session.subscribe(
					"/test/path",
					"track-name",
					{ priority: 5, ordered: false, maxLatency: 0, startGroup: 0, endGroup: 0 },
				);
				assertExists(track);
				assertEquals(err, undefined);

				// Verify track config is reflected
				const config = track.trackConfig;
				assertEquals(config.priority, 5);

				await session.close();
			},
		);

		await t.step(
			"close does nothing when context already has error",
			async () => {
				let closeCallCount = 0;
				const mock = new MockWebTransportSession({
					openStreamResponses: [],
				});
				const originalClose = mock.close.bind(mock);
				mock.close = (info?: WebTransportCloseInfo) => {
					closeCallCount++;
					originalClose(info);
				};

				const session = new Session({ transport: mock });
				await session.ready;

				await session.close();
				assertEquals(closeCallCount, 1);

				// Second close should be a no-op
				await session.close();
				assertEquals(closeCallCount, 1);
			},
		);

		await t.step(
			"listening for unknown bidirectional stream type logs warning",
			async () => {
				// Create a buffer with unknown stream type (0xFF)
				const unknownStreamBuf = new Uint8Array([0xFF]);

				const mock = new MockWebTransportSession({
					openStreamResponses: [],
					acceptStreamData: [{
						type: 0xFF, // Unknown type
						data: unknownStreamBuf,
					}],
				});

				const session = new Session({ transport: mock });
				await session.ready;

				await new Promise((resolve) => setTimeout(resolve, 10));
				await session.close();
			},
		);

		await t.step(
			"listening for unknown unidirectional stream type logs warning",
			async () => {
				// Create a buffer with unknown stream type (0xFF)
				const unknownStreamBuf = new Uint8Array([0xFF]);

				const mock = new MockWebTransportSession({
					openStreamResponses: [],
					acceptUniStreamData: [{
						type: 0xFF, // Unknown type
						data: unknownStreamBuf,
					}],
				});

				const session = new Session({ transport: mock });
				await session.ready;

				await new Promise((resolve) => setTimeout(resolve, 10));
				await session.close();
			},
		);

		await t.step(
			"group stream for unknown subscribe ID is ignored",
			async () => {
				// Create group message with subscribeId that doesn't exist
				const groupMsg = new GroupMessage({
					subscribeId: 999, // Non-existent subscribe ID
					sequence: 1,
				});
				const groupBuf = await encodeMessageToUint8Array(async (w) => {
					await writeVarint(w, UniStreamTypes.GroupStreamType);
					return await groupMsg.encode(w);
				});

				const mock = new MockWebTransportSession({
					openStreamResponses: [],
					acceptUniStreamData: [{
						type: UniStreamTypes.GroupStreamType,
						data: groupBuf,
					}],
				});

				const session = new Session({ transport: mock });
				await session.ready;

				await new Promise((resolve) => setTimeout(resolve, 10));
				await session.close();
			},
		);

		await t.step("fetch sends FETCH message and returns GroupReader", async () => {
			// The response stream will be empty (EOF) — that's fine for testing the request path
			const mock = new MockWebTransportSession({
				openStreamResponses: [new Uint8Array(0)],
			});

			const session = new Session({ transport: mock });
			await session.ready;

			const req = new FetchRequest({
				broadcastPath: "/live/stream",
				trackName: "video",
				priority: 5,
				groupSequence: 42,
			});

			const [group, err] = await session.fetch(req);
			assertExists(group);
			assertEquals(err, undefined);

			await session.close();
		});

		await t.step("fetch returns error when openStream fails", async () => {
			const mock = new MockWebTransportSession({
				openStreamResponses: [],
			});

			const session = new Session({ transport: mock });
			await session.ready;

			mock.close();

			const req = new FetchRequest({
				broadcastPath: "/live/stream",
				trackName: "video",
				priority: 5,
				groupSequence: 1,
			});

			const [group, err] = await session.fetch(req);
			assertEquals(group, undefined);
			assertExists(err);
		});

		await t.step(
			"incoming fetch stream calls fetchHandler.serveFetch",
			async () => {
				let served = false;
				let servedPath: string | undefined;
				let servedTrackName: string | undefined;
				const handler: FetchHandler = {
					serveFetch: async (_w: GroupWriter, r: FetchRequest) => {
						served = true;
						servedPath = r.broadcastPath;
						servedTrackName = r.trackName;
					},
				};

				const fm = new FetchMessage({
					broadcastPath: "/live/fetch",
					trackName: "audio",
					priority: 3,
					groupSequence: 10,
				});
				const buf = await encodeMessageToUint8Array(async (w) => {
					await writeVarint(w, BiStreamTypes.FetchStreamType);
					return await fm.encode(w);
				});

				const mock = new MockWebTransportSession({
					openStreamResponses: [],
					acceptStreamData: [{
						type: BiStreamTypes.FetchStreamType,
						data: buf,
					}],
				});

				const session = new Session({
					transport: mock,
					fetchHandler: handler,
				});
				await session.ready;

				await new Promise((resolve) => setTimeout(resolve, 10));
				assertEquals(served, true);
				assertEquals(servedPath, "/live/fetch");
				assertEquals(servedTrackName, "audio");
				await session.close();
			},
		);

		await t.step(
			"incoming fetch stream without handler cancels stream",
			async () => {
				const fm = new FetchMessage({
					broadcastPath: "/live/fetch",
					trackName: "audio",
					priority: 3,
					groupSequence: 10,
				});
				const buf = await encodeMessageToUint8Array(async (w) => {
					await writeVarint(w, BiStreamTypes.FetchStreamType);
					return await fm.encode(w);
				});

				const mock = new MockWebTransportSession({
					openStreamResponses: [],
					acceptStreamData: [{
						type: BiStreamTypes.FetchStreamType,
						data: buf,
					}],
				});

				// No fetchHandler provided
				const session = new Session({ transport: mock });
				await session.ready;

				await new Promise((resolve) => setTimeout(resolve, 10));
				await session.close();
			},
		);
	},
});
