import { assertEquals, assertInstanceOf, assertRejects } from "@std/assert";
import { tryInOrder } from "./try_in_order.ts";

type Outcome = "ok" | "fail" | "hang";

// fakeDialer answers each candidate with its outcome: "ok" resolves with
// "<candidate>-value", "fail" rejects with Error(candidate), and "hang"
// settles only when its signal aborts. It records what was tried and which
// attempts saw their signal abort.
class FakeDialer {
	readonly tried: string[] = [];
	readonly aborted: string[] = [];

	constructor(readonly outcomes: Readonly<Record<string, Outcome>>) {}

	attempt = (candidate: string, signal: AbortSignal): Promise<string> => {
		this.tried.push(candidate);
		signal.addEventListener("abort", () => this.aborted.push(candidate), { once: true });
		switch (this.outcomes[candidate] ?? "ok") {
			case "ok":
				return Promise.resolve(`${candidate}-value`);
			case "fail":
				return Promise.reject(new Error(candidate));
			case "hang":
				return new Promise((_, reject) => {
					signal.addEventListener("abort", () => reject(signal.reason), { once: true });
				});
		}
	};
}

Deno.test("tryInOrder returns the first candidate that succeeds", async () => {
	const dialer = new FakeDialer({ a: "fail", b: "ok" });

	const got = await tryInOrder(["a", "b"], dialer.attempt, { attemptTimeout: 0 });

	assertEquals(got, { value: "b-value", candidate: "b", index: 1 });
});

Deno.test("tryInOrder tries nothing after a success", async () => {
	const dialer = new FakeDialer({ a: "ok", b: "ok" });

	await tryInOrder(["a", "b"], dialer.attempt, { attemptTimeout: 0 });

	assertEquals(dialer.tried, ["a"]);
});

Deno.test("tryInOrder leaves a successful attempt's signal unaborted", async () => {
	const dialer = new FakeDialer({ a: "ok" });

	await tryInOrder(["a"], dialer.attempt, { attemptTimeout: 20 });
	await new Promise((resolve) => setTimeout(resolve, 40));

	assertEquals(dialer.aborted, []);
});

Deno.test("tryInOrder rejects with every error, in order, when all fail", async () => {
	const dialer = new FakeDialer({ a: "fail", b: "fail" });

	const err = await assertRejects(
		() => tryInOrder(["a", "b"], dialer.attempt, { attemptTimeout: 0 }),
		AggregateError,
	);

	assertEquals(err.errors.map((e: Error) => e.message), ["a", "b"]);
});

Deno.test("tryInOrder refuses an empty list", async () => {
	const dialer = new FakeDialer({});

	await assertRejects(() => tryInOrder([], dialer.attempt, { attemptTimeout: 0 }), TypeError);
});

Deno.test("tryInOrder aborts an attempt that times out and tries the next", async () => {
	const dialer = new FakeDialer({ a: "hang", b: "ok" });

	const got = await tryInOrder(["a", "b"], dialer.attempt, { attemptTimeout: 10 });

	assertEquals({ candidate: got.candidate, aborted: dialer.aborted }, {
		candidate: "b",
		aborted: ["a"],
	});
});

Deno.test("tryInOrder records a timeout as a TimeoutError", async () => {
	const dialer = new FakeDialer({ a: "hang" });

	const err = await assertRejects(
		() => tryInOrder(["a"], dialer.attempt, { attemptTimeout: 10 }),
		AggregateError,
	);

	assertInstanceOf(err.errors[0], DOMException);
	assertEquals(err.errors[0].name, "TimeoutError");
});

Deno.test("tryInOrder stops with the signal's reason when it aborts", async () => {
	const dialer = new FakeDialer({ a: "hang", b: "ok" });
	const controller = new AbortController();
	const reason = new Error("unmounted");
	setTimeout(() => controller.abort(reason), 10);

	const err = await assertRejects(() =>
		tryInOrder(["a", "b"], dialer.attempt, {
			attemptTimeout: 0,
			signal: controller.signal,
		})
	);

	assertEquals({ err, tried: dialer.tried, aborted: dialer.aborted }, {
		err: reason,
		tried: ["a"],
		aborted: ["a"],
	});
});

Deno.test("tryInOrder tries nothing when the signal has already aborted", async () => {
	const dialer = new FakeDialer({ a: "ok" });
	const reason = new Error("gone");

	const err = await assertRejects(() =>
		tryInOrder(["a"], dialer.attempt, {
			attemptTimeout: 0,
			signal: AbortSignal.abort(reason),
		})
	);

	assertEquals({ err, tried: dialer.tried }, { err: reason, tried: [] });
});
