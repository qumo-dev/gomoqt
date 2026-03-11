import { assertExists } from "@std/assert";
import * as mq from "./mod.ts";

Deno.test("mod exports important symbols", () => {
	assertExists(mq.Session);
	assertExists(mq.TrackWriter);
	assertExists(mq.Broadcast);
	assertExists(mq.NotFound);
	assertExists(mq.NotFoundTrackHandler);
});
