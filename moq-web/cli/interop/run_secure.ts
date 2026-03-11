import { join } from "@std/path";

// 1. Get mkcert CAROOT
// We use "cmd /c mkcert -CAROOT" on Windows to ensure it runs correctly if it's a batch file,
// but usually calling "mkcert" directly works if it's in PATH.
const cmd = Deno.build.os === "windows" ? "mkcert.exe" : "mkcert";

function hasFlag(args: string[], name: string): boolean {
	return args.includes(name);
}

async function computeHash(path: string): Promise<string> {
	const pem = await Deno.readTextFile(path);
	const m = pem.match(/-----BEGIN CERTIFICATE-----(.*?)-----END CERTIFICATE-----/s);
	if (!m) {
		throw new Error("no certificate block");
	}
	const certificateBody = m[1];
	if (certificateBody === undefined) {
		throw new Error("certificate body missing");
	}
	const b64 = certificateBody.replace(/\s+/g, "");
	const der = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
	const hashBuf = await crypto.subtle.digest("SHA-256", der);
	const hashBytes = new Uint8Array(hashBuf);
	return btoa(String.fromCharCode(...hashBytes));
}

try {
	const mkcert = new Deno.Command(cmd, {
		args: ["-CAROOT"],
		stdout: "piped",
		stderr: "inherit",
	});
	const output = await mkcert.output();

	if (!output.success) {
		console.error(
			"Error: 'mkcert -CAROOT' failed. Please ensure mkcert is installed and in your PATH.",
		);
		Deno.exit(1);
	}

	const caRoot = new TextDecoder().decode(output.stdout).trim();
	const certPath = join(caRoot, "rootCA.pem");

	console.log(`[Secure Wrapper] Using Root CA from: ${certPath}`);

	const forwardedArgs = [...Deno.args];

	// if no cert-hash was provided, try to compute it from the server's pem file
	if (!hasFlag(forwardedArgs, "--cert-hash")) {
		try {
			// path relative to moq-web directory where this wrapper runs
			const certFile = join("..", "cmd", "interop", "server", "localhost.pem");
			const hash = await computeHash(certFile);
			console.log(`[Secure Wrapper] computed cert hash: ${hash}`);
			forwardedArgs.push("--insecure", "--cert-hash", hash);
		} catch (e) {
			console.warn("[Secure Wrapper] failed to compute certificate hash:", e);
		}
	}

	// 2. Run the actual interop client with the cert
	const child = new Deno.Command(Deno.execPath(), {
		args: [
			"run",
			"--unstable-net",
			"--allow-all",
			"--cert",
			certPath,
			"cli/interop/main.ts",
			...forwardedArgs,
		],
		stdout: "inherit",
		stderr: "inherit",
		stdin: "inherit",
	});

	const status = await child.spawn().status;
	Deno.exit(status.code);
} catch (err) {
	if (err instanceof Deno.errors.NotFound) {
		console.error("Error: 'mkcert' not found in PATH. Please install mkcert.");
	} else {
		console.error("Error running secure wrapper:", err);
	}
	Deno.exit(1);
}
