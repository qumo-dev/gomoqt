package msf

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxNamespaceFields is the largest number of Track Namespace Fields a catalog
// namespace string may yield (draft-ietf-moq-msf-02 §5.2.2, [MOQT] §2.4.1).
const maxNamespaceFields = 32

// namespaceDelimiter separates Track Namespace Fields in a catalog namespace
// string (draft-ietf-moq-msf-02 §5.2.2).
const namespaceDelimiter = "/"

// utf8BOM is the UTF-8 encoding of U+FEFF, which a catalog document MUST NOT
// begin with (draft-ietf-moq-msf-02 §11.2.1).
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// SplitNamespace splits a catalog namespace string into the Track Namespace
// Fields it denotes (draft-ietf-moq-msf-02 §5.2.2). Every U+002F SOLIDUS is a
// field boundary. The string must yield between 1 and 32 fields, every field
// must be non-empty, and every field must satisfy the naming rules of §11.2
// (valid UTF-8, no NUL). The returned fields are the exact bytes of the MOQT
// Track Namespace Fields: no normalization or case folding is applied.
func SplitNamespace(namespace string) ([]string, error) {
	if err := validateNamespace(namespace); err != nil {
		return nil, err
	}
	return strings.Split(namespace, namespaceDelimiter), nil
}

// validateNamespace checks a catalog namespace string without allocating.
func validateNamespace(namespace string) error {
	if namespace == "" {
		return errors.New("namespace must not be empty")
	}
	fields := 1
	start := 0
	for i := 0; i < len(namespace); i++ {
		if namespace[i] != '/' {
			continue
		}
		if i == start {
			return fmt.Errorf("namespace %q contains an empty field", namespace)
		}
		fields++
		start = i + 1
	}
	if start == len(namespace) {
		return fmt.Errorf("namespace %q contains an empty field", namespace)
	}
	if fields > maxNamespaceFields {
		return fmt.Errorf("namespace %q has %d fields; at most %d are allowed", namespace, fields, maxNamespaceFields)
	}
	return validateNameText(namespace)
}

// validateNameText applies the string-to-bytes rules of draft-ietf-moq-msf-02
// §11.2.1 to a naming field (namespace, name, depends, parentName,
// parentNamespace): the value must be valid UTF-8, which also excludes
// surrogate code points, and must not contain U+0000.
func validateNameText(s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%q is not valid UTF-8", s)
	}
	if strings.IndexByte(s, 0) >= 0 {
		return fmt.Errorf("%q contains a NUL character", s)
	}
	return nil
}

// checkCatalogText rejects catalog documents that the JSON decoder would
// otherwise accept by silently substituting U+FFFD (draft-ietf-moq-msf-02
// §11.2.1): a leading byte order mark, bytes that are not UTF-8, and a
// \uXXXX escape that denotes an unpaired surrogate. Substituting U+FFFD would
// make distinct names compare equal, so the document is rejected instead.
//
// The surrogate check covers the whole document, not only the naming fields:
// a Go string cannot carry an unpaired surrogate, so the decoder cannot
// preserve such a value in any field.
func checkCatalogText(data []byte) error {
	if bytes.HasPrefix(data, utf8BOM) {
		return errors.New("msf: catalog must not begin with a byte order mark")
	}
	if !utf8.Valid(data) {
		return errors.New("msf: catalog is not valid UTF-8")
	}
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		if i+1 >= len(data) {
			return nil // malformed JSON; the decoder reports it
		}
		if data[i+1] != 'u' {
			i++ // skip the escaped character, which may itself be a backslash
			continue
		}
		r, ok := parseHex4(data, i+2)
		if !ok {
			return nil // malformed JSON; the decoder reports it
		}
		switch {
		case r >= 0xD800 && r <= 0xDBFF:
			low, ok := uint32(0), false
			if i+11 < len(data) && data[i+6] == '\\' && data[i+7] == 'u' {
				low, ok = parseHex4(data, i+8)
			}
			if !ok || low < 0xDC00 || low > 0xDFFF {
				return fmt.Errorf("msf: catalog contains an unpaired surrogate \\u%04X", r)
			}
			i += 11
		case r >= 0xDC00 && r <= 0xDFFF:
			return fmt.Errorf("msf: catalog contains an unpaired surrogate \\u%04X", r)
		default:
			i += 5
		}
	}
	return nil
}

// parseHex4 parses the four hexadecimal digits at data[at:at+4].
func parseHex4(data []byte, at int) (uint32, bool) {
	if at+4 > len(data) {
		return 0, false
	}
	var r uint32
	for _, c := range data[at : at+4] {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= uint32(c - '0')
		case c >= 'a' && c <= 'f':
			r |= uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			r |= uint32(c-'A') + 10
		default:
			return 0, false
		}
	}
	return r, true
}
