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
	 * How long one attempt may take, in milliseconds, before its signal
	 * aborts. Zero or less means no limit.
	 */
	readonly attemptTimeout: number;
	/** Aborts the attempt in progress, and no further candidate is tried. */
	readonly signal?: AbortSignal;
}

/**
 * Tries each candidate in turn with `attempt` and returns the first that
 * succeeds. Candidates are tried one at a time, in the order given: the
 * order is the caller's preference, and trying them all at once would load
 * every one of them for a single winner.
 *
 * Each attempt is passed a signal that aborts when the attempt times out or
 * when `options.signal` aborts. `attempt` must settle promptly once its
 * signal aborts, releasing what it opened, and reject with an error that
 * says so: that error is the attempt's, like any other failure.
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
		if (result.ok) {
			return { value: result.value, candidate, index };
		}
		options.signal?.throwIfAborted();
		errors.push(result.error);
	}
	throw new AggregateError(errors, `all ${candidates.length} candidates failed`);
}

type Outcome<T> =
	| { readonly ok: true; readonly value: T }
	| { readonly ok: false; readonly error: unknown };

/** Runs one attempt under its own signal, which also aborts on timeout. */
async function attemptOnce<C, T>(
	candidate: C,
	attempt: (candidate: C, signal: AbortSignal) => Promise<T>,
	options: TryInOrderOptions,
): Promise<Outcome<T>> {
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
	try {
		return { ok: true, value: await attempt(candidate, controller.signal) };
	} catch (error) {
		return { ok: false, error };
	} finally {
		clearTimeout(timer);
		options.signal?.removeEventListener("abort", onAbort);
	}
}
