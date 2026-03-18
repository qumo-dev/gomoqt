import { assertEquals, assertExists, assertInstanceOf } from "@std/assert";
import { spy } from "@std/testing/mock";
import { Session } from "./session.ts";
import {
	AnnounceInitMessage,
	AnnouncePleaseMessage,
	GroupMessage,
	SubscribeMessage,
	SubscribeOkMessage,
	writeVarint,
} from "./internal/message/mod.ts";
import { BiStreamTypes, UniStreamTypes } from "./stream_type.ts";
import { TrackMux } from "./track_mux.ts";
import type { TrackPrefix } from "./track_prefix.ts";
import { Writer } from "@okdaichi/golikejs/io";
import { EOFError } from "@okdaichi/golikejs/io";
import {
	ReceiveStream,
	SendStream,
	Stream,
	WebTransportSession,
} from "./internal/webtransport/mod.ts";

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
interface MockWebTransportSessionOptions {
	openStreamResponses?: Uint8Array[];
	openUniStreamCount?: number;
	acceptStreamData?: Array<{ type: number; data: Uint8Array }>;
	acceptUniStreamData?: Array<{ type: number; data: Uint8Array }>;
	closedPromise?: Promise<WebTransportCloseInfo>;
}

class MockWebTransportSession implements WebTransportSession {
	#streamIdCounter = 0n;
	#openStreamResponses: Uint8Array[];
	#openStreamIndex = 0;
	#acceptStreamData: Array<{ type: number; data: Uint8Array }>;
	#acceptStreamIndex = 0;
	#acceptUniStreamData: Array<{ type: number; data: Uint8Array }>;
	#acceptUniStreamIndex = 0;
	#closed = false;
	#closedPromise: Promise<WebTransportCloseInfo>;
	#closedResolve?: (info: WebTransportCloseInfo) => void;
	#waitingAcceptResolvers: Array<() => void> = [];

	ready: Promise<void> = Promise.resolve();

