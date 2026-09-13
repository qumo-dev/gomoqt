// Package msf implements the MOQT Streaming Format (MSF) draft-ietf-moq-msf-01.
//
// The package provides a self-contained representation of MSF catalog objects
// and timeline records, along with validation and delta-application logic.
// Independent catalog snapshots are modeled by Catalog, while incremental
// updates are modeled by CatalogDelta. Delta operations that have a narrower
// JSON shape than a full track entry are represented by TrackRef and
// TrackClone.
//
// Most of the package is transport-agnostic and can be used in pure
// data-processing tools or tests. The optional Broadcast helper integrates an
// MSF catalog snapshot with moqt.TrackHandler routing for publishers that want
// a small in-memory track registry.
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
