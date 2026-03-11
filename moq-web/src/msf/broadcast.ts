import { Broadcast as TrackBroadcast } from "../broadcast.ts";
import { GroupErrorCode, SubscribeErrorCode } from "../error.ts";
import type { TrackHandler } from "../track_mux.ts";
import type { TrackWriter } from "../track_writer.ts";
import {
	type Catalog,
	cloneCatalog,
	cloneTrack,
	stringifyCatalog,
	type Track,
	trackId,
	validateCatalog,
} from "./catalog.ts";

export const DefaultCatalogTrackName = "catalog";

export class Broadcast implements TrackHandler {
	#catalogTrackName: string;
	#catalog: Catalog;
	#tracks = new TrackBroadcast();

	constructor(catalog: Catalog, catalogTrackName = DefaultCatalogTrackName) {
		this.#catalogTrackName = catalogTrackName;
		this.#catalog = prepareCatalog(catalog, this.#catalogTrackName);
	}

	get catalogTrackName(): string {
		return this.#catalogTrackName;
	}

	catalog(): Catalog {
		return cloneCatalog(this.#catalog);
	}

	catalogBytes(): Uint8Array {
		return new TextEncoder().encode(stringifyCatalog(this.#catalog));
	}

	async setCatalog(catalog: Catalog): Promise<void> {
		const clone = prepareCatalog(catalog, this.#catalogTrackName);
		const staleTrackNames = staleTrackNamesFrom(this.#catalog, clone);
		this.#catalog = clone;
		await Promise.allSettled(staleTrackNames.map((name) => this.#tracks.remove(name)));
	}

	async registerTrack(track: Track, handler: TrackHandler): Promise<void> {
		if (handler === undefined || handler === null) {
			throw new Error("msf: track handler cannot be nil");
		}
		if (!track.name) {
			throw new Error("msf: track name is required");
		}
		if (track.name === this.#catalogTrackName) {
			throw new Error(`msf: \"${track.name}\" is reserved for the catalog track`);
		}
		const trackName = track.name;

		const trackClone = cloneTrack(track);
		const updated = cloneCatalog(this.#catalog);
		const nextId = trackId(trackClone, updated.defaultNamespace);

		let replaced = false;
		for (let i = 0; i < updated.tracks.length; i++) {
			const current = updated.tracks[i]!;
			if (current.name !== trackClone.name) {
				continue;
			}
			if (trackId(current, updated.defaultNamespace) !== nextId) {
				throw new Error(
					`msf: broadcast requires unique track names across namespaces; duplicate name ${
						JSON.stringify(trackClone.name)
					} found`,
				);
			}
			updated.tracks[i] = trackClone;
			replaced = true;
			break;
		}
		if (!replaced) {
			updated.tracks.push(trackClone);
		}
		validateCatalog(updated);
		validateCatalogForBroadcast(updated, this.#catalogTrackName);

		this.#catalog = updated;
		await this.#tracks.register(trackName, handler);
	}

	async removeTrack(name: string): Promise<boolean> {
		if (name === "" || name === this.#catalogTrackName) {
			return false;
		}

		const updated = cloneCatalog(this.#catalog);
		const beforeLength = updated.tracks.length;
		updated.tracks = updated.tracks.filter((track) => track.name !== name);
		this.#catalog = updated;
		const removedFromTracks = await this.#tracks.remove(name);
		return updated.tracks.length !== beforeLength || removedFromTracks;
	}

	handler(name: string): TrackHandler {
		if (name === this.#catalogTrackName) {
			return { serveTrack: this.#serveCatalogTrack.bind(this) };
		}
		return this.#tracks.handler(name);
	}

	async serveTrack(trackWriter: TrackWriter): Promise<void> {
		await this.handler(trackWriter.trackName).serveTrack(trackWriter);
	}

	async close(): Promise<void> {
		await this.#tracks.close();
	}

	async #serveCatalogTrack(trackWriter: TrackWriter): Promise<void> {
		const payload = this.catalogBytes();
		const [group, openErr] = await trackWriter.openGroup();
		if (openErr || !group) {
			await trackWriter.closeWithError(SubscribeErrorCode.InternalError);
			return;
		}

		const writeErr = await group.writeFrame(payload);
		if (writeErr) {
			await group.cancel(GroupErrorCode.InternalError);
			await trackWriter.closeWithError(SubscribeErrorCode.InternalError);
			return;
		}

		await group.close();
		await trackWriter.close();
	}
}

function staleTrackNamesFrom(previous: Catalog, next: Catalog): string[] {
	if (previous.tracks.length === 0) {
		return [];
	}

	const activeNames = new Set(
		next.tracks.map((track) => track.name).filter((name): name is string => !!name),
	);
	return previous.tracks
		.map((track) => track.name)
		.filter((name): name is string => !!name && !activeNames.has(name));
}

function prepareCatalog(catalog: Catalog, catalogTrackName: string): Catalog {
	const clone = cloneCatalog(catalog);
	validateCatalog(clone);
	validateCatalogForBroadcast(clone, catalogTrackName);
	return clone;
}

function validateCatalogForBroadcast(catalog: Catalog, catalogTrackName: string): void {
	const seen = new Map<string, string>();
	for (const track of catalog.tracks) {
		if (!track.name) {
			continue;
		}
		if (track.name === catalogTrackName) {
			throw new Error(`msf: catalog contains reserved track name \"${catalogTrackName}\"`);
		}
		const id = trackId(track, catalog.defaultNamespace);
		const previous = seen.get(track.name);
		if (previous !== undefined && previous !== id) {
			throw new Error(
				`msf: broadcast requires unique track names across namespaces; duplicate name ${
					JSON.stringify(track.name)
				} found`,
			);
		}
		seen.set(track.name, id);
	}
}
