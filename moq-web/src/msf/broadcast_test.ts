import { assertEquals, assertRejects, assertThrows } from "@std/assert";
import { background, withCancelCause } from "@okdaichi/golikejs/context";
import {
	Broadcast,
	DefaultCatalogTrackName,
	parseCatalog,
	type Track,
	ValidationError,
} from "./mod.ts";
import { SubscribeErrorCode } from "../error.ts";
import type { TrackHandler } from "../track_mux.ts";
import type { TrackWriter } from "../track_writer.ts";

class MockTrackHandler implements TrackHandler {
	calls: TrackWriter[] = [];

	async serveTrack(trackWriter: TrackWriter): Promise<void> {
		this.calls.push(trackWriter);
	}
}

interface CatalogWriterOptions {
	openErr?: Error;
	writeErr?: Error;
	/** readUpdate throws on leave, as a reset stream's read does. */
	leaveThrows?: boolean;
}

/**
 * A catalog subscription's track writer. Each group's frames are recorded and
 * waitForGroups(n) resolves once n groups are written; leave() ends the
 * subscribe stream as a departing subscriber does, and cancel() ends the
 * writer's context as closing it or the session would.
 */
function newCatalogTrackWriter(trackName: string, options: CatalogWriterOptions = {}) {
	const [context, cancel] = withCancelCause(background());
	const groups: Uint8Array[][] = [];
	const waiters: { count: number; resolve: () => void }[] = [];
	const closeWithErrorCalls: number[] = [];
	const cancelCalls: number[] = [];
	let closeCalls = 0;
	let leave: () => void = () => {};
	const left = new Promise<void>((resolve) => {
		leave = resolve;
	});
	// TrackWriter is a class with private state, so the fake is cast to it.
	const trackWriter = {
		trackName,
		context,
		async openGroup() {
			if (options.openErr) {
				return [undefined, options.openErr] as const;
			}
			const frames: Uint8Array[] = [];
			const group = {
				async writeFrame(payload: Uint8Array): Promise<Error | undefined> {
					if (options.writeErr) {
						return options.writeErr;
					}
					frames.push(payload);
					return undefined;
				},
				async close(): Promise<void> {
					groups.push(frames);
					for (const waiter of waiters.filter((w) => w.count <= groups.length)) {
						waiter.resolve();
					}
				},
				async cancel(code: number): Promise<void> {
					cancelCalls.push(code);
				},
			};
			return [group, undefined] as const;
		},
		async readUpdate() {
			await left;
			if (options.leaveThrows) {
				throw new Error("stream reset");
			}
			return [undefined, new Error("subscribe stream ended")] as const;
		},
		async closeWithError(code: number): Promise<void> {
			closeWithErrorCalls.push(code);
			cancel(new Error("closed with error"));
		},
		async close(): Promise<void> {
			closeCalls++;
			cancel(undefined);
		},
	} as unknown as TrackWriter;
	return {
		trackWriter,
		groups,
		closeWithErrorCalls,
		cancelCalls,
		closeCalls: () => closeCalls,
		waitForGroups(count: number): Promise<void> {
			if (groups.length >= count) {
				return Promise.resolve();
			}
			return new Promise((resolve) => waiters.push({ count, resolve }));
		},
		leave,
		cancel: () => cancel(undefined),
	};
}

/** The track names of each catalog group written so far. */
function groupTrackNames(groups: Uint8Array[][]): string[][] {
	return groups.map((frames) => {
		const text = frames.map((frame) => new TextDecoder().decode(frame)).join("");
		return parseCatalog(text).tracks.map((track) => track.name ?? "");
	});
}

const video = { name: "video", packaging: "loc", isLive: false } as const;
const audio = { name: "audio", packaging: "loc", isLive: false } as const;

Deno.test("msf Broadcast registerTrack updates catalog and handler", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	const handler = new MockTrackHandler();

	await broadcast.registerTrack(video, handler);

	assertEquals(broadcast.catalog().tracks.length, 1);
	assertEquals(broadcast.catalog().tracks[0]?.name, "video");

	const trackWriter = { trackName: "video" } as TrackWriter;
	await broadcast.handler("video").serveTrack(trackWriter);
	assertEquals(handler.calls.length, 1);
	assertEquals(handler.calls[0], trackWriter);
});

Deno.test("msf Broadcast registerTrack keeps tracks registered concurrently", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });

	await Promise.all([
		broadcast.registerTrack(video, new MockTrackHandler()),
		broadcast.registerTrack(audio, new MockTrackHandler()),
	]);

	assertEquals(broadcast.catalog().tracks.map((track) => track.name), ["video", "audio"]);
});

