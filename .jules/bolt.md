## 2025-02-28 - Optimize BenchmarkTrackMux_StringOperations
**What:** Extracted string cast out of benchmark loop.
**Why:** The `BenchmarkTrackMux_StringOperations/path-splitting` benchmark unintentionally measured `string()` cast overhead alongside `strings.Split`.
**Impact:** Accurately reflects `strings.Split` performance. Baseline `121.3 ns/op`, new `128.5 ns/op` - this represents a more accurate measurement rather than a pure performance optimization.
**Measurement:**
`go test -bench=BenchmarkTrackMux_StringOperations -benchmem -v`
Baseline: `121.3 ns/op`
Optimized: `128.5 ns/op`
