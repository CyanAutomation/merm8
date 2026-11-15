type Entry = { serialized: string; expiresAt: number; weight: number };

/** A small JSON-value LRU with both entry and UTF-8 byte limits. */
export class TtlLruCache<T> {
  private readonly entries = new Map<string, Entry>();
  private bytes = 0;

  constructor(
    private readonly maxEntries: number,
    private readonly ttlMs: number,
    private readonly maxBytes: number,
    private readonly now: () => number = Date.now,
  ) {}

  get(key: string): T | undefined {
    this.pruneExpired();
    const entry = this.entries.get(key);
    if (!entry) return undefined;
    if (entry.expiresAt <= this.now()) {
      this.remove(key, entry);
      return undefined;
    }
    this.entries.delete(key);
    this.entries.set(key, entry);
    return JSON.parse(entry.serialized) as T;
  }

  set(key: string, value: T): void {
    if (!this.canStoreEntries()) return;
    this.pruneExpired();
    this.removeExisting(key);
    const entry = this.createEntry(key, value);
    if (!entry) return;
    this.entries.set(key, entry);
    this.bytes += entry.weight;
    this.evictOverflow();
  }

  private canStoreEntries(): boolean {
    return this.maxEntries > 0 && this.ttlMs > 0 && this.maxBytes > 0;
  }

  private removeExisting(key: string): void {
    const previous = this.entries.get(key);
    if (previous) this.remove(key, previous);
  }

  private createEntry(key: string, value: T): Entry | undefined {
    const serialized = JSON.stringify(value);
    const encoder = new TextEncoder();
    const weight = encoder.encode(key).byteLength + encoder.encode(serialized).byteLength;
    if (weight > this.maxBytes) return undefined;
    return { serialized, expiresAt: this.now() + this.ttlMs, weight };
  }

  private evictOverflow(): void {
    while (this.entries.size > this.maxEntries || this.bytes > this.maxBytes) {
      if (!this.evictOldest()) return;
    }
  }

  private evictOldest(): boolean {
    const oldestKey = this.entries.keys().next().value as string | undefined;
    if (oldestKey === undefined) return false;
    const oldest = this.entries.get(oldestKey);
    if (!oldest) return false;
    this.remove(oldestKey, oldest);
    return true;
  }

  private pruneExpired(): void {
    const now = this.now();
    for (const [key, entry] of this.entries) {
      if (entry.expiresAt <= now) this.remove(key, entry);
    }
  }

  private remove(key: string, entry: Entry): void {
    this.entries.delete(key);
    this.bytes -= entry.weight;
  }
}
