## 2024-06-18 - Inline function calls in tight loops for small operations
**Learning:** In Go, function calls within tight loops (like `for ... range` iterating over slices) can introduce measurable overhead, especially when the called function performs a trivial calculation (e.g., length determination and basic arithmetic).
**Action:** For small, pure calculations within hot paths or loops, manually inlining the logic can yield minor but measurable performance improvements by eliminating function call overhead, as seen with `StringLen` inside `StringArrayLen`.
