import { assertEquals } from "@std/assert";
import type { ConnectInit, MoqOptions } from "./options.ts";
import { defaultProbeIntervalMs, defaultProbeMaxAgeMs, defaultProbeMaxDelta } from "./options.ts";

// Test configuration to ignore resource leaks from background operations
const testOptions = {
	sanitizeResources: false,
	sanitizeOps: false,
};

Deno.test("MoqOptions", testOptions, async (t) => {
	await t.step("should allow empty options", () => {
		// All options should be optional
		const emptyOptions: MoqOptions = {};
		assertEquals(emptyOptions.probeIntervalMs, undefined);
		assertEquals(emptyOptions.probeMaxAgeMs, undefined);
		assertEquals(emptyOptions.probeMaxDelta, undefined);
	});

	await t.step("should use defaults when fields are absent", () => {
		const options: MoqOptions = {};
		const intervalMs = options.probeIntervalMs ?? defaultProbeIntervalMs;
		const maxAgeMs = options.probeMaxAgeMs ?? defaultProbeMaxAgeMs;
		const maxDelta = options.probeMaxDelta ?? defaultProbeMaxDelta;
		assertEquals(intervalMs, defaultProbeIntervalMs);
		assertEquals(maxAgeMs, defaultProbeMaxAgeMs);
		assertEquals(maxDelta, defaultProbeMaxDelta);
	});

	await t.step("should support custom probe options", () => {
		const options: MoqOptions = {
			probeIntervalMs: 50,
			probeMaxAgeMs: 5_000,
			probeMaxDelta: 0.05,
		};

		assertEquals(options.probeIntervalMs, 50);
		assertEquals(options.probeMaxAgeMs, 5_000);
		assertEquals(options.probeMaxDelta, 0.05);
	});
});

Deno.test("ConnectInit", testOptions, async (t) => {
	await t.step("should allow empty init", () => {
		const emptyInit: ConnectInit = {};
		assertEquals(emptyInit.transportOptions, undefined);
		assertEquals(emptyInit.mux, undefined);
		assertEquals(emptyInit.onGoaway, undefined);
		assertEquals(emptyInit.transportFactory, undefined);
	});

	await t.step("should support transportOptions", () => {
		const transportOptions: WebTransportOptions = {
			allowPooling: true,
			congestionControl: "throughput",
		};

		const init: ConnectInit = {
			transportOptions: transportOptions,
		};

		assertEquals(init.transportOptions, transportOptions);
		assertEquals(init.transportOptions?.allowPooling, true);
		assertEquals(init.transportOptions?.congestionControl, "throughput");
	});
});