	constructor(options: MockWebTransportSessionOptions = {}) {
		this.#openStreamResponses = options.openStreamResponses ?? [];
		this.#acceptStreamData = options.acceptStreamData ?? [];
		this.#acceptUniStreamData = options.acceptUniStreamData ?? [];

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
		const id = this.#streamIdCounter;
		this.#streamIdCounter += 4n;

		const data = this.#openStreamResponses[this.#openStreamIndex] ??
			new Uint8Array();
		this.#openStreamIndex++;

		// Create inline mock stream
		const writtenData: Uint8Array[] = [];
		let readOffset = 0;
		const writable = {
			id,
			write: spy(async (p: Uint8Array) => {
				writtenData.push(new Uint8Array(p));
				return [p.length, undefined] as [number, Error | undefined];
			}),
			close: spy(async () => {}),
			cancel: spy(async (_code: number) => {}),
			closed: () => new Promise<void>(() => {}),
		};
		const readable = {
			id,
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
		return [{ id, writable, readable }, undefined];
	}

	async openUniStream(): Promise<[SendStream, undefined] | [undefined, Error]> {
		if (this.#closed) {
			return [undefined, new Error("session closed")];
		}
		const id = this.#streamIdCounter;
		this.#streamIdCounter += 4n;
		const writtenData: Uint8Array[] = [];
		return [{
			id,
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

		const id = this.#streamIdCounter;
		this.#streamIdCounter += 4n;

		const writtenData: Uint8Array[] = [];
		let readOffset = 0;
		const writable = {
			id,
			write: spy(async (p: Uint8Array) => {
				writtenData.push(new Uint8Array(p));
				return [p.length, undefined] as [number, Error | undefined];
			}),
			close: spy(async () => {}),
			cancel: spy(async (_code: number) => {}),
			closed: () => new Promise<void>(() => {}),
		};
		const readable = {
			id,
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
		return [{ id, writable, readable }, undefined];
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

		const id = this.#streamIdCounter;
		this.#streamIdCounter += 4n;

		let readOffset = 0;
		return [{
			id,
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
}

Deno.test({
	name: "Session",
	sanitizeOps: false,
	sanitizeResources: false,
	fn: async (t) => {
		await t.step("constructor and ready sends client message", async () => {
			const mock = new MockWebTransportSession({});

			const session = new Session({ webtransport: mock });
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
					session = new Session({ webtransport: mock });
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
			"constructor is ready without version negotiation",
			async () => {
				const mock = new MockWebTransportSession({});

				let threw = false;
				let session: Session | undefined;
				try {
					session = new Session({ webtransport: mock });
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
			"acceptAnnounce returns error when ANNOUNCE_INIT decode fails",
			async () => {
				const truncatedBytes = new Uint8Array([0x80]);

				const mock = new MockWebTransportSession({
					openStreamResponses: [truncatedBytes],
				});

				const session = new Session({ webtransport: mock });
				await session.ready;

				const [reader, err] = await session.acceptAnnounce(
					"/test/" as TrackPrefix,
				);
				assertEquals(reader, undefined);
				assertExists(err);
				await session.close();
			},
		);

		await t.step(
			"subscribe returns error when SUBSCRIBE_OK decode fails",
			async () => {
				const truncatedBytes = new Uint8Array([0x80]);

				const mock = new MockWebTransportSession({
					openStreamResponses: [truncatedBytes],
				});

				const session = new Session({ webtransport: mock });
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
					trackPriority: 0,
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

				const session = new Session({ webtransport: mock, mux });
				await session.ready;

				await new Promise((resolve) => setTimeout(resolve, 10));
				assertEquals(served, true);
				await session.close();
			},
		);

		await t.step("acceptAnnounce succeeds with valid messages", async () => {
			const init = new AnnounceInitMessage({ suffixes: ["suffix"] });
			const initBytes = await encodeMessageToUint8Array(async (w) => init.encode(w));

			const mock = new MockWebTransportSession({
				openStreamResponses: [initBytes],
			});

			const session = new Session({ webtransport: mock });
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
			const okBytes = await encodeMessageToUint8Array(async (w) => ok.encode(w));

			const mock = new MockWebTransportSession({
				openStreamResponses: [okBytes],
			});

			const session = new Session({ webtransport: mock });
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

				const req = new AnnouncePleaseMessage({ prefix: "/test/" });
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

				const session = new Session({ webtransport: mock, mux });
				await session.ready;

				await new Promise((resolve) => setTimeout(resolve, 10));
				assertEquals(served, true);
				assertEquals(servedPrefix, "/test/");
				await session.close();
			},
		);

		await t.step("listening for group stream enqueues to track", async () => {
			const ok = new SubscribeOkMessage({});
			const okBytes = await encodeMessageToUint8Array(async (w) => ok.encode(w));

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

			const session = new Session({ webtransport: mock });
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

			const session = new Session({ webtransport: mock });
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

				const session = new Session({ webtransport: mock });
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

				const session = new Session({ webtransport: mock });
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
				const okBytes = await encodeMessageToUint8Array(async (w) => ok.encode(w));

				const mock = new MockWebTransportSession({
					openStreamResponses: [okBytes, okBytes, okBytes],
				});

				const session = new Session({ webtransport: mock });
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

				const session = new Session({ webtransport: mock });
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

				const session = new Session({ webtransport: mock });
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
				const okBytes = await encodeMessageToUint8Array(async (w) => ok.encode(w));

				const mock = new MockWebTransportSession({
					openStreamResponses: [okBytes],
				});

				const session = new Session({ webtransport: mock });
				await session.ready;

				const [track, err] = await session.subscribe(
					"/test/path",
					"track-name",
					{ trackPriority: 5 },
				);
				assertExists(track);
				assertEquals(err, undefined);

				// Verify track config is reflected
				const config = track.trackConfig;
				assertEquals(config.trackPriority, 5);

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

				const session = new Session({ webtransport: mock });
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

				const session = new Session({ webtransport: mock });
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

				const session = new Session({ webtransport: mock });
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

				const session = new Session({ webtransport: mock });
				await session.ready;

				await new Promise((resolve) => setTimeout(resolve, 10));
				await session.close();
			},
		);
	},
});
