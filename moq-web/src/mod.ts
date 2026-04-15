/**
 * TypeScript client library for Media over QUIC Transport (MOQ Lite).
 *
 * Provides {@link connect} to open MOQ sessions over WebTransport,
 * a pub/sub model for tracks ({@link TrackWriter} / {@link TrackReader}),
 * announcement discovery ({@link AnnouncementReader} / {@link AnnouncementWriter}),
 * frame-level I/O ({@link GroupWriter} / {@link GroupReader}),
 * and a {@link TrackMux} for per-track routing.
 *
 * @example
 * ```ts
 * import { connect } from "@okdaichi/moq";
 *
 * const session = await connect("https://localhost:4443/moq");
 * const [reader] = await session.subscribe("/broadcast", "video");
 * ```
 *
 * @module
 */

export * from "./alias.ts";
export * from "./session.ts";
export * from "./broadcast_path.ts";
export * from "./track_prefix.ts";
export * from "./options.ts";
export * from "./info.ts";
export * from "./client.ts";
export * from "./announce_stream.ts";
export * from "./group_stream.ts";
export * from "./stream_type.ts";
