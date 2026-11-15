/**
 * The outcome of {@link tryInOrder}: the first candidate that succeeded,
 * what it produced, and its place in the list.
 */
export interface Tried<C, T> {
	readonly value: T;
	readonly candidate: C;
	readonly index: number;
}

/** Options for {@link tryInOrder}. */
export interface TryInOrderOptions {
	/**
	 * How long one attempt may take, in milliseconds, before it is aborted
	 * and the next candidate is tried. Zero or less means no limit.
	 */
	readonly attemptTimeout: number;
	/** Aborts the attempt in progress and the ones not yet made. */
	readonly signal?: AbortSignal;
}

/**
 * Tries each candidate in turn with `attempt` and returns the first that
 * succeeds. Candidates are tried one at a time, in the order given: the
 * order is the caller's preference, and trying them all at once would load
 * every one of them for a single winner.
 *
 * Each attempt is passed a signal that aborts when it times out or when
 * `options.signal` aborts. An attempt that settles after its signal aborted
 * is not waited for: `attempt` is expected to release what it opened when it
 * sees the abort, including on a late success.
 *
 * Rejects with `options.signal`'s reason when it aborts, and with an
 * {@link AggregateError} of every attempt's error, in order, when all fail.
 *
 * @throws TypeError when `candidates` is empty.
 */
export async function tryInOrder<C, T>(
	candidates: readonly C[],
	attempt: (candidate: C, signal: AbortSignal) => Promise<T>,
	options: TryInOrderOptions,
): Promise<Tried<C, T>> {
	if (candidates.length === 0) {
		throw new TypeError("no candidates to try");
	}
	const errors: unknown[] = [];
	for (const [index, candidate] of candidates.entries()) {
		options.signal?.throwIfAborted();
		const result = await attemptOnce(candidate, attempt, options);
		if (result[1] === undefined) {
			return { value: result[0], candidate, index };
		}
		options.signal?.throwIfAborted();
		errors.push(result[1].cause);
	}
	throw new AggregateError(errors, `all ${candidates.length} candidates failed`);
}

/** A failed attempt, holding what it failed with. */
class Failed {
	constructor(readonly cause: unknown) {}
}

/**
 * Runs one attempt under its own abort signal, which also fires on timeout.
 * Settles as soon as the signal aborts, whether or not the attempt has.
 */
async function attemptOnce<C, T>(
	candidate: C,
	attempt: (candidate: C, signal: AbortSignal) => Promise<T>,
	options: TryInOrderOptions,
): Promise<readonly [T, undefined] | readonly [undefined, Failed]> {
	const controller = new AbortController();
	const onAbort = () => controller.abort(options.signal?.reason);
	options.signal?.addEventListener("abort", onAbort, { once: true });
	const timer = options.attemptTimeout > 0
		? setTimeout(
			() =>
				controller.abort(
					new DOMException(
						`attempt timed out after ${options.attemptTimeout} ms`,
						"TimeoutError",
					),
				),
			options.attemptTimeout,
		)
		: undefined;
	const aborted = new Promise<never>((_, reject) => {
		controller.signal.addEventListener(
			"abort",
			() => reject(controller.signal.reason),
			{ once: true },
		);
	});
	// The race below observes the rejection when the abort comes first; one
	// that comes after the attempt settled has no one to tell.
	aborted.catch(() => {});
	try {
		const pending = attempt(candidate, controller.signal);
		// An attempt abandoned on abort may still reject later, unobserved.
		pending.catch(() => {});
		const value = await Promise.race([pending, aborted]);
		return [value, undefined];
	} catch (err) {
		return [undefined, new Failed(err)];
	} finally {
		clearTimeout(timer);
		options.signal?.removeEventListener("abort", onAbort);
	}
}
