package moqt

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// ErrInvalidPathPattern is returned for a path pattern that does not follow
// the syntax documented on Grant: one that is not a broadcast path or a prefix
// of them, or whose wildcard is malformed, misplaced or named twice. Received
// on an Auth Stream, it is a protocol violation.
var ErrInvalidPathPattern = errors.New("moqt: invalid path pattern")

// PathPattern is a parsed path pattern, a set of broadcast paths; Grant
// documents the syntax. An application that grants by the same rules as a
// session enforces, such as for requests outside MoQ, matches with it.
type PathPattern struct {
	raw string
	// segs holds the segments of a pattern with a wildcard, and is nil for
	// one without.
	segs []patternSeg
	// trailing is whether a pattern with a wildcard ends in "/".
	trailing bool
}

// patternSeg is a segment of a pattern with a wildcard.
type patternSeg struct {
	kind segKind
	// lit is the text of a literal segment, and empty for a wildcard.
	lit string
	// name is the name of a "{name}" or "{name...}" wildcard.
	name string
}

// ParsePathPattern parses s, an absolute pattern: a path starting with "/"
// whose wildcards are whole segments. An empty segment, as in "/room//x", is
// a literal one. ParseGrantPattern reads one relative to a root, as a Grant's
// are.
func ParsePathPattern(s string) (PathPattern, error) {
	if !isValidPath(BroadcastPath(s)) {
		return PathPattern{}, fmt.Errorf("%w: %q", ErrInvalidPathPattern, s)
	}
	if !strings.ContainsAny(s, "{}") {
		return PathPattern{raw: s}, nil
	}

	p := PathPattern{raw: s, trailing: strings.HasSuffix(s, "/")}
	var names []string
	for segs := strings.TrimSuffix(s[1:], "/"); ; {
		seg, next, more := strings.Cut(segs, "/")
		name, kind := parseWildcard(seg)
		last := !more && !p.trailing
		switch {
		case kind == segInvalid,
			kind == segAnchor && !last,
			kind == segRest && !last,
			name != "" && slices.Contains(names, name):
			return PathPattern{}, fmt.Errorf("%w: %q", ErrInvalidPathPattern, s)
		}
		if name != "" {
			names = append(names, name)
		}
		ps := patternSeg{kind: kind, name: name}
		if kind == segLiteral {
			ps.lit = seg
		}
		p.segs = append(p.segs, ps)
		if !more {
			return p, nil
		}
		segs = next
	}
}

// segKind is the kind of a pattern segment.
type segKind int

const (
	segInvalid  segKind = iota
	segLiteral          // a literal segment
	segWildcard         // "{name}"
	segRest             // "{name...}"
	segAnchor           // "{$}"
)

// parseWildcard reads a segment: its kind, and the wildcard's name.
func parseWildcard(seg string) (name string, kind segKind) {
	inner, ok := strings.CutPrefix(seg, "{")
	if !ok {
		if strings.ContainsAny(seg, "{}") {
			return "", segInvalid
		}
		return "", segLiteral
	}
	if inner, ok = strings.CutSuffix(inner, "}"); !ok {
		return "", segInvalid
	}
	if inner == "$" {
		return "", segAnchor
	}
	kind = segWildcard
	if name, ok = strings.CutSuffix(inner, "..."); ok {
		kind = segRest
	} else {
		name = inner
	}
	if !isWildcardName(name) {
		return "", segInvalid
	}
	return name, kind
}

// isWildcardName reports whether s is a wildcard name as net/http reads it:
// letters, digits and "_", not starting with a digit.
func isWildcardName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !unicode.IsLetter(c) && c != '_' && (i == 0 || !unicode.IsDigit(c)) {
			return false
		}
	}
	return true
}

// Match reports whether path is in the pattern's set, comparing segments as
// written, as a session does: "/room/1/../2" is a path beneath "/room/1/",
// not "/room/2". A caller matching a path from elsewhere, such as a URL,
// passes it as the broadcast path it names, with no "." or ".." segment for
// anything downstream to resolve. The zero PathPattern matches nothing.
func (p PathPattern) Match(path BroadcastPath) bool {
	if p.segs != nil {
		return p.matchSegments(path)
	}
	if isValidPrefix(p.raw) {
		return path.HasPrefix(p.raw)
	}
	return p.raw != "" && string(path) == p.raw
}

// matchSegments matches path against a pattern with a wildcard.
func (p PathPattern) matchSegments(path BroadcastPath) bool {
	if !isValidPath(path) {
		return false
	}
	rest := string(path[1:])
	for i, seg := range p.segs {
		switch seg.kind {
		case segAnchor:
			return rest == ""
		case segRest:
			return true
		}
		got, after, more := strings.Cut(rest, "/")
		if seg.kind == segWildcard {
			if got == "" {
				return false
			}
		} else if got != seg.lit {
			return false
		}
		if i == len(p.segs)-1 {
			// The pattern's segments are used up: an exact pattern needs the
			// path to end here, and a prefix needs it to go on.
			return more == p.trailing
		}
		if !more {
			return false
		}
		rest = after
	}
	return false
}

// values returns what each named wildcard of p matched in path, which p
// matches: a "{name}" its segment, a "{name...}" the rest of the path, with
// no leading "/". It returns nil for a pattern without a named wildcard.
func (p PathPattern) values(path BroadcastPath) map[string]string {
	var vals map[string]string
	rest := string(path[1:])
	for _, seg := range p.segs {
		if seg.kind == segAnchor {
			break
		}
		got, after, _ := strings.Cut(rest, "/")
		if seg.kind == segRest {
			got = rest
		}
		if seg.name != "" {
			if vals == nil {
				vals = make(map[string]string, len(p.segs))
			}
			vals[seg.name] = got
		}
		if seg.kind == segRest {
			break
		}
		rest = after
	}
	return vals
}

