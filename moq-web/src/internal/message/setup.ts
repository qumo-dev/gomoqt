import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, putVarint, readFull, readVarint } from "./message.ts";
import { varintLen } from "../webtransport/len.ts";

/** Setup Parameter IDs defined by moq-lite-05. */
export const SetupParamIDs = {
	/** Probe capability level advertisement. */
	Probe: 0x1,
	/** Request path for bindings without a request URI. */
	Path: 0x2,
} as const;

/**
 * Probe capability levels carried by the Probe Setup Parameter.
 * Each level includes the ones below it.
 */
export const ProbeLevels = {
	/** The publisher does not support probing. */
	None: 0,
	/** The publisher can measure and report its estimated bitrate. */
	Report: 1,
	/** The publisher can additionally pad the connection to probe for bandwidth. */
	Increase: 2,
} as const;

/** A single capability or extension advertisement within a SETUP message. */
export interface SetupParameter {
	id: number;
	value: Uint8Array;
}

export interface SetupMessageInit {
	parameters?: SetupParameter[];
}

/**
 * SETUP message, sent exactly once as the only message on a Setup Stream.
 *
 * ```text
 * SETUP Message {
 *   Message Length (i)
 *   Parameter Count (i)
 *   Setup Parameter (..) ...
 * }
 * ```
 */
export class SetupMessage {
	parameters: SetupParameter[];

	constructor(init: SetupMessageInit = {}) {
		this.parameters = init.parameters ?? [];
	}

	/** Appends a Probe parameter advertising the given capability level. */
	addProbe(level: number): void {
		const value = new Uint8Array(varintLen(level));
		putVarint(value, 0, level);
		this.parameters.push({ id: SetupParamIDs.Probe, value });
	}

	/** Appends a Path parameter carrying the request path. */
	addPath(path: string): void {
		this.parameters.push({
			id: SetupParamIDs.Path,
			value: new TextEncoder().encode(path),
		});
	}

	/**
	 * Returns the advertised probe capability level.
	 * Absent or malformed parameters mean {@link ProbeLevels.None}.
	 */
	probeLevel(): number {
		for (const p of this.parameters) {
			if (p.id !== SetupParamIDs.Probe) {
				continue;
			}
			if (p.value.length === 0) {
				return ProbeLevels.None;
			}
			const d = new MessageDecoder(p.value);
			return d.varint();
		}
		return ProbeLevels.None;
	}

	/** Returns the Path parameter value, or undefined if absent. */
	path(): string | undefined {
		for (const p of this.parameters) {
			if (p.id === SetupParamIDs.Path) {
				return new TextDecoder().decode(p.value);
			}
		}
		return undefined;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.varint(this.parameters.length);
		for (const p of this.parameters) {
			e.varint(p.id);
			e.bytes(p.value);
		}
		const [, err] = await w.write(e.frame());
		return err;
	}

	/**
	 * Decodes the message from the reader.
	 * Duplicate parameter IDs are a protocol violation and return an error.
	 */
	async decode(r: Reader): Promise<Error | undefined> {
		const [msgLen, , err1] = await readVarint(r);
		if (err1) return err1;

		const buf = new Uint8Array(msgLen);
		const [, err2] = await readFull(r, buf);
		if (err2) return err2;

		const d = new MessageDecoder(buf);

		const count = d.varint();
		const params: SetupParameter[] = [];
		const seen = new Set<number>();
		for (let i = 0; i < count; i++) {
			const id = d.varint();
			if (seen.has(id)) {
				return new Error("moq: duplicate setup parameter");
			}
			seen.add(id);
			const len = d.varint();
			// Copy the value out of the shared decode buffer.
			params.push({ id, value: new Uint8Array(d.bytes(len)) });
		}
		this.parameters = params;

		return undefined;
	}
}
