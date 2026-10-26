export type Event = {
  id: string;
  title: string;
  date: string;
  venue: string;
  remaining: number;
  full_price: number;
  child_price: number;
  demo_enabled: boolean;
};
export type Participant = {
  name: string;
  age: number;
  ticket_type: "full" | "child";
  price: number;
};
export type Checkout = {
  id: string;
  email: string;
  participants: Participant[];
  total: number;
  expires_at: string;
};
export type Order = {
  id: string;
  number: string;
  email: string;
  participants: Participant[];
  total: number;
  created_at: string;
};
export type PersonDraft = { id: string; name: string; age: string };
export type FieldErrors = Record<string, string>;
export const money = (value: number) =>
  new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
    maximumFractionDigits: 0,
  }).format(value);
export const ageLabel = (age: number) =>
  `${age} ${age % 100 >= 11 && age % 100 <= 14 ? "лет" : age % 10 === 1 ? "год" : age % 10 >= 2 && age % 10 <= 4 ? "года" : "лет"}`;
export const eventDate = (value: string) =>
  new Intl.DateTimeFormat("ru-RU", {
    day: "numeric",
    year: "numeric",
    month: "long",
    hour: "2-digit",
    minute: "2-digit",
    timeZone: "Europe/Moscow",
  }).format(new Date(value));
export function parseAge(value: string): number | null {
  if (!/^\d{1,3}$/.test(value)) return null;
  const age = Number(value);
  return age <= 120 ? age : null;
}
export function validate(people: PersonDraft[], email: string): FieldErrors {
  const errors: FieldErrors = {};
  if (people.length < 1 || people.length > 4)
    errors.participants = "В заказе должно быть от 1 до 4 участников.";
  people.forEach((p, i) => {
    if (!p.name.trim() || [...p.name.trim()].length > 80)
      errors[`participants.${i}.name`] = "Укажите имя: от 1 до 80 символов.";
    if (parseAge(p.age) === null)
      errors[`participants.${i}.age`] =
        "Укажите целый возраст от 0 до 120 лет.";
  });
  if (
    !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim()) ||
    email.trim().length > 254
  )
    errors.email = "Укажите email в формате name@example.ru.";
  if (
    people.some((p) => (parseAge(p.age) ?? 120) < 18) &&
    !people.some((p) => (parseAge(p.age) ?? -1) >= 18)
  ) {
    errors.accompaniment =
      "Участнику младше 18 лет нужен сопровождающий от 18 лет в этом же заказе.";
  }
  return errors;
}

export const checkoutExpired = (expiresAt: string, now = Date.now()) =>
  now >= Date.parse(expiresAt);
