import { assertEquals, assertExists } from "@std/assert";
import { Queue } from "./queue.ts";

Deno.test("internal/queue - basic enqueue/dequeue and close behavior", async (t) => {
	await t.step("constructor and basic properties", () => {
		const q = new Queue<number>();
		assertExists(q);
		assertEquals(q.closed, false);
		q.close();
		assertEquals(q.closed, true);
	});

	await t.step("enqueue and dequeue FIFO order", async () => {
		const q = new Queue<number>();
		await q.enqueue(1);
		await q.enqueue(2);
		await q.enqueue(3);

		const a = await q.dequeue();
		const b = await q.dequeue();
		const c = await q.dequeue();

		assertEquals(a, 1);
		assertEquals(b, 2);
		assertEquals(c, 3);
		q.close();
	});

	await t.step("wait for items when empty", async () => {
		const q = new Queue<number>();
		let result: number | undefined;

		const p = q.dequeue().then((v) => {
			result = v;
		});

		// enqueue after a tick
		setTimeout(() => {
			q.enqueue(42);
		}, 0);

		await p;
		assertEquals(result, 42);
		q.close();
	});

	await t.step("multiple waiters receive items in order", async () => {
		const q = new Queue<number>();
		const results: (number | undefined)[] = [];

		const promises = Array.from({ length: 3 }).map((_, i) =>
			q.dequeue().then((v) => {
				results[i] = v;
			})
		);

		// enqueue values
		await q.enqueue(100);
		await q.enqueue(200);
		await q.enqueue(300);

		await Promise.all(promises);
		assertEquals(results, [100, 200, 300]);
		q.close();
	});

	await t.step("dequeue after close returns undefined when empty", async () => {
		const q = new Queue<number>();
		q.close();
		const v = await q.dequeue();
		assertEquals(v, undefined);
	});

	await t.step("remaining items can be dequeued after close", async () => {
		const q = new Queue<number>();
		await q.enqueue(1);
		await q.enqueue(2);
		q.close();
		const a = await q.dequeue();
		const b = await q.dequeue();
		const c = await q.dequeue();
		assertEquals(a, 1);
		assertEquals(b, 2);
		assertEquals(c, undefined);
	});

	await t.step("close can be called multiple times", async () => {
		const q = new Queue<number>();
		q.close();
		await new Promise((resolve) => setTimeout(resolve, 0)); // Ensure async close operations complete
		q.close(); // Second call should not error
		assertEquals(q.closed, true);
	});

	await t.step("close resolves pending dequeue", async () => {
		const q = new Queue<number>();
		let result: number | undefined;

		const p = q.dequeue().then((v) => {
			result = v;
		});

		// Close the queue
		q.close();

		await p;
		assertEquals(result, undefined);
	});

	await t.step("enqueue after close still adds items", async () => {
		const q = new Queue<number>();
		q.close();
		await q.enqueue(1); // Should still add the item
		const v = await q.dequeue();
		assertEquals(v, 1);
	});

	await t.step("tryEnqueue rejects items after close", async () => {
		const q = new Queue<number>();
		q.close();
		assertEquals(await q.tryEnqueue(1), false);
		assertEquals(await q.dequeue(), undefined);
	});

	await t.step("dequeue after multiple enqueues and closes", async () => {
		const q = new Queue<number>();
		await q.enqueue(1);
		await q.enqueue(2);
		q.close();
		await q.enqueue(3); // This should still be added
		const a = await q.dequeue();
		const b = await q.dequeue();
		const c = await q.dequeue();
		const d = await q.dequeue();
		assertEquals(a, 1);
		assertEquals(b, 2);
		assertEquals(c, 3);
		assertEquals(d, undefined);
	});

	await t.step("close asynchronous behavior", async () => {
		const q = new Queue<number>();
		// Start dequeue to set pending
		const dequeuePromise = q.dequeue();
		// Close the queue
		q.close();
		// Wait for async operations
		await dequeuePromise;
		await new Promise((resolve) => setTimeout(resolve, 0)); // Ensure close's then() executes
		// Close again to test early return
		q.close();
		assertEquals(q.closed, true);
	});
});
