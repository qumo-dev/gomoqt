## 2024-05-24 - Initial
**Learning:** Initial entry.
**Action:** None.
## 2024-05-24 - String generation performance
**Learning:** In Go, string concatenation using `+` combined with `strconv.FormatUint` is significantly faster than using `fmt.Sprintf` for struct `String()` methods (e.g., up to 2-3x speedup, dropping from 155ns/op to 50ns/op for simple values and 591ns/op to 254ns/op for structs).
**Action:** Replace `fmt.Sprintf` with `strconv` and string concatenation in frequently called `.String()` methods.
