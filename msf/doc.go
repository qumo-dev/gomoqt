// Package msf implements the MOQT Streaming Format (MSF) draft-ietf-moq-msf-02.
//
// The package provides a self-contained representation of MSF catalog objects
// and timeline records, along with validation and delta-application logic.
// Independent catalog snapshots are modeled by Catalog, while incremental
// updates are modeled by CatalogDelta. Delta operations that have a narrower
// JSON shape than a full track entry are represented by TrackRef,
// TrackClone and TrackUpdate.
//
// Naming fields map to MOQT bytes as their UTF-8 encoding, compared byte for
// byte with no normalization (draft-ietf-moq-msf-02 §11.2). Terminology
// follows draft-ietf-moq-transport-23: object properties (formerly header
// extensions) and PUBLISH_DONE (formerly SUBSCRIBE_DONE). The MSF properties
// of §12 (MSF_COMPRESSION, VIDEO_CONFIG, AUDIO_CONFIG,
// MSF_EVENT_TIMELINE_HEADER) are transport-layer concerns and are not
// modeled here.
//
// Most of the package is transport-agnostic and can be used in pure
// data-processing tools or tests. The optional Broadcast helper integrates an
// MSF catalog with moqt.TrackHandler routing for publishers that want a small
// in-memory track registry, and sends each catalog change to its subscribers.
//
// Most optional catalog fields use pointer types so that the distinction
// between "field absent" and "field present with zero value" is preserved
// after decoding.  That choice supports both the `omitempty` JSON behavior and
// the delta/clone algorithms which need to know exactly which values were
// supplied by the sender.
//
// Example:
//
//	catalog, err := msf.ParseCatalog(raw)
//	if err != nil {
//	    // handle parse error
//	}
//	if err := catalog.Validate(); err != nil {
//	    // input was syntactically correct but violated MSF rules
//	}
//
// Networking primitives themselves still live in the
// github.com/qumo-dev/gomoqt/moqt package.
package msf
