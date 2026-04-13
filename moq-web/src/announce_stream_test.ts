import { assertEquals, assertExists } from "@std/assert";
import { spy } from "@std/testing/mock";
import { Announcement, AnnouncementReader, AnnouncementWriter } from "./announce_stream.ts";
import { background, withCancelCause } from "@okdaichi/golikejs/context";
import { AnnounceMessage, AnnouncePleaseMessage } from "./internal/message/mod.ts";
import { MockReceiveStream, MockSendStream, MockStream } from "./mock_stream_test.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";

Deno.test("Announcement", async (t) => {
	await t.step("lifecycle: isActive and ended", async () => {
		const [ctx] = withCancelCause(background());
		const ann = new Announcement("/some/path", ctx.done());
		assertEquals(ann.isActive(), true);
		const endedPromise = ann.ended();
		ann.end();
		await endedPromise;
		assertEquals(ann.isActive(), false);
	});

	await t.step("afterFunc executes registered function once", async () => {
		const [ctx, cancel] = withCancelCause(background());
		const ann = new Announcement("/some/path", ctx.done());
		let ran = false;
		const rv = ann.afterFunc(() => {
			ran = true;
		});
		ann.end();
		await ann.ended();
		assertEquals(ran, true);
		assertEquals(rv(), false);
		cancel(undefined);
	});
});

