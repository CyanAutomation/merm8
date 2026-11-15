import assert from "node:assert/strict";
import test from "node:test";

import { canonicalJson } from "../src/cache/canonical-json.js";
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

test("rejecting an oversized entry preserves entries already in the cache", () => {
  const cache = new TtlLruCache<{ value: string }>(2, 100, 30);
  cache.set("small", { value: "ok" });
  cache.set("large", { value: "x".repeat(100) });

  assert.deepEqual(cache.get("small"), { value: "ok" });
  assert.equal(cache.get("large"), undefined);
});

test("canonical JSON ignores object key order but preserves array order", () => {
  const first = { second: 2, nested: { z: 1, a: 0 }, items: [1, 2] };
  const same = { items: [1, 2], nested: { a: 0, z: 1 }, second: 2 };
  const reorderedArray = { items: [2, 1], nested: { a: 0, z: 1 }, second: 2 };

  assert.equal(canonicalJson(first), canonicalJson(same));
  assert.notEqual(canonicalJson(first), canonicalJson(reorderedArray));
});
