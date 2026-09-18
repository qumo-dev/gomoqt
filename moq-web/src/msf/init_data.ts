/**
 * Internal bridge between the draft-ietf-moq-msf-01 Initialization Data List
 * (a catalog-level `initDataList` of entries that tracks reference by
 * `initRef`) and the resolved {@link Track.initData} convenience field.
 *
 * Deliberately not re-exported from `mod.ts`: callers see the effect through
 * {@link parseCatalog}, {@link applyCatalogDelta} and {@link stringifyCatalog},
 * not through a separate API.
 *
 * @module
 */
import type { Catalog, InitDataRef, Track } from "./catalog.ts";

/** The only {@link InitDataRef.type} defined by draft-ietf-moq-msf-01. */
export const INIT_DATA_TYPE_INLINE = "inline";

/** Returns the entry's inline payload, or undefined for a non-inline entry. */
function inlineData(ref: InitDataRef): string | undefined {
	if (ref.type !== undefined && ref.type !== INIT_DATA_TYPE_INLINE) {
		return undefined;
	}
	return ref.data;
}

/**
 * Fill `initData` on every track whose `initRef` resolves to an inline entry
 * of `catalog.initDataList`, so consumers can read the payload directly. A
 * track that already carries `initData` keeps it. Mutates `catalog.tracks`.
 */
export function resolveInitData(catalog: Catalog): void {
	const list = catalog.initDataList;
	if (!list || list.length === 0) {
		return;
	}
	const byId = new Map<string, InitDataRef>();
	for (const ref of list) {
		if (!byId.has(ref.id)) {
			byId.set(ref.id, ref);
		}
	}
	for (const track of catalog.tracks) {
		if (track.initData !== undefined || !track.initRef) {
			continue;
		}
		const ref = byId.get(track.initRef);
		const data = ref === undefined ? undefined : inlineData(ref);
		if (data !== undefined) {
			track.initData = data;
		}
	}
}

/** The wire-form init data of a catalog: its list and each track's reference. */
export interface MaterializedInitData {
	/** The `initDataList` to serialize, or undefined when there is none. */
	initDataList?: InitDataRef[];
	/** The `initRef` to serialize for `catalog.tracks[i]`, if any. */
	initRefs: (string | undefined)[];
}

/**
 * Compute the msf-01 wire form of a catalog's init data without mutating it.
 *
 * msf-01 has no per-track `initData`, so every track payload must travel as an
 * `initDataList` entry. The existing list is kept in order. A track whose
 * `initData` matches its referenced entry keeps that reference; otherwise its
 * payload is matched to an existing inline entry with identical data or, if
 * there is none, added as a new entry, so tracks sharing a payload share one
 * entry and a payload edited in place is not lost to a stale reference.
 */
export function materializeInitData(catalog: Catalog): MaterializedInitData {
	const list: InitDataRef[] = (catalog.initDataList ?? []).map((ref) => ({ ...ref }));
	const ids = new Set(list.map((ref) => ref.id));
	const byId = new Map<string, InitDataRef>();
	const byData = new Map<string, string>();
	for (const ref of list) {
		if (!byId.has(ref.id)) {
			byId.set(ref.id, ref);
		}
		const data = inlineData(ref);
		if (data !== undefined && !byData.has(data)) {
			byData.set(data, ref.id);
		}
	}

	const freshId = (base: string): string => {
		if (!ids.has(base)) {
			return base;
		}
		for (let n = 2;; n++) {
			const candidate = `${base}-${n}`;
			if (!ids.has(candidate)) {
				return candidate;
			}
		}
	};

	const initRefs = catalog.tracks.map((track: Track): string | undefined => {
		if (track.initData === undefined) {
			return track.initRef || undefined;
		}
		if (track.initRef) {
			const referenced = byId.get(track.initRef);
			if (referenced !== undefined && inlineData(referenced) === track.initData) {
				return track.initRef;
			}
		}
		const shared = byData.get(track.initData);
		if (shared !== undefined) {
			return shared;
		}
		// Keep the caller's chosen id when it is still free; otherwise derive one.
		// An empty initRef is unset, as in the Go package.
		const initRef = track.initRef || undefined;
		const id = initRef !== undefined && !byId.has(initRef)
			? initRef
			: freshId(initRef ?? (track.name || "init"));
		const ref: InitDataRef = { id, type: INIT_DATA_TYPE_INLINE, data: track.initData };
		list.push(ref);
		ids.add(id);
		byId.set(id, ref);
		byData.set(track.initData, id);
		return id;
	});

	return { initDataList: list.length > 0 ? list : undefined, initRefs };
}
