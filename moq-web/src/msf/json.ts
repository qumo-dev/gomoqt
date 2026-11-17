/**
 * JSON decoding helpers shared by the msf modules. Internal: not re-exported
 * from `mod.ts`.
 *
 * @module
 */
import type { z } from "zod";

/** Matches a lone (unpaired) UTF-16 surrogate. */
const LONE_SURROGATE = /\p{Cs}/u;

/**
 * Decodes a catalog document. It rejects what draft-ietf-moq-msf-02 §11.2.1
 * forbids and a lenient decoder would accept by substituting U+FFFD: a
 * leading byte order mark, bytes that are not UTF-8, and an unpaired
 * surrogate anywhere in the document, keys included.
 */
export function decodeCatalogJSON(data: string | Uint8Array): unknown {
	let text: string;
	if (typeof data === "string") {
		text = data;
	} else {
		try {
			// ignoreBOM keeps a leading BOM in the text so it is rejected below.
			text = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(data);
		} catch (e) {
			throw new Error("msf: catalog is not valid UTF-8", { cause: e });
		}
	}
	if (text.startsWith("﻿")) {
		throw new Error("msf: catalog must not begin with a byte order mark");
	}
	const value: unknown = JSON.parse(text);
	if (containsLoneSurrogate(value)) {
		throw new Error("msf: catalog contains an unpaired surrogate");
	}
	return value;
}

function containsLoneSurrogate(value: unknown): boolean {
	if (typeof value === "string") {
		return LONE_SURROGATE.test(value);
	}
	if (Array.isArray(value)) {
		return value.some(containsLoneSurrogate);
	}
	if (typeof value === "object" && value !== null) {
		return Object.entries(value).some(([key, member]) =>
			LONE_SURROGATE.test(key) || containsLoneSurrogate(member)
		);
	}
	return false;
}

/** Converts the first issue of a zod error into an Error with prefix. */
export function zodSchemaError(prefix: string, error: z.ZodError): Error {
	const issue = error.issues[0];
	if (!issue) {
		return new Error(prefix);
	}
	const path = issue.path.length > 0 ? ` at ${issue.path.join(".")}` : "";
	return new Error(`${prefix}${path}: ${issue.message}`);
}

/** Narrows value to a plain JSON object, or throws errorMessage. */
export function asRecord(value: unknown, errorMessage: string): Record<string, unknown> {
	if (typeof value !== "object" || value === null || Array.isArray(value)) {
		throw new Error(errorMessage);
	}
	// reason: checked above to be a non-null, non-array object.
	return value as Record<string, unknown>;
}

/** Splits raw into the members named in known and the rest. */
export function splitKnown(
	raw: Record<string, unknown>,
	known: ReadonlySet<string>,
): [known: Record<string, unknown>, extra: Record<string, unknown> | undefined] {
	const knownPart: Record<string, unknown> = {};
	const extra: Record<string, unknown> = {};
	let hasExtra = false;
	for (const [key, value] of Object.entries(raw)) {
		if (known.has(key)) {
			knownPart[key] = value;
		} else {
			extra[key] = value;
			hasExtra = true;
		}
	}
	return [knownPart, hasExtra ? extra : undefined];
}

/** An object type with null and undefined dropped from every field. */
export type Present<T> = { [K in keyof T]?: Exclude<T[K], null | undefined> };

/** Returns a copy of obj without its null and undefined fields. */
export function withoutNullish<T extends object>(obj: T): Present<T> {
	const out: Record<string, unknown> = {};
	for (const [key, value] of Object.entries(obj)) {
		if (value !== null && value !== undefined) {
			out[key] = value;
		}
	}
	// reason: out holds exactly obj's fields that are neither null nor undefined.
	return out as Present<T>;
}
