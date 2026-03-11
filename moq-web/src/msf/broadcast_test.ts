import { assertEquals, assertThrows } from "@std/assert";
import { Broadcast, DefaultCatalogTrackName } from "./mod.ts";
import type { TrackHandler } from "../track_mux.ts";
import type { TrackWriter } from "../track_writer.ts";

class MockTrackHandler implements TrackHandler {
	calls: TrackWriter[] = [];

	async serveTrack(trackWriter: TrackWriter): Promise<void> {
		this.calls.push(trackWriter);
	}
}

function newCatalogTrackWriter(trackName: string) {
	const frames: Uint8Array[] = [];
	const closeWithErrorCalls: number[] = [];
	let closeCalls = 0;
	let groupCloseCalls = 0;
	const group = {
		async writeFrame(payload: Uint8Array): Promise<Error | undefined> {
			frames.push(payload);
			return undefined;
		},
		async close(): Promise<void> {
			groupCloseCalls++;
		},
		async cancel(): Promise<void> {
			groupCloseCalls++;
		},
	};
	const trackWriter = {
		trackName,
		async openGroup() {
			return [group, undefined] as const;
		},
		async closeWithError(code: number): Promise<void> {
			closeWithErrorCalls.push(code);
		},
		async close(): Promise<void> {
			closeCalls++;
		},
	} as unknown as TrackWriter & {
		frames: Uint8Array[];
		closeWithErrorCalls: number[];
		getCloseCalls: () => number;
		getGroupCloseCalls: () => number;
	};
	trackWriter.frames = frames;
	trackWriter.closeWithErrorCalls = closeWithErrorCalls;
	trackWriter.getCloseCalls = () => closeCalls;
	trackWriter.getGroupCloseCalls = () => groupCloseCalls;
	return trackWriter;
}

Deno.test("msf Broadcast registerTrack updates catalog and handler", async () => {
	const broadcast = new Broadcast({ version: 1, tracks: [] });
	const handler = new MockTrackHandler();

	await broadcast.registerTrack(
		{ name: "video", packaging: "loc", isLive: false },
		handler,
	);

	assertEquals(broadcast.catalog().tracks.length, 1);
	assertEquals(broadcast.catalog().tracks[0]?.name, "video");

	const trackWriter = { trackName: "video" } as TrackWriter;
	await broadcast.handler("video").serveTrack(trackWriter);
	assertEquals(handler.calls.length, 1);
	assertEquals(handler.calls[0], trackWriter);
});

Deno.test("msf Broadcast serves catalog on reserved track", async () => {
	const broadcast = new Broadcast({
		version: 1,
		tracks: [{ name: "video", packaging: "loc", isLive: false }],
	});
	const trackWriter = newCatalogTrackWriter(DefaultCatalogTrackName);

	await broadcast.serveTrack(trackWriter);

	assertEquals(trackWriter.frames.length, 1);
	const text = new TextDecoder().decode(trackWriter.frames[0]);
	assertEquals(text.includes('"video"'), true);
	assertEquals(trackWriter.getGroupCloseCalls(), 1);
	assertEquals(trackWriter.getCloseCalls(), 1);
	assertEquals(trackWriter.closeWithErrorCalls.length, 0);
});

Deno.test("msf Broadcast rejects duplicate track names across namespaces", () => {
	assertThrows(
		() =>
			new Broadcast({
				version: 1,
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
