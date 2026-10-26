import { ageLabel, eventDate, money, parseAge } from "../types";
import type { Event, Participant, PersonDraft } from "../types";
export function TicketList({ people }: { people: Participant[] }) {
  return (
    <ul className="ticket-list">
      {people.map((p, i) => (
        <li key={i}>
          <span className="person-avatar" aria-hidden="true">
            {p.name.slice(0, 1).toLocaleUpperCase("ru")}
          </span>
          <div className="ticket-person">
            <strong>{p.name}</strong>
            <span>
              {ageLabel(p.age)} ·{" "}
              {p.ticket_type === "child" ? "Детский" : "Полный"} билет
            </span>
          </div>
          <strong className="ticket-price">{money(p.price)}</strong>
        </li>
      ))}
    </ul>
  );
}
export function OrderSummary({
  event,
  people,
  total,
  confirmed = false,
}: {
  event: Event;
  people: PersonDraft[];
  total?: number;
  confirmed?: boolean;
}) {
  const ages = people.map((p) => parseAge(p.age));
  const complete = ages.every((age) => age !== null);
  const full = ages.filter((age) => age !== null && age >= 14).length;
  const child = ages.filter((age) => age !== null && age < 14).length;
  const amount =
    total ??
    (complete ? full * event.full_price + child * event.child_price : null);
  return (
    <aside className="order-summary" aria-label="Состав и стоимость заказа">
      <div className="event-art" aria-hidden="true">
        <div className="art-orbit orbit-one" />
        <div className="art-orbit orbit-two" />
        <div className="art-planet" />
        <span className="art-label">SQOCH / LIVE</span>
        <span className="art-caption">ВЫЙТИ ЗА РАМКИ</span>
        <span className="art-star">✦</span>
      </div>
      <div className="summary-content">
        <p className="eyebrow">НАУКА · МУЗЫКА · ОТКРЫТИЯ</p>
        <h2>{event.title}</h2>
        <p className="event-meta">
          <span aria-hidden="true">◷</span>
          {eventDate(event.date)} · МСК
        </p>
        <p className="event-meta">
          <span aria-hidden="true">⌖</span>
          {event.venue}
        </p>
        <div className="summary-divider" />
        <h3>
          Ваш заказ <span>{people.length} из 4</span>
        </h3>
        <div className="summary-lines">
          {full > 0 && (
            <p>
              <span>Полный × {full}</span>
              <span>{money(full * event.full_price)}</span>
            </p>
          )}
          {child > 0 && (
            <p>
              <span>Детский × {child}</span>
              <span>{money(child * event.child_price)}</span>
            </p>
          )}
          {!complete && (
            <p className="price-hint">
              Укажите возраст каждого участника — мы рассчитаем стоимость.
            </p>
          )}
        </div>
        <div className="summary-total">
          <span>Итого</span>
          <strong>{amount === null ? "—" : money(amount)}</strong>
        </div>
        <p className="summary-note">Без дополнительных сборов</p>
        {!confirmed && (
          <div className="seats-note">
            <span className="dot" />
            Места проверим при подтверждении
          </div>
        )}
      </div>
    </aside>
  );
}
