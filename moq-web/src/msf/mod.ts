/**
 * MOQT Streaming Format (MSF, draft-ietf-moq-msf-02) catalog, delta,
 * timeline, and broadcast helpers.
 *
 * Provides parsing and validation for MSF catalogs
 * ({@link parseCatalog}, {@link validateCatalog}),
 * delta updates ({@link parseCatalogDelta}, {@link applyCatalogDelta}),
 * media/event timeline decoding, and a catalog-aware {@link Broadcast}.
 *
 * @example
 * ```ts
 * import { Broadcast, parseCatalog, validateCatalog } from "@qumo/moq/msf";
 *
 * const catalog = parseCatalog(catalogJson);
 * validateCatalog(catalog);
 * const broadcast = new Broadcast(catalog);
 * ```
 *
 * @module
 */

export {
	type AccessibilityDescriptor,
	type Buffers,
	type Catalog,
	cloneCatalog,
	cloneTrack,
	CurrentVersion,
	type Dependency,
	effectiveNamespace,
	initDataPropertyType,
	type InitDataRef,
	type InitDataType,
	type Packaging,
	parseCatalog,
	parseTrack,
	type Role,
	stringifyCatalog,
	type Track,
	trackId,
	validateCatalog,
	ValidationError,
} from "./catalog.ts";
export {
	applyCatalogDelta,
	CatalogCompleteError,
	type CatalogDelta,
	type DeltaOp,
	type DeltaOperationKind,
	parseCatalogDelta,
	stringifyCatalogDelta,
	type TrackClone,
	type TrackRef,
	type TrackUpdate,
	validateCatalogDelta,
} from "./catalog_delta.ts";
export { splitNamespace } from "./names.ts";
export { packagingMetadataValue, type StreamMapping, streamMapping } from "./track_fields.ts";
export {
	decodeLocation,
	decodeMediaTimelineEntry,
	encodeEventTimelineRecord,
	encodeLocation,
	encodeMediaTimelineEntry,
	type EventTimelineRecord,
	type Location,
	type MediaTimelineEntry,
	parseEventTimelineRecord,
	type Template,
	templateEntry,
	validateEventTimelineRecord,
	validateMediaTimeline,
} from "./timeline.ts";
export { Broadcast, DefaultCatalogTrackName } from "./broadcast.ts";
