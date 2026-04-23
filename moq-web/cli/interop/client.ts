import { connect, FetchRequest, Frame, TrackMux, TrackWriter } from "@qumo/moq";
import { background } from "@okdaichi/golikejs/context";

// shared client logic exported as function
export async function runClient(
	addr: string,
	transportOptions: WebTransportOptions,
	debugEnabled: boolean,
): Promise<void> {
	// GOAWAY handling
	let goawayResolve: ((uri: string) => void) | undefined;
	const goawayPromise = new Promise<string>((resolve) => {
		goawayResolve = resolve;
	});

	// basic prefixed log functions
	function info(msg: string, ...args: any[]) {
		console.log(msg, ...args);
	}
	function debug(msg: string, ...args: any[]) {
		console.debug(msg, ...args);
	}

	// helper to log a step and mark success/failure on one line
	async function write(s: string) {
		const encoder = new TextEncoder();
		await Deno.stdout.write(encoder.encode(s));
	}

	async function step<T>(msg: string, fn: () => Promise<T>): Promise<T> {
		await write(`${msg}...`);
		try {
			const res = await fn();
			console.log(" ok");
			return res;
		} catch (err: any) {
			console.log(" failed:", err instanceof Error ? err.message : err);
			throw err;
		}
	}

	if (!debugEnabled) {
		console.debug = () => {};
	}

	// Channel to signal publish handler completion
	const doneCh: Array<() => void> = [];
	let done = false;

	// Signal to defer publishing until all probe/fetch steps are done
	let readyToPublish: () => void = () => {};
	const readyToPublishPromise = new Promise<void>((resolve) => {
		readyToPublish = resolve;
	});

	const mux = new TrackMux();

	mux.publishFunc(
		background().done(),
		"/interop/client",
		async (track: TrackWriter) => {
			try {
				debug("Server subscribed, waiting for ready signal...");
				await readyToPublishPromise;
				debug("Ready signal received, sending data...");

				const group = await step("Opening group", async () => {
					const [g, err] = await track.openGroup();
					if (err) throw err;
					return g;
				});
				const frame = new Frame(new TextEncoder().encode("Hello from moq-ts client"));
				await step("Writing frame to server", () => group.writeFrame(frame));
				await group.close();
			} catch (e) {
				console.error("Error in publish:", e);
			} finally {
				done = true;
				doneCh.forEach((resolve) => resolve());
			}
		},
	);

	debug("Registering /interop/client handler");

	const session = await step("Connecting to server", () =>
		connect(addr, {
			mux,
			transportOptions,
			onGoaway: (newSessionURI: string) => {
				console.log(`ok (newSessionURI: ${newSessionURI})`);
				if (goawayResolve) goawayResolve(newSessionURI);
			},
		}));

	const announced = await step("Accepting server announcements", async () => {
		const [a, err] = await session.acceptAnnounce("/");
		if (err) throw err;
		return a;
	});

	const announcement = await step("Receiving announcement", async () => {
		const [a, err] = await announced.receive(background().done());
		if (err) throw err;
		return a;
	});

	info(`Discovered broadcast: ${announcement.broadcastPath}`);

	const track = await step("Subscribing to broadcast", async () => {
		const [t, err] = await session.subscribe(
			announcement.broadcastPath,
			"",
		);
		if (err) throw err;
		return t;
	});

	const group = await step("Accepting group", async () => {
		const [g, err] = await track.acceptGroup(background().done());
		if (err) throw err;
		return g;
	});

	await step("Reading frame from server", async () => {
		const frame = new Frame(new Uint8Array(1024));
		const err = await group.readFrame(frame);
		if (err) throw err;
		info("Frame data length:", frame.bytes.byteLength);
		info("Received data from server:", new TextDecoder().decode(frame.bytes));
	});

	// Step 3: Probe the server bitrate (before Fetch to ensure connection is still up)
	await step("Probing server bitrate", async () => {
		const [resultGen, err] = await session.probe(1_000_000);
		if (err) throw err;
		const result = await resultGen!.next();
		if (result.done || result.value === undefined) {
			throw new Error("probe stream ended without result");
		}
		info(`Probe result: ${result.value.bitrate} bps`);
	});

	// Step 4: Fetch a single group from the server
	const fetchGroup = await step("Fetching group from server", async () => {
		const req = new FetchRequest({
			broadcastPath: announcement.broadcastPath,
			trackName: "",
			priority: 0,
			groupSequence: 0,
		});
		const [g, err] = await session.fetch(req);
		if (err) throw err;
		return g;
	});

	await step("Reading frame via Fetch", async () => {
		const frame = new Frame(new Uint8Array(1024));
		const err = await fetchGroup.readFrame(frame);
		if (err) throw err;
		info("Fetch payload:", new TextDecoder().decode(frame.bytes));
	});

	// All probe/fetch steps done — signal publishFunc to proceed
	readyToPublish();

	debug("Operations completed");

	if (!done) {
		let doneTimerId: ReturnType<typeof setTimeout>;
		const doneTimeout = new Promise<void>((resolve) => {
			doneTimerId = setTimeout(() => resolve(), 5000);
		});
		await Promise.race([
			new Promise<void>((resolve) => doneCh.push(resolve)),
			doneTimeout,
		]);
		clearTimeout(doneTimerId!);
	}

	// Wait for GOAWAY from server (non-fatal timeout, matching Go client behavior)
	await write("Waiting for GOAWAY...");
	let goawayTimerId: ReturnType<typeof setTimeout>;
	const goawayTimeout = new Promise<string>((resolve) => {
		goawayTimerId = setTimeout(() => resolve(""), 5000);
	});
	const goawayURI = await Promise.race([goawayPromise, goawayTimeout]);
	clearTimeout(goawayTimerId!);
	if (goawayURI) {
		console.log(" ok");
	} else {
		console.log(" failed: timed out");
	}

	await step("Closing session", () => session.closeWithError(0, "no error"));
}
