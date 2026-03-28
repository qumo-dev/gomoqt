import { assertEquals } from "@std/assert";
import type { MOQOptions } from "./options.ts";

// Test configuration to ignore resource leaks from background operations
const testOptions = {
	sanitizeResources: false,
	sanitizeOps: false,
};

Deno.test("MOQOptions", testOptions, async (t) => {
	await t.step("should define the correct interface structure", () => {
		// This is a type-only test to ensure the interface is correctly defined
		const mockOptions: MOQOptions = {
			reconnect: true,
		};

		assertEquals(mockOptions.reconnect, true);
	});

	await t.step("should allow empty options", () => {
		// All options should be optional
		const emptyOptions: MOQOptions = {};
		assertEquals(emptyOptions.reconnect, undefined);
		assertEquals(emptyOptions.transportOptions, undefined);
	});

	await t.step("should allow options with reconnect", () => {
		const options: MOQOptions = {
			reconnect: false,
		};

		assertEquals(options.reconnect, false);
	});

	await t.step("should support partial assignment", () => {
		// Should be able to create options incrementally
		const options: MOQOptions = {};

		// Initially no reconnect setting
		assertEquals(options.reconnect, undefined);

		// Can set reconnect later
		options.reconnect = true;
		assertEquals(options.reconnect, true);
	});

	await t.step("should support transportOptions", () => {
		const transportOptions: WebTransportOptions = {
			allowPooling: true,
			congestionControl: "throughput",
		};

		const options: MOQOptions = {
			transportOptions: transportOptions,
		};

		assertEquals(options.transportOptions, transportOptions);
		assertEquals(options.transportOptions?.allowPooling, true);
		assertEquals(options.transportOptions?.congestionControl, "throughput");
	});
});
