## 2025-02-12 - Swap-and-Pop Slice Deletion

**Learning:** When deleting an element from a slice where element order does not semantically matter, replacing the standard O(N) `append(slice[:i], slice[i+1:]...)` shift with an O(1) 'swap-and-trim' approach (swapping the target with the last element and truncating) provides measurable performance improvements, especially as the slice grows larger or the underlying structs become more complex.

**Action:** Before defaulting to an O(N) slice deletion, evaluate if the data structure's element order must be strictly preserved. If order is flexible, apply the O(1) swap-and-pop pattern, explicitly remembering to zero out the last element (`slice[len(slice)-1] = Type{}`) before truncation to prevent memory leaks if the slice contains pointers.