Deno.test("msf Broadcast serveCatalogTrack sends the catalog, then each change", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [video] });
	const writer = newCatalogTrackWriter(DefaultCatalogTrackName);
	const served = broadcast.serveTrack(writer.trackWriter);

	await writer.waitForGroups(1);
	await broadcast.registerTrack(audio, new MockTrackHandler());
	await writer.waitForGroups(2);
	await broadcast.removeTrack("video");
	await writer.waitForGroups(3);
	await broadcast.setCatalog({ version: "draft-02", tracks: [{ ...video, name: "text" }] });
	await writer.waitForGroups(4);

	assertEquals(groupTrackNames(writer.groups), [["video"], ["video", "audio"], ["audio"], [
		"text",
	]]);
	writer.leave();
	await served;
});

Deno.test("msf Broadcast serveCatalogTrack skips an unchanged catalog", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [video] });
	const writer = newCatalogTrackWriter(DefaultCatalogTrackName);
	const served = broadcast.serveTrack(writer.trackWriter);
	await writer.waitForGroups(1);

	// Neither changes the catalog, so the next group is the change after them.
	await broadcast.setCatalog({ version: "draft-02", tracks: [video] });
	await broadcast.removeTrack("missing");
	await broadcast.registerTrack(audio, new MockTrackHandler());
	await writer.waitForGroups(2);

	assertEquals(groupTrackNames(writer.groups), [["video"], ["video", "audio"]]);
	writer.leave();
	await served;
});

Deno.test("msf Broadcast serveCatalogTrack ends when the subscriber leaves", async () => {
	const tests: Record<string, CatalogWriterOptions> = {
		"stream closed": {},
		"stream reset": { leaveThrows: true },
	};
	for (const [name, options] of Object.entries(tests)) {
		const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
		const writer = newCatalogTrackWriter(DefaultCatalogTrackName, options);
		const served = broadcast.serveTrack(writer.trackWriter);
		await writer.waitForGroups(1);

		writer.leave();
		await served;

		assertEquals(writer.groups.length, 1, name);
		assertEquals(writer.closeCalls(), 1, name);
		assertEquals(writer.closeWithErrorCalls, [], name);
	}
});

Deno.test("msf Broadcast serveCatalogTrack ends when its context ends", async () => {
	// The mux closes the writer when the announcement ends, which ends its
	// context; so does the session ending.
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	const writer = newCatalogTrackWriter(DefaultCatalogTrackName);
	const served = broadcast.serveTrack(writer.trackWriter);
	await writer.waitForGroups(1);

	writer.cancel();
	await served;

	assertEquals(writer.groups.length, 1);
	assertEquals(writer.closeWithErrorCalls, []);
});

Deno.test("msf Broadcast close ends catalog subscriptions after the pending change", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	const writer = newCatalogTrackWriter(DefaultCatalogTrackName);
	const served = broadcast.serveTrack(writer.trackWriter);
	await writer.waitForGroups(1);
	await broadcast.registerTrack(video, new MockTrackHandler());

	await broadcast.close();
	await served;

	assertEquals(groupTrackNames(writer.groups), [[], ["video"]]);
	assertEquals(writer.closeCalls(), 1);
});

Deno.test("msf Broadcast close is idempotent", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	await broadcast.close();

	await broadcast.close();
});

Deno.test("msf Broadcast close gives a later catalog subscription one group", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [video] });
	await broadcast.close();
	const writer = newCatalogTrackWriter(DefaultCatalogTrackName);

	await broadcast.serveTrack(writer.trackWriter);

	assertEquals(groupTrackNames(writer.groups), [["video"]]);
	assertEquals(writer.closeCalls(), 1);
});

Deno.test("msf Broadcast catalogBytes returns a copy", () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [video] });
	broadcast.catalogBytes().fill(0);

	const bytes = broadcast.catalogBytes();

	assertEquals(parseCatalog(new TextDecoder().decode(bytes)).tracks[0]?.name, "video");
});

Deno.test("msf Broadcast rejects duplicate track names across namespaces", () => {
	assertThrows(
		() =>
			new Broadcast({
				version: "draft-02",
				defaultNamespace: "live/main",
				tracks: [
					{ name: "video", packaging: "loc", isLive: false },
					{
						namespace: "live/backup",
						name: "video",
						packaging: "loc",
						isLive: false,
					},
				],
			}),
		Error,
		"unique track names across namespaces",
	);
});

Deno.test("msf Broadcast catalogTrackName returns custom name", () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] }, "meta");
	assertEquals(broadcast.catalogTrackName, "meta");
});

Deno.test("msf Broadcast registerTrack rejects nil handler", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	await assertRejects(
		() =>
			broadcast.registerTrack(
				{ name: "video", packaging: "cmaf", isLive: true },
				null as unknown as TrackHandler,
			),
		Error,
		"cannot be nil",
	);
});

Deno.test("msf Broadcast registerTrack rejects missing track name", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	const handler = new MockTrackHandler();
	await assertRejects(
		() => broadcast.registerTrack({ name: "", packaging: "cmaf", isLive: true }, handler),
		Error,
		"track name is required",
	);
});

