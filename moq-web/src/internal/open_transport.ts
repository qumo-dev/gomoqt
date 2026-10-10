import { WebTransportSession } from "./webtransport/mod.ts";

/** Opens one transport. It may throw, as `new WebTransport(url)` does for a bad URL. */
export type TransportOpener = () => WebTransport;

/** Thrown when a transport did not become ready in time and another is tried instead. */
export class TransportTimeoutError extends Error {
	constructor(timeoutMs: number) {
		super(`transport not ready after ${timeoutMs} ms`);
		this.name = "TransportTimeoutError";
	}
}

/**
 * Opens the first transport that becomes ready, trying the openers in order.
 *
 * Only a transport that fails to become ready is passed over: one that
 * becomes ready and then closes is the caller's, with whatever its close
 * says. A transport that is passed over is closed. Every opener but the last
 * has `timeoutMs` to become ready, so that a silent failure of the preferred
 * transport does not hold up the next; the last waits as long as it takes.
 *
 * @throws The failure of the last opener, when none becomes ready.
 */
export async function openFirstReady(
	openers: readonly TransportOpener[],
	timeoutMs: number,
): Promise<WebTransportSession> {
	let failure: unknown = new Error("no transport to open");
	for (const [index, open] of openers.entries()) {
		const isLast = index === openers.length - 1;
		let transport: WebTransportSession | undefined;
		try {
			transport = new WebTransportSession(open());
			await (isLast ? transport.ready : withTimeout(transport.ready, timeoutMs));
			return transport;
		} catch (err) {
			failure = err;
			transport?.close();
		}
	}
	throw failure;
}

/** Resolves or rejects with `promise`, or rejects once `timeoutMs` has passed. */
async function withTimeout<T>(promise: Promise<T>, timeoutMs: number): Promise<T> {
	let timer: ReturnType<typeof setTimeout> | undefined;
	const expired = new Promise<never>((_, reject) => {
		timer = setTimeout(() => reject(new TransportTimeoutError(timeoutMs)), timeoutMs);
	});
	try {
		return await Promise.race([promise, expired]);
	} finally {
		clearTimeout(timer);
	}
}
