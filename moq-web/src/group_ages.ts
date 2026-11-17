/**
 * Expires the groups of one subscription that its Subscriber Max Age leaves
 * behind (moq-lite-06 §6.2), mirroring Go's `moqt/group_ages.go`.
 *
 * A group's age is measured in frame timestamps alone: from the group's reach,
 * the first frame timestamp of the next group by sequence, to the newest frame
 * timestamp of the latest group that has a frame. A group whose successor has
 * presented no frame never expires. Reach is exclusive, so an age equal to Max
 * Age already expires the group; a Max Age of 0 keeps only the latest group.
 * "Newest" is the most recently written frame of the latest group.
 */

/** What the tracker can do to a group: reset it once it has expired. */
export interface AgedGroup {
	readonly sequence: number;
	expire(): void;
}

interface GroupAge {
	seq: number;
	first?: number;
	writer?: AgedGroup;
}

export class GroupAges {
	readonly #maxAge: () => number;
	readonly #timescale: number;
	// Ascending by sequence: open groups and the closed ones still needed as
	// the reach of an open one.
	#groups: GroupAge[] = [];
	#latest = -1;
	#newest = 0;

	/**
	 * @param maxAge - Returns the subscriber's current Max Age, in milliseconds.
	 * @param timescale - The track's timestamp units per second.
	 */
	constructor(maxAge: () => number, timescale: number) {
		this.#maxAge = maxAge;
		this.#timescale = timescale;
	}

	/** Registers a newly opened group. */
	add(w: AgedGroup): void {
		const [i, found] = this.#find(w.sequence);
		if (found) {
			this.#groups[i]!.writer = w;
			return;
		}
		this.#groups.splice(i, 0, { seq: w.sequence, writer: w });
	}

	/** Records that w's stream ended; its first timestamp stays as a reach. */
	closed(w: AgedGroup): void {
		const [i, found] = this.#find(w.sequence);
		if (found && this.#groups[i]!.writer === w) {
			this.#groups[i]!.writer = undefined;
		}
		this.#prune();
	}

	/** Records a frame w writes at ts, and expires the groups it leaves behind. */
	observe(w: AgedGroup, ts: number): void {
		const [i, found] = this.#find(w.sequence);
		if (!found) return;
		const g = this.#groups[i]!;
		if (g.first === undefined) g.first = ts;
		if (w.sequence >= this.#latest) {
			this.#latest = w.sequence;
			this.#newest = ts;
		}

		const maxAge = this.#maxAge();
		let reach: number | undefined;
		const expired: AgedGroup[] = [];
		// Walk down from the newest group, carrying the first timestamp of the
		// group above as the reach of the one below.
		for (let j = this.#groups.length - 1; j >= 0; j--) {
			const e = this.#groups[j]!;
			if (
				e.writer !== undefined && e.seq < this.#latest && reach !== undefined &&
				this.#expired(reach, maxAge)
			) {
				expired.push(e.writer);
				e.writer = undefined;
			}
			if (e.first !== undefined) reach = e.first;
		}
		if (expired.length > 0) {
			this.#prune();
			for (const ew of expired) ew.expire();
		}
	}

	#expired(reach: number, maxAge: number): boolean {
		if (this.#newest < reach) {
			// Timestamps went backwards; nothing proves the group is behind.
			return false;
		}
		const age = this.#newest - reach;
		// age / timescale >= maxAge / 1000, compared exactly when the products
		// exceed the safe-integer range.
		const a = age * 1000;
		const b = maxAge * this.#timescale;
		if (a <= Number.MAX_SAFE_INTEGER && b <= Number.MAX_SAFE_INTEGER) {
			return a >= b;
		}
		return BigInt(age) * 1000n >= BigInt(maxAge) * BigInt(this.#timescale);
	}

	#find(seq: number): [number, boolean] {
		let lo = 0;
		let hi = this.#groups.length;
		while (lo < hi) {
			const mid = (lo + hi) >>> 1;
			const s = this.#groups[mid]!.seq;
			if (s < seq) lo = mid + 1;
			else hi = mid;
		}
		return [lo, this.#groups[lo]?.seq === seq];
	}

	// Drops the closed groups below the oldest open one.
	#prune(): void {
		let i = 0;
		while (i < this.#groups.length - 1 && this.#groups[i]!.writer === undefined) i++;
		if (i > 0) this.#groups.splice(0, i);
	}
}
