package msf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Variable substitution (draft-ietf-moq-msf-02 §5.4).
//
// Catalog field values MAY contain variables denoted by enclosing the variable
// name in percent characters (e.g. "%token%"). Variables are resolved at
// delivery time from the fragment identifier of the URI used to access the
// catalog, enabling per-viewer customization without affecting server-side
// caching.
//
// This file provides a fragment parser, a string resolver, a catalog resolver
// and a syntax validator. Variable resolution is a client-side delivery
// concern, so none of them is invoked automatically by Catalog.Validate.

// connectionURIKey is the track field that names a network endpoint, which
// Catalog.ResolveVariables leaves unsubstituted (§13.5).
const connectionURIKey = "connectionURI"

// isValidVariableName reports whether s can name a variable. Draft-02 places
// no restriction on the characters of a name; it only has to be non-empty,
// and it cannot contain '%', which delimits the reference.
func isValidVariableName(s string) bool {
	return s != ""
}

// isValidVariableValue reports whether s is a legal substituted value per the
// spec: alphanumeric characters, hyphens, underscores, plus signs, forward
// slashes, equals signs, periods and the at sign (@). This restriction
// prevents injection into catalog field values.
func isValidVariableValue(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '+', r == '/', r == '=', r == '.', r == '@':
		default:
			return false
		}
	}
	return true
}

// ParseFragmentVariables parses the variables of a catalog URI fragment
// (draft-ietf-moq-msf-02 §5.4.2). The fragment, with or without its leading
// '#', is split at each '&' into pairs, and each pair is split at its first
// '=' into a variable name and value, so values may themselves contain '='.
// Pairs without '=', such as the "msf:namespace--name" track locator, carry no
// variable and are skipped, as are pairs with an empty name. When a name
// repeats, the first value is kept. Values are not percent-decoded: '%' is not
// a legal variable value.
func ParseFragmentVariables(fragment string) map[string]string {
	fragment = strings.TrimPrefix(fragment, "#")
	vars := make(map[string]string)
	for pair := range strings.SplitSeq(fragment, "&") {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || name == "" {
			continue
		}
		if _, seen := vars[name]; !seen {
			vars[name] = value
		}
	}
	return vars
}

// ResolveVariables substitutes %name% tokens in s using the provided variables.
// It returns the resolved string and ok=true on success. It returns ok=false
// (and the partially processed input up to the failure) when:
//   - a percent character appears outside a %name% reference,
//   - a referenced variable is missing from vars, or
//   - a substituted value contains characters disallowed by the spec.
//
// vars is typically the result of ParseFragmentVariables.
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

// ResolveVariables substitutes variables in every string value of the catalog,
// including nested objects such as authInfo and unknown extension fields,
// then validates the result, since a catalog MUST be validated after
// substitution (draft-ietf-moq-msf-02 §5.4.1, §13.5). Object keys and
// non-string values are left as they are. The name mapping rules of §11.2
// apply to the substituted names, which Validate checks.
//
// Following the §13.5 advice that substitution SHOULD NOT reach fields that
// determine a network endpoint, connectionURI values are not substituted; a
// connectionURI that still holds a variable reference is reported as an error.
// A caller that trusts the URI source can resolve such a value itself with
// the string form of ResolveVariables.
//
// On error the returned catalog is the zero value.
func (c Catalog) ResolveVariables(vars map[string]string) (Catalog, error) {
	encoded, err := c.MarshalJSON()
	if err != nil {
		return Catalog{}, fmt.Errorf("msf: encode catalog for variable resolution: %w", err)
	}
	// UseNumber keeps integers beyond 2^53 exact through the round trip.
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return Catalog{}, fmt.Errorf("msf: decode catalog for variable resolution: %w", err)
	}
	resolved, err := resolveJSONValue(tree, vars, "")
	if err != nil {
		return Catalog{}, err
	}
	encoded, err = json.Marshal(resolved)
	if err != nil {
		return Catalog{}, fmt.Errorf("msf: encode resolved catalog: %w", err)
	}
	var out Catalog
	if err := json.Unmarshal(encoded, &out); err != nil {
		return Catalog{}, fmt.Errorf("msf: decode resolved catalog: %w", err)
	}
	out.DefaultNamespace = c.DefaultNamespace
	if err := out.Validate(); err != nil {
		return Catalog{}, err
	}
	return out, nil
}

// errUnresolvedEndpoint reports a connectionURI that holds a variable.
var errUnresolvedEndpoint = errors.New("msf: variables in connectionURI are not substituted")

// resolveJSONValue substitutes variables in every string of a decoded JSON
// value. key is the object member name the value belongs to, if any.
func resolveJSONValue(v any, vars map[string]string, key string) (any, error) {
	switch value := v.(type) {
	case string:
		if !strings.Contains(value, "%") {
			return value, nil
		}
		if key == connectionURIKey {
			return nil, fmt.Errorf("%w: %q", errUnresolvedEndpoint, value)
		}
		out, ok := ResolveVariables(value, vars)
		if !ok {
			return nil, fmt.Errorf("msf: failed to resolve variables in %q", value)
		}
		return out, nil
	case map[string]any:
		for k, member := range value {
			resolved, err := resolveJSONValue(member, vars, k)
			if err != nil {
				return nil, err
			}
			value[k] = resolved
		}
		return value, nil
	case []any:
		for i, element := range value {
			resolved, err := resolveJSONValue(element, vars, "")
			if err != nil {
				return nil, err
			}
			value[i] = resolved
		}
		return value, nil
	default:
		return value, nil
	}
}

// ValidateVariableSyntax checks that every percent character in a string occurs
// as part of a well-formed, non-empty %name% reference (draft-ietf-moq-msf-02
// §5.4.1). It does not check that the referenced variable exists; use
// ResolveVariables for that. This is opt-in and is not called by
// Catalog.Validate.
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
			return fmt.Errorf("msf: empty variable name in %q", s)
		}
		i = i + 1 + end + 1
	}
	return nil
}
