## 2024-05-24 - Path Splitting Allocation Optimization
**Learning:** For `BroadcastPath` splitting (which separates the prefix segments from the final track name), the previous implementation unconditionally split the entire path and re-sliced to drop the last element (`segments[:len(segments)-1]`). Finding the last slash using `strings.LastIndexByte` and counting the remaining slashes to avoid `strings.Split` when there are 0 or 1 slashes reduces string allocations and improves path splitting time significantly, especially since the typical `BroadcastPath` in `TrackMux` has 1 to 3 segments.
**Action:** Always pre-calculate where to split strings using `strings.LastIndexByte` or `strings.IndexByte` for simple, predictable segment extractions to avoid creating intermediate slices and then throwing away the last element, especially in hot paths like `TrackMux` routing.
## 2024-06-25 - Exact pre-allocation over fixed bounds
**Learning:** For variable depth path splitting (e.g. prefix arrays), pre-calculating the exact number of segments via `strings.Count(str, "/")` and allocating the slice exactly (`make([]T, 0, n)`) is measurably faster than fixed pre-allocation (e.g., `make([]T, 0, 8)`). Removing the final `strings.Split` allocation in the `pathSegments` function further reduces GC pressure.
**Action:** Always count known delimiters in small string parsing rather than falling back to `strings.Split` or using arbitrary fixed pre-allocations when generating slices in hot paths.
## Repeated Regex Compilation

**Learning:** Move `regexp.MustCompile` calls out of functions into package-level variables. `regexp.Regexp` objects are safe for concurrent use by multiple goroutines. This avoids significant CPU and allocation overhead from recompiling the regex on every function call.
**Impact:** Local benchmark showed an improvement from ~6775 ns/op (33 allocs) to ~564 ns/op (2 allocs) when moved to the package level.
