## 2024-05-24 - Path Splitting Allocation Optimization
**Learning:** For `BroadcastPath` splitting (which separates the prefix segments from the final track name), the previous implementation unconditionally split the entire path and re-sliced to drop the last element (`segments[:len(segments)-1]`). Finding the last slash using `strings.LastIndexByte` and counting the remaining slashes to avoid `strings.Split` when there are 0 or 1 slashes reduces string allocations and improves path splitting time significantly, especially since the typical `BroadcastPath` in `TrackMux` has 1 to 3 segments.
**Action:** Always pre-calculate where to split strings using `strings.LastIndexByte` or `strings.IndexByte` for simple, predictable segment extractions to avoid creating intermediate slices and then throwing away the last element, especially in hot paths like `TrackMux` routing.
## 2024-06-25 - Exact pre-allocation over fixed bounds
**Learning:** For variable depth path splitting (e.g. prefix arrays), pre-calculating the exact number of segments via `strings.Count(str, "/")` and allocating the slice exactly (`make([]T, 0, n)`) is measurably faster than fixed pre-allocation (e.g., `make([]T, 0, 8)`). Removing the final `strings.Split` allocation in the `pathSegments` function further reduces GC pressure.
**Action:** Always count known delimiters in small string parsing rather than falling back to `strings.Split` or using arbitrary fixed pre-allocations when generating slices in hot paths.
## 2023-10-25 - Defer String Formatting in Validation Loops

**What:** Deferred string formatting (`fmt.Sprintf` and concatenation) in MSF catalog validation loops, moving the prefix generation only to code paths where errors actually occur.
**Why:** Unnecessary string formatting and concatenations inside `for` loops (like constructing `addTracks[%d]`) were allocating strings and executing formatting logic on every iteration, even when no validation errors existed, causing unnecessary GC pressure on the "happy path".
**Impact:** Reduced `CatalogDelta.Validate()` execution time by roughly 66% (from ~3300 ns/op to ~1050 ns/op) and allocations from 16 to 10.
**Measurement:** Go benchmarks.
