/**
 * Helpers for the packagingMetadata track field (draft-ietf-moq-msf-02
 * §5.2.5) and the LOC stream mapping it carries (§4.1.1).
 *
 * @module
 */

/** Any string, while editors still suggest the listed literals. @internal */
export type OpenString = string & Record<never, never>;

/**
 * The LOC to MOQT stream mapping mode of a loc track (draft-ietf-moq-msf-02
 * §4.1.1): `"group"` sends all Objects of a Group in one Subgroup (the
 * default), `"object"` sends each Object in its own Subgroup.
 */
export type StreamMapping = "group" | "object" | OpenString;

const STREAM_MAPPING_KEY = "streamMapping";

/** Matches a Unicode white space character. */
const WHITESPACE = /\p{White_Space}/u;

/**
 * Returns the value of key in a packagingMetadata string, a
 * semicolon-separated list of key=value pairs. When key repeats, the first
 * value is used. Keys are case-sensitive.
 */
export function packagingMetadataValue(
	track: { packagingMetadata?: string },
	key: string,
): string | undefined {
	const metadata = track.packagingMetadata;
	if (!metadata) {
		return undefined;
	}
	for (const pair of metadata.split(";")) {
		const eq = pair.indexOf("=");
		if (eq >= 0 && pair.slice(0, eq) === key) {
			return pair.slice(eq + 1);
		}
	}
	return undefined;
}

/**
 * Returns a track's LOC stream mapping mode from the streamMapping key of
 * its packagingMetadata, or `"group"` when the key is absent.
 */
export function streamMapping(track: { packagingMetadata?: string }): StreamMapping {
	const mode = packagingMetadataValue(track, STREAM_MAPPING_KEY);
	return mode ?? "group";
}

/**
 * Checks the list syntax of draft-ietf-moq-msf-02 §5.2.5, whose keys and
 * values "MUST NOT contain whitespace, = or ;": an empty string is a list of
 * no pairs; every item between semicolons must be a pair with a non-empty
 * key, so an empty item (a stray `;`) is rejected; an empty value is
 * allowed. For loc packaging a streamMapping value must be a defined mode.
 * Returns the problem, or undefined.
 */
export function packagingMetadataProblem(
	metadata: string,
	packaging: string | undefined,
): string | undefined {
	if (metadata === "") {
		return undefined;
	}
	for (const pair of metadata.split(";")) {
		const eq = pair.indexOf("=");
		if (eq <= 0) {
			return `packagingMetadata entry ${JSON.stringify(pair)} must be a key=value pair`;
		}
		if (pair.indexOf("=", eq + 1) >= 0 || WHITESPACE.test(pair)) {
			return `packagingMetadata entry ${
				JSON.stringify(pair)
			} must not contain whitespace or a second '='`;
		}
	}
	if (packaging !== "loc") {
		return undefined;
	}
	const mode = packagingMetadataValue({ packagingMetadata: metadata }, STREAM_MAPPING_KEY);
	if (mode !== undefined && mode !== "group" && mode !== "object") {
		return `packagingMetadata streamMapping ${
			JSON.stringify(mode)
		} must be "group" or "object"`;
	}
	return undefined;
}
