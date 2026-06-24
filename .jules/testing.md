## 2024-06-22 - Testing Go Deep Copy Logic
**Context:** Improved testing for a Go map containing `json.RawMessage` (which is an alias for `[]byte`).
**Learning:** Testing deep copies of nested slices (like `map[string][]byte`) requires mutating an element inside the slice rather than simply re-assigning the map key. If only the map key is re-assigned, it only proves the maps are distinct, but does not prove the underlying slice data is distinct.
**Solution:** `clone.ExtraFields["x"][1] = '9'` to test the slice memory boundaries instead of `clone.ExtraFields["x"] = ...`