// coversBeneath reports whether the pattern matches every path beneath route,
// the paths an announcement of route claims (moq-lite-06 §5.1.1): those that
// continue it past a "/", "/live/" and "/live/alice" for route "/live".
func (p PathPattern) coversBeneath(route BroadcastPath) bool {
	base := string(route)
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	if p.segs == nil {
		// A literal pattern covers a subtree only as a prefix leading it.
		return isValidPrefix(p.raw) && strings.HasPrefix(base, p.raw)
	}
	// Walk base's segments; whatever follows them is arbitrary, so the
	// pattern must accept anything from there: a rest wildcard, or a prefix
	// ending right there.
	rest := strings.TrimSuffix(base[1:], "/")
	for i, seg := range p.segs {
		if seg.kind == segRest {
			return true
		}
		if rest == "" && base == "/" {
			return false
		}
		got, after, more := strings.Cut(rest, "/")
		switch seg.kind {
		case segAnchor:
			return false
		case segWildcard:
			if got == "" {
				return false
			}
		default:
			if got != seg.lit {
				return false
			}
		}
		if !more {
			// base is used up: the pattern must go on with a rest wildcard,
			// or end here as a prefix.
			if i == len(p.segs)-1 {
				return p.trailing
			}
			return p.segs[i+1].kind == segRest
		}
		rest = after
	}
	return false
}

// String returns the pattern as it was parsed.
func (p PathPattern) String() string {
	return p.raw
}

// ParseGrantPattern parses pattern relative to root, as a Grant's patterns
// are read: with root "/room/1/", "/alice" is "/room/1/alice". root must be a
// prefix, ending in "/", and pattern start with "/".
func ParseGrantPattern(root, pattern string) (PathPattern, error) {
	if !isValidGrantRoot(root) {
		return PathPattern{}, fmt.Errorf("%w: root %q", ErrInvalidPathPattern, root)
	}
	return joinPattern(root, pattern)
}

// isValidGrantRoot reports whether root may be a Grant's root: a prefix with
// no wildcard.
func isValidGrantRoot(root string) bool {
	return isValidPrefix(root) && !strings.ContainsAny(root, "{}")
}

// joinPattern parses pattern relative to root, which the caller has checked.
func joinPattern(root, pattern string) (PathPattern, error) {
	if !strings.HasPrefix(pattern, "/") {
		return PathPattern{}, fmt.Errorf("%w: %q", ErrInvalidPathPattern, pattern)
	}
	return ParsePathPattern(strings.TrimSuffix(root, "/") + pattern)
}

// patternRelation is how the sets of paths two patterns match relate, as
// http.ServeMux relates its patterns.
type patternRelation int

const (
	relEquivalent   patternRelation = iota // the same set
	relMoreSpecific                        // a strict subset
	relMoreGeneral                         // a strict superset
	relOverlaps                            // neither, but they share a path
	relDisjoint                            // they share no path
)

// compare relates the set of paths p matches to the set q matches.
func (p PathPattern) compare(q PathPattern) patternRelation {
	a, b := p.elems(), q.elems()
	rel := relEquivalent
	for i := 0; ; i++ {
		if i == len(a) || i == len(b) {
			if len(a) == len(b) {
				return rel
			}
			return relDisjoint
		}
		ea, eb := a[i], b[i]
		switch {
		case ea.multi && eb.multi:
			return rel
		case ea.multi:
			return combineRelations(rel, relMoreGeneral)
		case eb.multi:
			return combineRelations(rel, relMoreSpecific)
		}
		if rel = combineRelations(rel, ea.compare(eb)); rel == relDisjoint {
			return rel
		}
	}
}

// combineRelations relates two patterns from how two consecutive parts of
// them relate.
func combineRelations(r1, r2 patternRelation) patternRelation {
	switch {
	case r1 == relDisjoint || r2 == relDisjoint:
		return relDisjoint
	case r1 == relEquivalent:
		return r2
	case r2 == relEquivalent || r1 == r2:
		return r1
	default:
		return relOverlaps
	}
}

// patternElem is one segment of a pattern: a literal, a "{name}" wildcard,
// or the rest of a path ("{name...}" or what a trailing "/" leaves).
type patternElem struct {
	literal  string
	wildcard bool
	multi    bool
}

// compare relates the segments e and f match.
func (e patternElem) compare(f patternElem) patternRelation {
	switch {
	case e.wildcard && f.wildcard:
		return relEquivalent
	case e.wildcard:
		// "{name}" matches no empty segment.
		if f.literal == "" {
			return relDisjoint
		}
		return relMoreGeneral
	case f.wildcard:
		if e.literal == "" {
			return relDisjoint
		}
		return relMoreSpecific
	case e.literal == f.literal:
		return relEquivalent
	default:
		return relDisjoint
	}
}

// elems returns the segments of p. "{$}" is the empty literal segment it
// matches.
func (p PathPattern) elems() []patternElem {
	if p.raw == "" {
		return nil
	}
	var elems []patternElem
	if p.raw != "/" {
		for _, seg := range strings.Split(strings.TrimSuffix(p.raw[1:], "/"), "/") {
			switch _, kind := parseWildcard(seg); kind {
			case segAnchor:
				elems = append(elems, patternElem{})
			case segRest:
				return append(elems, patternElem{multi: true})
			case segWildcard:
				elems = append(elems, patternElem{wildcard: true})
			default:
				elems = append(elems, patternElem{literal: seg})
			}
		}
	}
	if strings.HasSuffix(p.raw, "/") {
		elems = append(elems, patternElem{multi: true})
	}
	return elems
}
