import { parseArgs } from "@std/cli/parse-args";
import { runClient } from "./client.ts";

async function main() {
	const args = parseArgs(Deno.args, {
		string: ["addr", "cert-hash"],
		boolean: ["insecure", "debug"],
		default: { addr: "https://localhost:9000", insecure: false, debug: false },
	});

	// Suppress debug logs unless --debug flag is provided
	if (!args.debug) {
		console.debug = () => {};
	}

	const addr = args.addr;

	console.log(`Connecting to server at ${addr}`);

	// For local development with self-signed certificates
	let transportOptions: WebTransportOptions = {};

	if (args.insecure && args["cert-hash"]) {
		// Use provided certificate hash for localhost development
		const hashBase64 = args["cert-hash"];
		const hashBytes = Uint8Array.from(atob(hashBase64), (c) => c.charCodeAt(0));

		transportOptions = {
			serverCertificateHashes: [{
				algorithm: "sha-256",
				value: hashBytes,
			}],
		};
		console.log("[DEV] Using provided certificate hash for self-signed cert");
	}

	// delegate the heavy lifting to shared client logic
	await runClient(addr, transportOptions, args.debug);
}

if (import.meta.main) {
	main().catch((err) => {
		console.error("Fatal error:", err);
		Deno.exit(1);
	});
}