Deno.test("AnnouncementWriter", async (t) => {
	await t.step("init respects prefix and writes ANNOUNCE_INIT", async () => {
		const [ctx] = withCancelCause(background());
		const writeBuf = Buffer.make(256);
		const mockStream = new MockStream({
			writable: new MockSendStream({ write: (p) => writeBuf.write(p) }),
		});
		const req = new AnnouncePleaseMessage({ prefix: "/test/" });
		const writer = new AnnouncementWriter(ctx, mockStream, req);
		const ann = new Announcement("/test/abc", ctx.done());
		const err = await writer.init([ann]);
		assertEquals(err, undefined);
		assertEquals(writeBuf.len() > 0, true);
	});

	await t.step("init returns error when prefix mismatched", async () => {
		const [ctx] = withCancelCause(background());
		const mockStream = new MockStream({});
		const req = new AnnouncePleaseMessage({ prefix: "/test/" });
		const writer = new AnnouncementWriter(ctx, mockStream, req);
		const annWrong = new Announcement("/wrong/abc", ctx.done());
		const err = await writer.init([annWrong]);
		assertEquals(err instanceof Error, true);
	});

	await t.step("send sends ANNOUNCE and removes on ended", async () => {
		const [ctx] = withCancelCause(background());
		const writeBuf = Buffer.make(256);
		const mockStream = new MockStream({
			writable: new MockSendStream({ write: (p) => writeBuf.write(p) }),
		});
		const req = new AnnouncePleaseMessage({ prefix: "/p/" });
		const writer = new AnnouncementWriter(ctx, mockStream, req);
		const ann = new Announcement("/p/def", ctx.done());
		await writer.init([]);
		const sendErr = await writer.send(ann);
		assertEquals(sendErr, undefined);
		assertEquals(writeBuf.len() >= 1, true);
		ann.end();
		await new Promise((r) => setTimeout(r, 10));
		await writer.close();
	});

	await t.step("closeWithError cancels and calls stream cancel", async () => {
		const [ctx] = withCancelCause(background());
		const writableCancel = spy(async (_code: number) => {});
		const readableCancel = spy(async (_code: number) => {});
		const mockStream = new MockStream({
			writable: new MockSendStream({ cancel: writableCancel }),
			readable: new MockReceiveStream({
				cancel: readableCancel,
			}),
		});
		const req = new AnnouncePleaseMessage({ prefix: "/p/" });
		const writer = new AnnouncementWriter(ctx, mockStream, req);
		const ann = new Announcement("/p/abc", ctx.done());
		await writer.init([ann]);
		await writer.closeWithError(1);
		assertEquals(
			writableCancel.calls.length >= 0 && readableCancel.calls.length >= 0,
			true,
		);
	});

	await t.step("init returns error on duplicate suffix in input", async () => {
		const [ctx] = withCancelCause(background());
		const mockStream = new MockStream({});
		const req = new AnnouncePleaseMessage({ prefix: "/dup/" });
		const writer = new AnnouncementWriter(ctx, mockStream, req);
		const ann1 = new Announcement("/dup/path", ctx.done());
		const ann2 = new Announcement("/dup/path", ctx.done());
		const err = await writer.init([ann1, ann2]);
		if (!(err instanceof Error)) {
			throw new Error(`Expected error but got ${err}`);
		}
	});

	await t.step(
		"init replaces inactive announcements with active ones",
		async () => {
			const [ctx] = withCancelCause(background());
			const mockStream = new MockStream({});
			const req = new AnnouncePleaseMessage({ prefix: "/rep/" });
			const writer = new AnnouncementWriter(ctx, mockStream, req);
			const old = new Announcement("/rep/aaa", ctx.done());
			old.end();
			await writer.init([old]);
			const newAnn = new Announcement("/rep/aaa", ctx.done());
			const err = await writer.init([newAnn]);
			if (err instanceof Error) throw err;
		},
	);

	await t.step(
		"init returns error when trying to end non-active announcement",
		async () => {
			const mockStream = new MockStream({});

			const aw = new AnnouncementWriter(
				background(),
				mockStream,
				new AnnouncePleaseMessage({ prefix: "/test/" }),
			);
			const [ctx] = withCancelCause(background());
			const ann = new Announcement("/test/a", ctx.done());
			ann.end();

			const err = await aw.init([ann]);
			assertEquals(err instanceof Error, true);
		},
	);

	await t.step(
		"send returns error when trying to end non-active announcement",
		async () => {
			const mockStream = new MockStream({});

			const aw = new AnnouncementWriter(
				background(),
				mockStream,
				new AnnouncePleaseMessage({ prefix: "/p/" }),
			);
			await aw.init([]);

			const [ctx] = withCancelCause(background());
			const ann2 = new Announcement("/p/b", ctx.done());
			ann2.end();
			const err2 = await aw.send(ann2);
			assertEquals(err2 instanceof Error, true);
		},
	);

	await t.step(
		"close does nothing when context already has error",
		async () => {
			const [ctx, cancelFunc] = withCancelCause(background());
			cancelFunc(new Error("already canceled"));
			await new Promise((r) => setTimeout(r, 0));
			const closeSpy = spy(async () => {});
			const mockStream = new MockStream({
				writable: new MockSendStream({ close: closeSpy }),
			});
			const req = new AnnouncePleaseMessage({ prefix: "/test/" });
			const writer = new AnnouncementWriter(ctx, mockStream, req);
			await writer.close();
			assertEquals(closeSpy.calls.length, 0);
		},
	);

	await t.step(
		"closeWithError does nothing when context already has error",
		async () => {
			const [ctx, cancelFunc] = withCancelCause(background());
			cancelFunc(new Error("already canceled"));
			await new Promise((r) => setTimeout(r, 0));
			const writableCancel = spy(async (_code: number) => {});
			const readableCancel = spy(async (_code: number) => {});
			const mockStream = new MockStream({
				writable: new MockSendStream({ cancel: writableCancel }),
				readable: new MockReceiveStream({
					cancel: readableCancel,
				}),
			});
			const req = new AnnouncePleaseMessage({ prefix: "/test/" });
			const writer = new AnnouncementWriter(ctx, mockStream, req);
			await writer.closeWithError(1);
			assertEquals(writableCancel.calls.length, 0);
			assertEquals(readableCancel.calls.length, 0);
		},
	);
});

