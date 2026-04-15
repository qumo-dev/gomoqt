/** Stream type identifiers for bidirectional streams as defined in MOQ Lite. */
export const BiStreamTypes = {
	/** Announce stream for broadcasting track availability. */
	AnnounceStreamType: 0x01,
	/** Subscribe stream for requesting track data. */
	SubscribeStreamType: 0x02,
	/** Fetch stream for retrieving a single group. */
	FetchStreamType: 0x03,
	/** Probe stream for bandwidth estimation. */
	ProbeStreamType: 0x04,
	/** Goaway stream for session migration. */
	GoawayStreamType: 0x05,
} as const;

/** Stream type identifiers for unidirectional streams as defined in MOQ Lite. */
export const UniStreamTypes = {
	/** Group stream carrying frame data from publisher to subscriber. */
	GroupStreamType: 0x00,
} as const;

/** Union of all bidirectional stream type values. */
export type BiStreamType = typeof BiStreamTypes[keyof typeof BiStreamTypes];
/** Union of all unidirectional stream type values. */
export type UniStreamType = typeof UniStreamTypes[keyof typeof UniStreamTypes];
