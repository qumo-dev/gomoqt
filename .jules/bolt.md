## 2024-05-23 - Optimize ApplyDelta with O(1) track lookups
**Learning:** O(N^2) algorithms on small data sets in Go can be faster than O(N) algorithms that rely on map allocation and hashing due to contiguous memory cache locality and low constant factors.
**Action:** Always measure algorithmic changes. Do not assume replacing an O(N^2) array search with an O(N) map-based lookup is inherently faster for small N. Validate with realistic benchmarks before committing to complex structures.