Deno.test("msf Broadcast registerTrack rejects catalog track name", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	const handler = new MockTrackHandler();
	await assertRejects(
		() =>
			broadcast.registerTrack({
				name: DefaultCatalogTrackName,
				packaging: "cmaf",
				isLive: true,
			}, handler),
		Error,
		"reserved for the catalog track",
	);
});

Deno.test("msf Broadcast registerTrack replaces existing track", async () => {
	const broadcast = new Broadcast({
		version: "draft-02",
		tracks: [{ name: "audio", packaging: "cmaf", isLive: true }],
	});
	const handler1 = new MockTrackHandler();
	await broadcast.registerTrack(
		{ name: "audio", packaging: "cmaf", isLive: true, bitrate: 128 },
		handler1,
	);
	assertEquals(broadcast.catalog().tracks.length, 1);
	assertEquals(broadcast.catalog().tracks[0]?.bitrate, 128);
});

Deno.test("msf Broadcast setCatalog removes stale tracks", async () => {
	const broadcast = new Broadcast({
		version: "draft-02",
		tracks: [{ name: "video", packaging: "cmaf", isLive: true }],
	});
	const handler = new MockTrackHandler();
	await broadcast.registerTrack({ name: "video", packaging: "cmaf", isLive: true }, handler);

	await broadcast.setCatalog({
		version: "draft-02",
		tracks: [{ name: "audio", packaging: "cmaf", isLive: true }],
	});
	assertEquals(broadcast.catalog().tracks.length, 1);
	assertEquals(broadcast.catalog().tracks[0]?.name, "audio");
});

Deno.test("msf Broadcast setCatalog with empty previous tracks", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	await broadcast.setCatalog({
		version: "draft-02",
		tracks: [{ name: "video", packaging: "cmaf", isLive: true }],
	});
	assertEquals(broadcast.catalog().tracks.length, 1);
});

Deno.test("msf Broadcast removeTrack removes registered track", async () => {
	const broadcast = new Broadcast({
		version: "draft-02",
		tracks: [{ name: "video", packaging: "cmaf", isLive: true }],
	});
	const handler = new MockTrackHandler();
	await broadcast.registerTrack({ name: "video", packaging: "cmaf", isLive: true }, handler);

	const removed = await broadcast.removeTrack("video");
	assertEquals(removed, true);
	assertEquals(broadcast.catalog().tracks.length, 0);
});

Deno.test("msf Broadcast removeTrack returns false for empty name", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	const removed = await broadcast.removeTrack("");
	assertEquals(removed, false);
});

Deno.test("msf Broadcast removeTrack returns false for catalog track name", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	const removed = await broadcast.removeTrack(DefaultCatalogTrackName);
	assertEquals(removed, false);
});

Deno.test("msf Broadcast removeTrack returns true when track not in catalog but was registered", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	const handler = new MockTrackHandler();
	await broadcast.registerTrack({ name: "audio", packaging: "cmaf", isLive: true }, handler);

	// Remove from catalog first via setCatalog, then removeTrack
	const removed = await broadcast.removeTrack("nonexistent");
	assertEquals(removed, false);
});

Deno.test("msf Broadcast serveCatalogTrack closes with an error when a group can't be written", async () => {
	const tests: Record<string, CatalogWriterOptions> = {
		"open fails": { openErr: new Error("open failed") },
		"write fails": { writeErr: new Error("write failed") },
	};
	for (const [name, options] of Object.entries(tests)) {
		const broadcast = new Broadcast({
			version: "draft-02",
			tracks: [{ name: "video", packaging: "cmaf", isLive: true }],
		});
		const writer = newCatalogTrackWriter(DefaultCatalogTrackName, options);

		await broadcast.serveTrack(writer.trackWriter);

		assertEquals(writer.closeWithErrorCalls, [SubscribeErrorCode.InternalError], name);
		assertEquals(writer.cancelCalls.length, options.writeErr ? 1 : 0, name);
	}
});

Deno.test("msf Broadcast serveCatalogTrack returns quietly when the subscriber left mid-write", async () => {
	const broadcast = new Broadcast({ version: "draft-02", tracks: [] });
	const writer = newCatalogTrackWriter(DefaultCatalogTrackName, {
		writeErr: new Error("stream reset"),
	});
	writer.cancel();

	await broadcast.serveTrack(writer.trackWriter);

	assertEquals(writer.closeWithErrorCalls, []);
});

Deno.test("msf Broadcast removeTrack refuses a track another track depends on", async () => {
	const timeline: Track = {
		name: "timeline",
		packaging: "mediatimeline",
		isLive: true,
		depends: ["video"],
	};
	const broadcast = new Broadcast({ version: "draft-02", tracks: [video, timeline] });

	await assertRejects(() => broadcast.removeTrack("video"), ValidationError);

	assertEquals(broadcast.catalog().tracks.map((t) => t.name), ["video", "timeline"]);
});
