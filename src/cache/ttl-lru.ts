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
    if (this.maxEntries <= 0 || this.ttlMs <= 0 || this.maxBytes <= 0) return;
    this.pruneExpired();
    const previous = this.entries.get(key);
    if (previous) this.remove(key, previous);
    const serialized = JSON.stringify(value);
    const weight = new TextEncoder().encode(key).byteLength + new TextEncoder().encode(serialized).byteLength;
    if (weight > this.maxBytes) return;
    const entry: Entry = { serialized, expiresAt: this.now() + this.ttlMs, weight };
    this.entries.set(key, entry);
    this.bytes += weight;
    while (this.entries.size > this.maxEntries || this.bytes > this.maxBytes) {
      const oldestKey = this.entries.keys().next().value as string | undefined;
      if (oldestKey === undefined) break;
      const oldest = this.entries.get(oldestKey);
      if (oldest) this.remove(oldestKey, oldest);
    }
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

/** Canonical JSON makes semantically equal object-key orders share a cache key. */
export function canonicalJson(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
  if (value && typeof value === "object") {
    const record = value as Record<string, unknown>;
    return `{${Object.keys(record).sort().map((key) => `${JSON.stringify(key)}:${canonicalJson(record[key])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}
