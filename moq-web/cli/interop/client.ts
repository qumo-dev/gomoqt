import { Client, Frame, TrackMux, TrackWriter } from "@okdaichi/moq";
import { background } from "@okdaichi/golikejs/context";

// shared client logic exported as function
export async function runClient(
	addr: string,
	transportOptions: WebTransportOptions,
	debugEnabled: boolean,
): Promise<void> {
	const client = new Client({ transportOptions });
	const mux = new TrackMux();

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

	mux.publishFunc(
		background().done(),
		"/interop/client",
		async (track: TrackWriter) => {
			try {
				debug("Server subscribed, sending data...");

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

	const session = await step("Connecting to server", () => client.dial(addr, mux));

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

	debug("Operations completed");

	if (!done) {
		await Promise.race([
			new Promise<void>((resolve) => doneCh.push(resolve)),
			new Promise<void>((resolve) => setTimeout(() => resolve(), 5000)),
		]);
	}

	await new Promise((resolve) => setTimeout(resolve, 2000));

	await step("Closing session", () => session.closeWithError(0, "no error"));
}
