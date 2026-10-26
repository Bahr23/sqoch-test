import test from "node:test";
import assert from "node:assert/strict";
import { decodeSession } from "../src/session.ts";
import { checkoutExpired, parseAge, validate } from "../src/types.ts";
const people = [{ id: "person-1", name: "Анна", age: "18" }];
const quote = {
  id: "abcdefghijklmnopqrstuv",
  email: "anna@example.ru",
  participants: [{ name: "Анна", age: 18, ticket_type: "full", price: 2000 }],
  total: 2000,
  expires_at: "2026-11-21T10:00:00Z",
};
const session = {
  version: 1,
  people,
  email: "anna@example.ru",
  step: "review",
  quote,
  order: null,
  simulate: false,
  uncertain: true,
};
test("restore confirmation recovery without losing the checkout ID", () => {
  const restored = decodeSession(JSON.stringify(session));
  assert.equal(restored?.step, "review");
  assert.equal(restored?.quote?.id, quote.id);
  assert.equal(restored?.uncertain, true);
  assert.deepEqual(restored?.people, people);
});
test("restore confirmed result instead of offering a duplicate purchase", () => {
  const order = {
    ...quote,
    id: "order-id",
    number: "SQ-12345",
    created_at: "2026-11-21T09:50:00Z",
  };
  const restored = decodeSession(
    JSON.stringify({ ...session, step: "success", order, uncertain: false }),
  );
  assert.equal(restored?.step, "success");
  assert.equal(restored?.order?.number, "SQ-12345");
  assert.equal(restored?.uncertain, false);
});
test("corrupted or incompatible storage falls back safely", () => {
  for (const raw of [
    null,
    "{",
    "null",
    "[]",
    "{}",
    JSON.stringify({ ...session, version: 2 }),
    JSON.stringify({ ...session, people: [...people, ...people] }),
  ])
    assert.equal(decodeSession(raw), null);
  const restored = decodeSession(
    JSON.stringify({ ...session, quote: { id: "broken" } }),
  );
  assert.equal(restored?.step, "participants");
  assert.equal(restored?.uncertain, false);
});
test("keep invalid unfinished draft editable without silently coercing age", () => {
  const restored = decodeSession(
    JSON.stringify({
      ...session,
      step: "participants",
      people: [{ id: "p", name: "", age: "13.5" }],
      quote: null,
    }),
  );
  assert.equal(restored?.people[0].age, "13.5");
  for (const value of ["", "13.5", "-1", "121", "1e2"])
    assert.equal(parseAge(value), null);
  assert.equal(parseAge("0"), 0);
  assert.equal(parseAge("18"), 18);
});
test("full fare minors still require an accompanying adult", () => {
  assert.ok(
    validate([{ id: "p", name: "Анна", age: "17" }], "anna@example.ru")
      .accompaniment,
  );
  assert.deepEqual(
    validate(
      [...people, { id: "p2", name: "Маша", age: "13" }],
      "anna@example.ru",
    ),
    {},
  );
});

test("checkout expiry switches exactly at the deadline", () => {
  const end = Date.parse(quote.expires_at);
  assert.equal(checkoutExpired(quote.expires_at, end - 1), false);
  assert.equal(checkoutExpired(quote.expires_at, end), true);
  assert.equal(checkoutExpired(quote.expires_at, end + 1), true);
});
