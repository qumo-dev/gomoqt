package msf

import (
	"fmt"
	"strings"
)

// Variable substitution (draft-ietf-moq-msf-01 §5.4).
//
// Catalog field values MAY contain variables denoted by enclosing the variable
// name in percent characters (e.g. "%token%"). Variables are resolved at
// delivery time from the fragment identifier of the URI used to access the
// catalog, enabling per-viewer customization without affecting server-side
// caching.
//
// This file provides a resolver and a syntax validator. Variable resolution is
// a client-side delivery concern, so neither is invoked automatically by
// Catalog.Validate; callers apply ResolveVariables when serving a catalog.

// variableNameMaxLen bounds the length of a single variable name accepted by
// the resolver. It guards the scanner against pathologically long runs.
const variableNameMaxLen = 1024

// isValidVariableName reports whether s is a legal variable name per the spec:
// alphanumeric characters, hyphens, and underscores, case-sensitive.
func isValidVariableName(s string) bool {
	if s == "" || len(s) > variableNameMaxLen {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// isValidVariableValue reports whether s is a legal substituted value per the
// spec: alphanumeric characters, hyphens, underscores, and the at sign (@).
// This restriction prevents injection into catalog field values.
func isValidVariableValue(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '@':
		default:
			return false
		}
	}
	return true
}

// ResolveVariables substitutes %name% tokens in s using the provided variables.
// It returns the resolved string and ok=true on success. It returns ok=false
// (and the partially processed input up to the failure) when:
//   - a percent character appears outside a valid %name% reference,
//   - a referenced variable is missing from vars, or
//   - a substituted value contains characters disallowed by the spec.
//
// vars should typically be parsed from the URI fragment (keys and values
// separated by '&' and '=').
func ResolveVariables(s string, vars map[string]string) (string, bool) {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] != '%' {
			b.WriteByte(s[i])
			i++
			continue
		}
		// s[i] == '%': must be a %name% reference.
		end := strings.IndexByte(s[i+1:], '%')
		if end < 0 {
			return b.String(), false
		}
		name := s[i+1 : i+1+end]
		if !isValidVariableName(name) {
			return b.String(), false
		}
		value, ok := vars[name]
		if !ok {
			return b.String(), false
		}
		if !isValidVariableValue(value) {
			return b.String(), false
		}
		b.WriteString(value)
		i = i + 1 + end + 1
	}
	return b.String(), true
}

// ResolveVariables applies ResolveVariables to every string-valued field of the
// catalog (including nested objects) and returns a new catalog. It returns an
// error if any field fails to resolve; the returned catalog is best-effort and
// may be partially resolved on error.
func (c Catalog) ResolveVariables(vars map[string]string) (Catalog, error) {
	clone := c.Clone()

	resolveStr := func(s string) (string, error) {
		if !strings.Contains(s, "%") {
			return s, nil
		}
		out, ok := ResolveVariables(s, vars)
		if !ok {
			return s, fmt.Errorf("msf: failed to resolve variables in %q", s)
		}
		return out, nil
	}

	resolveTrack := func(t *Track) error {
		var err error
		if t.Namespace, err = resolveStr(t.Namespace); err != nil {
			return err
		}
		if t.Name, err = resolveStr(t.Name); err != nil {
			return err
		}
		if t.ParentNamespace, err = resolveStr(t.ParentNamespace); err != nil {
			return err
		}
		if t.ConnectionURI, err = resolveStr(t.ConnectionURI); err != nil {
			return err
		}
		if t.Token, err = resolveStr(t.Token); err != nil {
			return err
		}
		if t.TrackBaseKey, err = resolveStr(t.TrackBaseKey); err != nil {
			return err
		}
		return nil
	}

	for i := range clone.Tracks {
		if err := resolveTrack(&clone.Tracks[i]); err != nil {
			return clone, fmt.Errorf("tracks[%d]: %w", i, err)
		}
	}
	for i := range clone.PublishTracks {
		if err := resolveTrack(&clone.PublishTracks[i]); err != nil {
			return clone, fmt.Errorf("publishTracks[%d]: %w", i, err)
		}
	}
	return clone, nil
}

// ValidateVariableSyntax checks that every percent character in a string occurs
// as part of a well-formed %name% reference (draft-ietf-moq-msf-01 §5.4.1). It
// does not check that the referenced variable exists; use ResolveVariables for
// that. This is opt-in and is not called by Catalog.Validate.
func ValidateVariableSyntax(s string) error {
	i := 0
	for i < len(s) {
		if s[i] != '%' {
			i++
			continue
		}
		end := strings.IndexByte(s[i+1:], '%')
		if end < 0 {
			return fmt.Errorf("msf: unterminated variable reference in %q", s)
		}
		name := s[i+1 : i+1+end]
		if !isValidVariableName(name) {
			return fmt.Errorf("msf: invalid variable name %q in %q", name, s)
		}
		i = i + 1 + end + 1
	}
	return nil
}
