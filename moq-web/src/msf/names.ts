/**
 * Naming rules of draft-ietf-moq-msf-02 §5.2.2 and §11.2: how a catalog
 * namespace string maps to MOQT Track Namespace Fields, and which strings
 * may name a track.
 *
 * @module
 */

/** The most Track Namespace Fields a catalog namespace may yield. */
const MAX_NAMESPACE_FIELDS = 32;

/** Matches a lone (unpaired) UTF-16 surrogate, which has no UTF-8 encoding. */
const LONE_SURROGATE = /\p{Cs}/u;

/**
 * Splits a catalog namespace string into the Track Namespace Fields it
 * denotes (draft-ietf-moq-msf-02 §5.2.2). Every `/` is a field boundary. The
 * string must yield 1 to 32 fields, none empty, each satisfying the naming
 * rules of §11.2 (no NUL, no lone surrogate). The fields are returned as
 * given, without normalization or case folding.
 * @throws Error if the namespace breaks these rules.
 */
export function splitNamespace(namespace: string): string[] {
	const problem = namespaceProblem(namespace);
	if (problem !== undefined) {
		throw new Error(`msf: ${problem}`);
	}
	return namespace.split("/");
}

/** Describes what makes namespace invalid, or returns undefined. */
export function namespaceProblem(namespace: string): string | undefined {
	if (namespace === "") {
		return "namespace must not be empty";
	}
	const fields = namespace.split("/");
	if (fields.some((field) => field === "")) {
		return `namespace ${JSON.stringify(namespace)} contains an empty field`;
	}
	if (fields.length > MAX_NAMESPACE_FIELDS) {
		return `namespace ${
			JSON.stringify(namespace)
		} has ${fields.length} fields; at most ${MAX_NAMESPACE_FIELDS} are allowed`;
	}
	return nameTextProblem(namespace);
}

/**
 * Applies the string-to-bytes rules of draft-ietf-moq-msf-02 §11.2.1 to a
 * naming field: it must have a UTF-8 encoding, so no lone surrogate, and
 * must not contain U+0000. Returns the problem, or undefined.
 */
export function nameTextProblem(s: string): string | undefined {
	if (s.includes("\u0000")) {
		return `${JSON.stringify(s)} contains a NUL character`;
	}
	if (LONE_SURROGATE.test(s)) {
		return `${JSON.stringify(s)} is not valid UTF-8`;
	}
	return undefined;
}
