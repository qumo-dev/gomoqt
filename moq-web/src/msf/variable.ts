/**
 * Variable substitution (draft-ietf-moq-msf-02 §5.4). Catalog values may
 * hold `%name%` references, resolved at delivery time from the fragment of
 * the URI the catalog was requested with. Resolution is a client-side
 * concern: nothing here runs as part of {@link validateCatalog}.
 *
 * @module
 */
import { type Catalog, parseCatalog, stringifyCatalog, validateCatalog } from "./catalog.ts";

/** The track field naming a network endpoint, which is not substituted (§13.5). */
const CONNECTION_URI_KEY = "connectionURI";

/**
 * Legal substituted values: alphanumerics, `-`, `_`, `+`, `/`, `=`, `.` and
 * `@`. The restriction prevents injection into catalog values.
 */
const VARIABLE_VALUE = /^[A-Za-z0-9\-_+/=.@]*$/;

/**
 * Parse the variables of a catalog URI fragment (draft-ietf-moq-msf-02
 * §5.4.2). The fragment, with or without its leading `#`, splits at each `&`
 * into pairs, and each pair at its first `=` into a name and a value, so a
 * value may itself contain `=`. Pairs without `=` (such as the
 * `msf:namespace--name` track locator) or with an empty name are skipped.
 * When a name repeats the first value is kept. Values are not
 * percent-decoded.
 */
export function parseFragmentVariables(fragment: string): Map<string, string> {
	const vars = new Map<string, string>();
	const body = fragment.startsWith("#") ? fragment.slice(1) : fragment;
	for (const pair of body.split("&")) {
		const eq = pair.indexOf("=");
		if (eq <= 0) {
			continue;
		}
		const name = pair.slice(0, eq);
		if (!vars.has(name)) {
			vars.set(name, pair.slice(eq + 1));
		}
	}
	return vars;
}

/**
 * Substitute the `%name%` references in s from vars. Returns undefined when
 * a `%` is not part of a `%name%` reference, a referenced variable is
 * missing, or its value holds a character the spec disallows.
 */
export function resolveVariables(
	s: string,
	vars: ReadonlyMap<string, string>,
): string | undefined {
	let out = "";
	let i = 0;
	while (i < s.length) {
		const start = s.indexOf("%", i);
		if (start < 0) {
			return out + s.slice(i);
		}
		const end = s.indexOf("%", start + 1);
		if (end < 0) {
			return undefined;
		}
		const name = s.slice(start + 1, end);
		const value = name === "" ? undefined : vars.get(name);
		if (value === undefined || !VARIABLE_VALUE.test(value)) {
			return undefined;
		}
		out += s.slice(i, start) + value;
		i = end + 1;
	}
	return out;
}

/**
 * Check that every `%` in s is part of a well-formed, non-empty `%name%`
 * reference (draft-ietf-moq-msf-02 §5.4.1). Whether the variable exists is
 * not checked; see {@link resolveVariables}.
 * @throws Error on an unterminated reference or an empty name.
 */
export function validateVariableSyntax(s: string): void {
	let i = 0;
	while (i < s.length) {
		const start = s.indexOf("%", i);
		if (start < 0) {
			return;
		}
		const end = s.indexOf("%", start + 1);
		if (end < 0) {
			throw new Error(`msf: unterminated variable reference in ${JSON.stringify(s)}`);
		}
		if (end === start + 1) {
			throw new Error(`msf: empty variable name in ${JSON.stringify(s)}`);
		}
		i = end + 1;
	}
}

function resolveValue(value: unknown, vars: ReadonlyMap<string, string>, key: string): unknown {
	if (typeof value === "string") {
		if (!value.includes("%")) {
			return value;
		}
		if (key === CONNECTION_URI_KEY) {
			throw new Error(
				`msf: variables in connectionURI are not substituted: ${JSON.stringify(value)}`,
			);
		}
		const resolved = resolveVariables(value, vars);
		if (resolved === undefined) {
			throw new Error(`msf: failed to resolve variables in ${JSON.stringify(value)}`);
		}
		return resolved;
	}
	if (Array.isArray(value)) {
		return value.map((element) => resolveValue(element, vars, ""));
	}
	if (typeof value === "object" && value !== null) {
		return Object.fromEntries(
			Object.entries(value).map(([k, member]) => [k, resolveValue(member, vars, k)]),
		);
	}
	return value;
}

/**
 * Substitute variables in every string value of a catalog, nested objects
 * and extra fields included, then validate the result, since a catalog must
 * be validated after substitution (draft-ietf-moq-msf-02 §5.4.1, §13.5).
 * Object keys and non-string values are kept.
 *
 * As §13.5 advises, values that determine a network endpoint are not
 * substituted: a connectionURI holding a variable reference is an error. A
 * caller that trusts the URI source can resolve it with
 * {@link resolveVariables}.
 * @throws Error if a reference cannot be resolved, and
 * {@link ValidationError} if the resolved catalog is invalid.
 */
export function resolveCatalogVariables(
	catalog: Catalog,
	vars: ReadonlyMap<string, string>,
): Catalog {
	const tree: unknown = JSON.parse(stringifyCatalog(catalog));
	const resolved = parseCatalog(JSON.stringify(resolveValue(tree, vars, "")));
	if (catalog.defaultNamespace !== undefined) {
		resolved.defaultNamespace = catalog.defaultNamespace;
	}
	validateCatalog(resolved);
	return resolved;
}