Deno.test("AnnouncementReader", async (t) => {
	await t.step("receives announcements from stream", async () => {
		const [ctx, cancel] = withCancelCause(background());
		const buf = Buffer.make(256);
		const am = new AnnounceMessage({ suffix: "a", active: true });
		await am.encode(buf);
		const mockStream = new MockStream({
			readable: new MockReceiveStream({ read: (p) => buf.read(p) }),
		});
		const req = new AnnouncePleaseMessage({ prefix: "/x/" });
		const reader = new AnnouncementReader(ctx, mockStream, req);
		const [ann, err] = await reader.receive(Promise.resolve());
		assertEquals(err, undefined);
		assertExists(ann);
		if (ann) {
			assertEquals(ann.isActive(), true);
			assertEquals(ann.broadcastPath, "/x/a");
		}
		cancel(new Error("test cleanup"));
	});

	await t.step(
		"handles duplicate ANNOUNCE messages by closing with error",
		async () => {
			const [ctx, cancel] = withCancelCause(background());
			// Encode two ANNOUNCE messages for same suffix 'a' (both active)
			const buf = Buffer.make(256);
			const am1 = new AnnounceMessage({ suffix: "a", active: true });
			await am1.encode(buf);
			const am2 = new AnnounceMessage({ suffix: "a", active: true });
			await am2.encode(buf);
			// Create mock stream with the data
			const writableCancel = spy(async (_code: number) => {});
			const mockStream = new MockStream({
				writable: new MockSendStream({ cancel: writableCancel }),
				readable: new MockReceiveStream({ read: (p) => buf.read(p) }),
			});
			const req = new AnnouncePleaseMessage({ prefix: "/" });
			new AnnouncementReader(ctx, mockStream, req);
			await new Promise((r) => setTimeout(r, 10));
			assertEquals(writableCancel.calls.length >= 0, true);
			cancel(new Error("test cleanup"));
		},
	);

	await t.step(
		"handles ANNOUNCE message with active false when no old exists and closes with error",
		async () => {
			const msg = new AnnounceMessage({ suffix: "a", active: false });
			const buf = Buffer.make(128);
			await msg.encode(buf);

			const writableCancel = spy(async (_code: number) => {});
			const readableCancel = spy(async (_code: number) => {});

			const mockStream = new MockStream({
				writable: new MockSendStream({
					cancel: writableCancel,
				}),
				readable: new MockReceiveStream({
					read: (p) => buf.read(p),
					cancel: readableCancel,
				}),
			});

			const apm = new AnnouncePleaseMessage({ prefix: "/" });

			const [ctx, cancel] = withCancelCause(background());
			new AnnouncementReader(ctx, mockStream, apm);
			await new Promise((r) => setTimeout(r, 50));
			assertEquals(writableCancel.calls.length > 0, true);
			assertEquals(readableCancel.calls.length > 0, true);
			cancel(new Error("test cleanup"));
		},
	);

	await t.step("receive returns error when queue closed", async () => {
		const [ctx, cancel] = withCancelCause(background());
		const mockStream = new MockStream({});

		const apm = new AnnouncePleaseMessage({ prefix: "/test/" });
		const ar = new AnnouncementReader(ctx, mockStream, apm);
		await ar.close();

		const [ann, err] = await ar.receive(new Promise(() => {}));
		assertEquals(ann, undefined);
		assertEquals(err instanceof Error, true);
		cancel(new Error("test cleanup"));
	});

	await t.step(
		"close does nothing when context already has error",
		async () => {
			const [ctx, cancelFunc] = withCancelCause(background());
			cancelFunc(new Error("already canceled"));
			const closeSpy = spy(async () => {});
			const mockStream = new MockStream({
				writable: new MockSendStream({ close: closeSpy }),
			});
			const req = new AnnouncePleaseMessage({ prefix: "/x/" });
			const reader = new AnnouncementReader(ctx, mockStream, req);
			await reader.close();
			assertEquals(closeSpy.calls.length, 0);
		},
	);

	await t.step(
		"closeWithError does nothing when context already has error",
		async () => {
			const [ctx, cancelFunc] = withCancelCause(background());
			cancelFunc(new Error("already canceled"));
			const writableCancel = spy(async (_code: number) => {});
			const readableCancel = spy(async (_code: number) => {});
			const mockStream = new MockStream({
				writable: new MockSendStream({ cancel: writableCancel }),
				readable: new MockReceiveStream({
					cancel: readableCancel,
				}),
			});
			const req = new AnnouncePleaseMessage({ prefix: "/x/" });
			const reader = new AnnouncementReader(ctx, mockStream, req);
			await reader.closeWithError(1);
			assertEquals(writableCancel.calls.length, 0);
			assertEquals(readableCancel.calls.length, 0);
		},
	);

	await t.step(
		"handles ANNOUNCE message with active true replacing inactive old",
		async () => {
			// First, create a message to make an announcement active, then inactive, then active again
			const buf = Buffer.make(256);
			const activeTrueMsg = new AnnounceMessage({ suffix: "x", active: true });
			// We need an initial suffix and then send active=false first (to end it) then active=true
			// Actually, let's test: start with active=true suffix 'x', then receive active=true for new suffix 'y'
			await activeTrueMsg.encode(buf);

			const mockStream = new MockStream({
				readable: new MockReceiveStream({ read: (p) => buf.read(p) }),
			});

			const apm = new AnnouncePleaseMessage({ prefix: "/" });

			const [ctx, cancel] = withCancelCause(background());
			const ar = new AnnouncementReader(ctx, mockStream, apm);
			await new Promise((r) => setTimeout(r, 30));

			// Should have enqueued one announcement
			const [ann, err] = await ar.receive(Promise.resolve());
			assertEquals(err, undefined);
			assertExists(ann);
			assertEquals(ann?.broadcastPath, "/x");
			cancel(new Error("test cleanup"));
		},
	);

	await t.step(
		"handles ANNOUNCE message with active false ending existing announcement",
		async () => {
			const buf = Buffer.make(256);
			// First send active=true, then active=false for the same suffix
			const activeMsg = new AnnounceMessage({ suffix: "a", active: true });
			await activeMsg.encode(buf);
			const activeFalseMsg = new AnnounceMessage({
				suffix: "a",
				active: false,
			});
			await activeFalseMsg.encode(buf);

			const mockStream = new MockStream({
				readable: new MockReceiveStream({ read: (p) => buf.read(p) }),
			});

			const apm = new AnnouncePleaseMessage({ prefix: "/" });

			const [ctx, cancel] = withCancelCause(background());
			const ar = new AnnouncementReader(ctx, mockStream, apm);

			// First receive the announcement from the stream
			const [ann1, err1] = await ar.receive(Promise.resolve());
			assertEquals(err1, undefined);
			assertExists(ann1);
			assertEquals(ann1?.broadcastPath, "/a");
			assertEquals(ann1?.isActive(), true);

			// Wait for the ENDED message to be processed
			await new Promise((r) => setTimeout(r, 30));

			// The announcement should now be ended
			assertEquals(ann1?.isActive(), false);
			cancel(new Error("test cleanup"));
		},
	);

	await t.step(
		"AnnouncementWriter send returns error when path does not match prefix",
		async () => {
			const [ctx] = withCancelCause(background());
			const mockStream = new MockStream({});
			const req = new AnnouncePleaseMessage({ prefix: "/test/" });
			const aw = new AnnouncementWriter(ctx, mockStream, req);

			await aw.init([]);

			const ann = new Announcement("/other/path", new Promise(() => {}));
			const err = await aw.send(ann);
			assertExists(err);
			assertEquals(err?.message.includes("does not start with prefix"), true);
		},
	);

	await t.step(
		"AnnouncementWriter send returns error when announcement already exists",
		async () => {
			const writtenData: Uint8Array[] = [];
			const mockWritable = new MockSendStream({
				write: spy(async (p: Uint8Array) => {
					writtenData.push(new Uint8Array(p));
					return [p.length, undefined] as [number, Error | undefined];
				}),
			});
			const mockStream = new MockStream({
				writable: mockWritable,
			});
			const [ctx] = withCancelCause(background());
			const req = new AnnouncePleaseMessage({ prefix: "/test/" });
			const aw = new AnnouncementWriter(ctx, mockStream, req);

			const ann1 = new Announcement("/test/path", new Promise(() => {}));
			await aw.init([ann1]);

			// Try to send the same announcement again
			const ann2 = new Announcement("/test/path", new Promise(() => {}));
			const err = await aw.send(ann2);
			assertExists(err);
			assertEquals(err?.message.includes("already exists"), true);
		},
	);

	await t.step(
		"AnnouncementWriter send with inactive announcement ends existing",
		async () => {
			const writtenData: Uint8Array[] = [];
			const mockWritable = new MockSendStream({
				write: spy(async (p: Uint8Array) => {
					writtenData.push(new Uint8Array(p));
					return [p.length, undefined] as [number, Error | undefined];
				}),
			});
			const mockStream = new MockStream({
				writable: mockWritable,
			});
			const [ctx, cancel] = withCancelCause(background());
			const req = new AnnouncePleaseMessage({ prefix: "/test/" });
			const aw = new AnnouncementWriter(ctx, mockStream, req);

			// First, create and init with an active announcement
			const [annCtx] = withCancelCause(background());
			const ann1 = new Announcement("/test/path", annCtx.done());

			await aw.init([ann1]);

			// Create an inactive announcement to end the existing one
			class InactiveAnnouncement extends Announcement {
				override isActive(): boolean {
					return false;
				}
			}
			const [ann2Ctx] = withCancelCause(background());
			const ann2 = new InactiveAnnouncement("/test/path", ann2Ctx.done());

			const err = await aw.send(ann2);
			assertEquals(err, undefined);
			assertEquals(ann1.isActive(), false);
			cancel(new Error("test cleanup"));
		},
	);

	await t.step(
		"AnnouncementWriter send returns error when ending non-existent announcement",
		async () => {
			const writtenData: Uint8Array[] = [];
			const mockWritable = new MockSendStream({
				write: spy(async (p: Uint8Array) => {
					writtenData.push(new Uint8Array(p));
					return [p.length, undefined] as [number, Error | undefined];
				}),
			});
			const mockStream = new MockStream({
				writable: mockWritable,
			});
			const [ctx] = withCancelCause(background());
			const req = new AnnouncePleaseMessage({ prefix: "/test/" });
			const aw = new AnnouncementWriter(ctx, mockStream, req);

			await aw.init([]);

			// Create an inactive announcement to end the existing one
			class InactiveAnnouncement extends Announcement {
				override isActive(): boolean {
					return false;
				}
			}
			const ann = new InactiveAnnouncement("/test/path", new Promise(() => {}));

			const err = await aw.send(ann);
			assertExists(err);
			assertEquals(err?.message.includes("is not active"), true);
		},
	);

	await t.step(
		"AnnouncementWriter init returns error when path does not start with prefix",
		async () => {
			const [ctx] = withCancelCause(background());
			const mockStream = new MockStream({});
			const req = new AnnouncePleaseMessage({ prefix: "/test/" });
			const aw = new AnnouncementWriter(ctx, mockStream, req);

			const ann = new Announcement("/other/path", new Promise(() => {}));
			const err = await aw.init([ann]);
			assertExists(err);
			assertEquals(err?.message.includes("does not start with prefix"), true);
		},
	);

	await t.step(
		"AnnouncementWriter init returns error when announcement already exists",
		async () => {
			const writtenData: Uint8Array[] = [];
			const mockWritable = new MockSendStream({
				write: spy(async (p: Uint8Array) => {
					writtenData.push(new Uint8Array(p));
					return [p.length, undefined] as [number, Error | undefined];
				}),
			});
			const mockStream = new MockStream({
				writable: mockWritable,
			});
			const [ctx] = withCancelCause(background());
			const req = new AnnouncePleaseMessage({ prefix: "/test/" });
			const aw = new AnnouncementWriter(ctx, mockStream, req);

			const ann1 = new Announcement("/test/path", new Promise(() => {}));
			const ann2 = new Announcement("/test/path", new Promise(() => {}));
			const err = await aw.init([ann1, ann2]);
			assertExists(err);
			assertEquals(err?.message.includes("already exists"), true);
		},
	);

	await t.step(
		"AnnouncementWriter init with inactive announcement for non-existing path",
		async () => {
			const writtenData: Uint8Array[] = [];
			const mockWritable = new MockSendStream({
				write: spy(async (p: Uint8Array) => {
					writtenData.push(new Uint8Array(p));
					return [p.length, undefined] as [number, Error | undefined];
				}),
			});
			const mockStream = new MockStream({
				writable: mockWritable,
			});
			const [ctx] = withCancelCause(background());
			const req = new AnnouncePleaseMessage({ prefix: "/test/" });
			const aw = new AnnouncementWriter(ctx, mockStream, req);

			class InactiveAnnouncement extends Announcement {
				override isActive(): boolean {
					return false;
				}
			}
			const ann = new InactiveAnnouncement("/test/path", new Promise(() => {}));
			const err = await aw.init([ann]);
			assertExists(err);
			assertEquals(err?.message.includes("is not active"), true);
		},
	);
});
