import type { Checkout, Order, PersonDraft } from "./types";
export type Step = "participants" | "review" | "success" | "sold-out";
export type BookingSession = {
  people: PersonDraft[];
  email: string;
  step: Step;
  quote: Checkout | null;
  order: Order | null;
  simulate: boolean;
  uncertain: boolean;
};
const KEY = "sqoch-booking-v1";
const record = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null;
const text = (v: unknown, max: number): v is string =>
  typeof v === "string" && v.length <= max;
const integer = (v: unknown, min: number, max: number): v is number =>
  typeof v === "number" && Number.isInteger(v) && v >= min && v <= max;
function snapshot(v: unknown): boolean {
  if (
    !record(v) ||
    !text(v.id, 64) ||
    !text(v.email, 254) ||
    !integer(v.total, 1000, 8000) ||
    !Array.isArray(v.participants) ||
    v.participants.length < 1 ||
    v.participants.length > 4
  )
    return false;
  return v.participants.every(
    (p) =>
      record(p) &&
      text(p.name, 160) &&
      integer(p.age, 0, 120) &&
      ["full", "child"].includes(String(p.ticket_type)) &&
      integer(p.price, 1000, 2000),
  );
}
export function decodeSession(raw: string | null): BookingSession | null {
  try {
    if (!raw) return null;
    const v: unknown = JSON.parse(raw);
    if (
      !record(v) ||
      v.version !== 1 ||
      !Array.isArray(v.people) ||
      v.people.length < 1 ||
      v.people.length > 4 ||
      !text(v.email, 254)
    )
      return null;
    if (
      !v.people.every(
        (p) =>
          record(p) && text(p.id, 64) && text(p.name, 160) && text(p.age, 20),
      )
    )
      return null;
    if (new Set(v.people.map((p) => p.id)).size !== v.people.length)
      return null;
    if (
      !["participants", "review", "success", "sold-out"].includes(
        String(v.step),
      )
    )
      return null;
    const quote =
      snapshot(v.quote) &&
      record(v.quote) &&
      typeof v.quote.expires_at === "string" &&
      Number.isFinite(Date.parse(v.quote.expires_at))
        ? (v.quote as Checkout)
        : null;
    const order =
      snapshot(v.order) &&
      record(v.order) &&
      text(v.order.number, 64) &&
      typeof v.order.created_at === "string" &&
      Number.isFinite(Date.parse(v.order.created_at))
        ? (v.order as Order)
        : null;
    let step = v.step as Step;
    if ((step === "review" && !quote) || (step === "success" && !order))
      step = "participants";
    return {
      people: v.people as PersonDraft[],
      email: v.email,
      step,
      quote,
      order,
      simulate: v.simulate === true,
      uncertain: step === "review" && v.uncertain === true,
    };
  } catch {
    return null;
  }
}
export function readSession(): BookingSession | null {
  try {
    return decodeSession(sessionStorage.getItem(KEY));
  } catch {
    return null;
  }
}
export function saveSession(value: BookingSession): boolean {
  try {
    sessionStorage.setItem(KEY, JSON.stringify({ version: 1, ...value }));
    return true;
  } catch {
    return false;
  }
}
