import { assertEquals } from "@std/assert";
import { ProbeLevels, SetupMessage } from "./setup.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";

Deno.test("SetupMessage - encode/decode roundtrip", async (t) => {
	await t.step("empty parameter list", async () => {
		const buffer = Buffer.make(64);
		const message = new SetupMessage({});
		assertEquals(await message.encode(buffer), undefined);

		const decoded = new SetupMessage({});
		assertEquals(await decoded.decode(buffer), undefined);
		assertEquals(decoded.parameters.length, 0);
		assertEquals(decoded.probeLevel(), ProbeLevels.None);
		assertEquals(decoded.path(), undefined);
	});

	await t.step("probe parameter", async () => {
		const buffer = Buffer.make(64);
		const message = new SetupMessage({});
		message.addProbe(ProbeLevels.Report);
		assertEquals(await message.encode(buffer), undefined);

		const decoded = new SetupMessage({});
		assertEquals(await decoded.decode(buffer), undefined);
		assertEquals(decoded.probeLevel(), ProbeLevels.Report);
	});

	await t.step("path parameter", async () => {
		const buffer = Buffer.make(64);
		const message = new SetupMessage({});
		message.addPath("/relay/live");
		assertEquals(await message.encode(buffer), undefined);

		const decoded = new SetupMessage({});
		assertEquals(await decoded.decode(buffer), undefined);
		assertEquals(decoded.path(), "/relay/live");
	});

	await t.step("unknown parameter is preserved", async () => {
		const buffer = Buffer.make(64);
		const message = new SetupMessage({
			parameters: [{ id: 0x7f, value: new Uint8Array([1, 2]) }],
		});
		assertEquals(await message.encode(buffer), undefined);

		const decoded = new SetupMessage({});
		assertEquals(await decoded.decode(buffer), undefined);
		assertEquals(decoded.parameters.length, 1);
		assertEquals(decoded.parameters[0]?.id, 0x7f);
		assertEquals(decoded.parameters[0]?.value, new Uint8Array([1, 2]));
	});

	await t.step("duplicate parameter is rejected", async () => {
		const buffer = Buffer.make(64);
		const message = new SetupMessage({});
		message.addProbe(ProbeLevels.Report);
		message.addProbe(ProbeLevels.Increase);
		assertEquals(await message.encode(buffer), undefined);

		const decoded = new SetupMessage({});
		const err = await decoded.decode(buffer);
		assertEquals(err instanceof Error, true);
	});

	await t.step("decode returns error on empty input", async () => {
		const buffer = Buffer.make(0);
		const decoded = new SetupMessage({});
		const err = await decoded.decode(buffer);
		assertEquals(err !== undefined, true);
	});
});
