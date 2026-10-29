import assert from "node:assert/strict";
import test from "node:test";

import { TtlLruCache } from "../src/cache/ttl-lru.js";

test("TTL LRU refreshes recency, expires entries, and enforces its byte budget", () => {
  let now = 0;
  const cache = new TtlLruCache<{ value: string }>(2, 10, 30, () => now);
  cache.set("a", { value: "1" });
  cache.set("b", { value: "2" });
  assert.deepEqual(cache.get("a"), { value: "1" });
  cache.set("c", { value: "3" });
  assert.equal(cache.get("b"), undefined);
  assert.deepEqual(cache.get("a"), { value: "1" });
  now = 11;
  assert.equal(cache.get("a"), undefined);
});

test("cached JSON values are isolated from caller mutation", () => {
  const cache = new TtlLruCache<{ nested: { count: number } }>(2, 100, 1000);
  const value = { nested: { count: 1 } };
  cache.set("key", value);
  value.nested.count = 2;
  const result = cache.get("key")!;
  result.nested.count = 3;
  assert.deepEqual(cache.get("key"), { nested: { count: 1 } });
});
