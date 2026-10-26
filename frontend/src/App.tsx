import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Header } from "./components/Header";
import { OrderSummary, TicketList } from "./components/OrderSummary";
import { ApiError, request } from "./api";
import { checkoutExpired, eventDate, money, parseAge, validate } from "./types";
import type { Checkout, Event, FieldErrors, Order, PersonDraft } from "./types";

import { readSession, saveSession } from "./session";
import type { Step } from "./session";
const newPerson = (): PersonDraft => ({
  id: crypto.randomUUID(),
  name: "",
  age: "",
});

export default function App() {
  const [restored] = useState(readSession);
  const [event, setEvent] = useState<Event | null>(null);
  const [loadError, setLoadError] = useState("");
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [people, setPeople] = useState<PersonDraft[]>(
    () => restored?.people ?? [newPerson()],
  );
  const [email, setEmail] = useState(restored?.email ?? "");
  const [step, setStep] = useState<Step>(restored?.step ?? "participants");
  const [quote, setQuote] = useState<Checkout | null>(restored?.quote ?? null);
  const [order, setOrder] = useState<Order | null>(restored?.order ?? null);
  const [simulate, setSimulate] = useState(restored?.simulate ?? false);
  const [operation, setOperation] = useState<"checking" | "confirming" | null>(null);
  const busy = operation !== null;
  const [submitted, setSubmitted] = useState(false);
  const [touched, setTouched] = useState<Set<string>>(() => new Set());
  const [serverFields, setServerFields] = useState<FieldErrors>({});
  const [notice, setNotice] = useState(
    restored?.uncertain
      ? "Подтверждение было прервано. Запросите результат — повторный заказ не создастся."
      : "",
  );
  const [uncertain, setUncertain] = useState(restored?.uncertain ?? false);
  const [expired, setExpired] = useState(false);
  const [removed, setRemoved] = useState<{
    person: PersonDraft;
    index: number;
  } | null>(null);
  const [saved, setSaved] = useState(false);
  const focusTarget = useRef<string | null>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const errors = { ...validate(people, email), ...serverFields };
  const stableKey = (key: string) =>
    key.replace(
      /^participants\.(\d+)\./,
      (_, index) => `person.${people[Number(index)]?.id}.`,
    );
  const fieldError = (key: string) =>
    submitted || touched.has(stableKey(key)) ? errors[key] : undefined;
  const ages = people.map((p) => parseAge(p.age));
  const draftTotal =
    event && ages.every((age) => age !== null)
      ? ages.reduce(
          (sum, age) =>
            sum + (age! < 14 ? event.child_price : event.full_price),
          0,
        )
      : null;

  useEffect(() => {
    setSaved(
      saveSession({ people, email, step, quote, order, simulate, uncertain }),
    );
  }, [people, email, step, quote, order, simulate, uncertain]);
  useEffect(() => {
    if (step !== "review" || !quote || uncertain) return;
    const check = () =>
      setExpired((previous) => previous || checkoutExpired(quote.expires_at));
    check();
    const timer = window.setInterval(check, 1000);
    return () => window.clearInterval(timer);
  }, [step, quote, uncertain]);
  useEffect(() => {
    if (focusTarget.current) {
      document.getElementById(focusTarget.current)?.focus();
      focusTarget.current = null;
    }
  }, [people]);

  useEffect(() => {
    let active = true;
    setLoadError("");
    request<Event>("/api/event")
      .then((value) => {
        if (active) {
          setEvent(value);
          if (!value.demo_enabled) setSimulate(false);
        }
      })
      .catch((error: Error) => {
        if (active) setLoadError(error.message);
      });
    return () => {
      active = false;
    };
  }, [loadAttempt]);
  useEffect(() => {
    headingRef.current?.focus({ preventScroll: true });
    window.scrollTo({ top: 0, behavior: "instant" });
  }, [step]);

  function touch(key: string) {
    setTouched((previous) => new Set([...previous, stableKey(key)]));
  }
  function updatePerson(id: string, field: "name" | "age", value: string) {
    setPeople((previous) =>
      previous.map((p) => (p.id === id ? { ...p, [field]: value } : p)),
    );
    setServerFields({});
    setNotice("");
  }
  function addPerson() {
    if (people.length >= 4) return;
    // Existing errors remain visible, but a newly added row starts untouched.
    if (submitted)
      setTouched(
        (previous) =>
          new Set([...previous, ...Object.keys(errors).map(stableKey)]),
      );
    setSubmitted(false);
    setServerFields({});
    setNotice("");
    focusTarget.current = `participants.${people.length}.name`;
    setPeople([...people, newPerson()]);
  }
  function removePerson(index: number) {
    if (people.length <= 1) return;
    setRemoved({ person: people[index], index });
    setPeople(people.filter((_, i) => i !== index));
    setServerFields({});
    setNotice("");
    focusTarget.current = `participants.${Math.min(index, people.length - 2)}.name`;
  }
  function undoRemove() {
    if (!removed || people.length >= 4) return;
    const next = [...people];
    const index = Math.min(removed.index, next.length);
    next.splice(index, 0, removed.person);
    setPeople(next);
    setRemoved(null);
    setServerFields({});
    focusTarget.current = `participants.${index}.name`;
  }
  function edit() {
    setStep("participants");
    setQuote(null);
    setNotice("");
    setUncertain(false);
    setExpired(false);
  }
  function reset() {
    setPeople([newPerson()]);
    setEmail("");
    setOrder(null);
    setEvent(null);
    setRemoved(null);
    setSubmitted(false);
    setTouched(new Set());
    setServerFields({});
    setSimulate(false);
    edit();
    setLoadAttempt((n) => n + 1);
  }
  async function review(e?: FormEvent) {
    e?.preventDefault();
    if (busy) return;
    setSubmitted(true);
    setServerFields({});
    setNotice("");
    const invalid = validate(people, email);
    if (Object.keys(invalid).length) {
      const first = Object.keys(invalid).find(
        (key) => key !== "accompaniment" && key !== "participants",
      );
      requestAnimationFrame(() =>
        document.getElementById(first ?? "accompaniment")?.focus(),
      );
      return;
    }
    setOperation("checking");
    try {
      const result = await request<Checkout>("/api/checkouts", {
        email: email.trim(),
        participants: people.map((p) => ({
          name: p.name.trim(),
          age: Number(p.age),
        })),
        simulate_sold_out: simulate,
      });
      setRemoved(null);
      setQuote(result);
      setStep("review");
      setExpired(false);
      setUncertain(false);
    } catch (error) {
      if (error instanceof ApiError && error.code === "sold_out")
        setStep("sold-out");
      else if (error instanceof ApiError && error.code === "validation") {
        setServerFields(error.fields);
        setNotice(error.message);
        const first = Object.keys(error.fields).find((key) =>
          key === "email" || /^participants\.\d+\.(name|age)$/.test(key),
        );
        requestAnimationFrame(() =>
          document.getElementById(first ?? "accompaniment")?.focus(),
        );
      } else setNotice((error as Error).message);
    } finally {
      setOperation(null);
    }
  }
  async function confirm() {
    if (!quote || busy) return;
    // Save the recovery ID before sending a request that may already create an order.
    saveSession({
      people,
      email,
      step: "review",
      quote,
      order,
      simulate,
      uncertain: true,
    });
    setUncertain(true);
    setOperation("confirming");
    setNotice("");
    try {
      const result = await request<Order>(
        `/api/checkouts/${quote.id}/confirm`,
        {},
      );
      setOrder(result);
      setStep("success");
      setUncertain(false);
    } catch (error) {
      if (error instanceof ApiError && error.code === "sold_out") {
        setStep("sold-out");
        setUncertain(false);
      } else if (
        error instanceof ApiError &&
        ["expired", "not_found"].includes(error.code)
      ) {
        setNotice(error.message);
        setExpired(true);
        setUncertain(false);
      } else {
        setUncertain(true);
        setNotice(
          "Ответ о подтверждении не получен. Повторите попытку — второй заказ не создастся.",
        );
      }
    } finally {
      setOperation(null);
    }
  }

  const finished = step === "success" || step === "sold-out";
  return (
    <div className="shell">
      <a className="skip-link" href="#booking-content">
        Перейти к оформлению
      </a>
      <Header />
      <main className={`booking-main${finished ? " booking-main-result" : ""}`} id="booking-content" tabIndex={-1}>
        <span className="sr-only" role="status">
          {busy
            ? operation === "confirming"
              ? "Оформляем заказ. Дождитесь результата."
              : "Проверяем данные заказа."
            : ""}
        </span>
        {!event ? (
          <section className="loading-panel" aria-live="polite">
            <h1>
              {loadError
                ? "Не удалось загрузить мероприятие"
                : "Готовим ваше приглашение…"}
            </h1>
            <p>{loadError || "Проверяем доступность билетов."}</p>
            {loadError && (
              <button
                className="primary-button"
                onClick={() => setLoadAttempt((n) => n + 1)}
              >
                Попробовать снова
              </button>
            )}
          </section>
        ) : (
          <>
            <div className="booking-heading">
              <p className="breadcrumb">
                Sqoch Live <span>/</span> Билеты на мероприятие
              </p>
              <h1>Оформление билетов</h1>
              <p>
                «{event.title}» · {eventDate(event.date)} · МСК
                <span className="heading-venue">{event.venue}</span>
              </p>
            </div>
            <ol className="steps" aria-label="Этапы оформления">
              {["Участники", "Проверка", "Результат"].map((label, i) => {
                const current =
                  step === "participants" ? 0 : step === "review" ? 1 : 2;
                return (
                  <li
                    key={label}
                    className={
                      i === current ? "current" : i < current ? "complete" : ""
                    }
                    aria-current={i === current ? "step" : undefined}
                  >
                    <span>{i < current ? "✓" : `0${i + 1}`}</span>
                    {label}
                  </li>
                );
              })}
            </ol>
            <div className={`checkout-layout ${finished ? "is-result" : ""}`}>
              <div className="checkout-content">
                {step === "participants" && (
                  <form
                    className="booking-panel"
                    onSubmit={review}
                    noValidate
                    aria-busy={busy}
                  >
                    <div className="panel-heading">
                      <div>
                        <p className="eyebrow">ШАГ 01</p>
                        <h2 ref={headingRef} tabIndex={-1}>
                          Кто пойдёт с вами?
                        </h2>
                        <p>От 1 до 4 участников в одном заказе.</p>
                      </div>
                      <span className="count-badge">{people.length} / 4</span>
                    </div>
                    <div className="price-guide">
                      <div>
                        <strong>{money(event.full_price)}</strong>
                        <span>Полный · от 14 лет</span>
                      </div>
                      <div>
                        <strong>{money(event.child_price)}</strong>
                        <span>Детский · младше 14 лет</span>
                      </div>
                    </div>
                    <fieldset disabled={busy} className="form-fields">
                      <legend className="sr-only">
                        Данные участников и контактный email
                      </legend>
                      <div className="participants">
                        {people.map((person, i) => (
                          <section
                            className="participant"
                            key={person.id}
                            aria-labelledby={`person-${person.id}`}
                          >
                            <div className="participant-heading">
                              <h3 id={`person-${person.id}`}>
                                <span className="person-number">
                                  {String(i + 1).padStart(2, "0")}
                                </span>
                                Участник {i + 1}
                              </h3>
                              {people.length > 1 && (
                                <button
                                  className="remove-button"
                                  type="button"
                                  aria-label={`Удалить участника ${i + 1}`}
                                  title={`Удалить участника ${i + 1}`}
                                  onClick={() => removePerson(i)}
                                >
                                  <svg
                                    width="18"
                                    height="18"
                                    viewBox="0 0 24 24"
                                    fill="none"
                                    stroke="currentColor"
                                    strokeWidth="1.5"
                                    strokeLinecap="round"
                                    strokeLinejoin="round"
                                    aria-hidden="true"
                                  >
                                    <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v5M14 11v5" />
                                  </svg>
                                </button>
                              )}
                            </div>
                            <div className="participant-fields">
                              <div className="field">
                                <label htmlFor={`participants.${i}.name`}>
                                  Имя
                                </label>
                                <input
                                  required
                                  id={`participants.${i}.name`}
                                  type="text"
                                  autoComplete={`section-${person.id} given-name`}
                                  placeholder="Например, Анна"
                                  value={person.name}
                                  maxLength={80}
                                  aria-invalid={
                                    !!fieldError(`participants.${i}.name`)
                                  }
                                  aria-describedby={
                                    fieldError(`participants.${i}.name`)
                                      ? `name-error-${i}`
                                      : undefined
                                  }
                                  onBlur={() => touch(`participants.${i}.name`)}
                                  onChange={(e) =>
                                    updatePerson(
                                      person.id,
                                      "name",
                                      e.target.value,
                                    )
                                  }
                                />
                                {fieldError(`participants.${i}.name`) && (
                                  <p
                                    className="field-error"
                                    id={`name-error-${i}`}
                                  >
                                    {fieldError(`participants.${i}.name`)}
                                  </p>
                                )}
                              </div>
                              <div className="field age-field">
                                <label htmlFor={`participants.${i}.age`}>
                                  Возраст, лет
                                </label>
                                <input
                                  required
                                  id={`participants.${i}.age`}
                                  type="number"
                                  inputMode="numeric"
                                  min={0}
                                  max={120}
                                  step={1}
                                  placeholder="0–120"
                                  value={person.age}
                                  aria-invalid={
                                    !!fieldError(`participants.${i}.age`)
                                  }
                                  aria-describedby={`age-help-${i}${fieldError(`participants.${i}.age`) ? ` age-error-${i}` : ""}`}
                                  onBlur={() => touch(`participants.${i}.age`)}
                                  onChange={(e) =>
                                    updatePerson(
                                      person.id,
                                      "age",
                                      e.target.value,
                                    )
                                  }
                                />
                                <p className="field-hint" id={`age-help-${i}`}>
                                  {parseAge(person.age) === null
                                    ? "Полных лет на день события"
                                    : Number(person.age) < 14
                                      ? "Детский билет"
                                      : "Полный билет"}
                                </p>
                                {fieldError(`participants.${i}.age`) && (
                                  <p
                                    className="field-error"
                                    id={`age-error-${i}`}
                                  >
                                    {fieldError(`participants.${i}.age`)}
                                  </p>
                                )}
                              </div>
                            </div>
                          </section>
                        ))}
                      </div>
                      <button
                        className="add-button"
                        type="button"
                        disabled={people.length >= 4}
                        onClick={addPerson}
                      >
                        <span aria-hidden="true">＋</span>
                        {people.length >= 4
                          ? "В заказе максимум 4 участника"
                          : "Добавить участника"}
                        <span className="add-count">{people.length}/4</span>
                      </button>
                      {removed && (
                        <div className="undo-notice">
                          <span role="status">
                            {removed.person.name.trim()
                              ? `Участник «${removed.person.name.trim()}» удалён.`
                              : "Участник удалён."}
                          </span>
                          <button
                            type="button"
                            className="text-button"
                            onClick={undoRemove}
                            disabled={people.length >= 4}
                          >
                            Вернуть
                          </button>
                          {people.length >= 4 && (
                            <small>
                              Для возврата освободите место в заказе.
                            </small>
                          )}
                        </div>
                      )}
                      <div
                        id="accompaniment"
                        tabIndex={-1}
                        className={`rule-note ${submitted && errors.accompaniment ? "rule-error" : ""}`}
                        role={
                          submitted && errors.accompaniment
                            ? "alert"
                            : undefined
                        }
                      >
                        <span className="rule-icon" aria-hidden="true">
                          i
                        </span>
                        <p>
                          <strong>Вместе со взрослым</strong>Участникам младше
                          18 лет нужен сопровождающий от 18 лет в этом же
                          заказе. С 14 лет действует полный тариф.
                        </p>
                      </div>
                      <div className="contact-section">
                        <h3>Контактные данные</h3>
                        <div className="field">
                          <label htmlFor="email">Контактный email</label>
                          <input
                            required
                            id="email"
                            type="email"
                            inputMode="email"
                            autoComplete="email"
                            autoCapitalize="none"
                            spellCheck={false}
                            placeholder="you@example.ru"
                            value={email}
                            maxLength={254}
                            aria-invalid={!!fieldError("email")}
                            aria-describedby={`email-help${fieldError("email") ? " email-error" : ""}`}
                            onBlur={() => touch("email")}
                            onChange={(e) => {
                              setEmail(e.target.value);
                              setServerFields({});
                              setNotice("");
                            }}
                          />
                          <p className="field-hint" id="email-help">
                            Один email для всех участников заказа.
                          </p>
                          {fieldError("email") && (
                            <p className="field-error" id="email-error">
                              {fieldError("email")}
                            </p>
                          )}
                        </div>
                      </div>
                    </fieldset>
                    {notice && (
                      <p className="error-notice" role="alert">
                        {notice}
                      </p>
                    )}
                    {submitted && Object.keys(errors).length > 0 && (
                      <p className="validation-summary" role="alert">
                        {errors.accompaniment ??
                          "Проверьте выделенные поля, чтобы продолжить."}
                      </p>
                    )}
                    <div className="inline-total" aria-live="polite">
                      <span>Итого · билетов: {people.length}</span>
                      <strong>
                        {draftTotal === null
                          ? "Укажите возраст"
                          : money(draftTotal)}
                      </strong>
                    </div>
                    <div className="panel-actions">
                      <p>
                        На следующем шаге можно проверить
                        <br className="desktop-break" /> и изменить все данные.
                      </p>
                      <button
                        className="primary-button"
                        type="submit"
                        disabled={busy}
                      >
                        {busy ? "Проверяем…" : "Проверить заказ"}
                        <span aria-hidden="true">→</span>
                      </button>
                    </div>
                    <p className="draft-note">
                      Демо без оплаты и отправки писем.
                      {saved
                        ? " Данные сохраняются при обновлении этой вкладки."
                        : ""}
                    </p>
                  </form>
                )}
                {step === "review" && quote && (
                  <section
                    className="booking-panel review-panel"
                    aria-busy={busy}
                  >
                    <p className="eyebrow">ШАГ 02</p>
                    <h2 ref={headingRef} tabIndex={-1}>
                      Всё верно?
                    </h2>
                    <p className="panel-intro">
                      Проверьте участников и контактный email.
                      <br />
                      После подтверждения заказ будет оформлен.
                    </p>
                    <TicketList people={quote.participants} />
                    <div className="review-email">
                      <span>Контактный email</span>
                      <strong>{quote.email}</strong>
                    </div>
                    <div className="rule-note">
                      <span className="rule-icon" aria-hidden="true">
                        i
                      </span>
                      <p>
                        Места пока не зарезервированы. Мы проверим их наличие
                        при подтверждении заказа.
                      </p>
                    </div>
                    {notice && (
                      <p className="error-notice" role="alert">
                        {notice}
                      </p>
                    )}
                    <div className="inline-total">
                      <span>Итого</span>
                      <strong>{money(quote.total)}</strong>
                    </div>
                    <p className="draft-note" role="status">
                      {expired && !uncertain
                        ? "Срок проверки истёк. Обновите расчёт и наличие мест."
                        : `Проверка действительна до ${new Date(quote.expires_at).toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" })} по времени вашего устройства.`}{" "}
                      Места не удерживаются.
                    </p>
                    <p className="draft-note">
                      Демонстрационное оформление без оплаты и отправки писем.
                    </p>
                    <div className="review-actions">
                      <button
                        type="button"
                        className="secondary-button"
                        disabled={busy || uncertain}
                        onClick={edit}
                      >
                        ← Изменить данные
                      </button>
                      <button
                        className="primary-button"
                        disabled={busy}
                        onClick={
                          expired && !uncertain ? () => void review() : confirm
                        }
                      >
                        {busy
                          ? operation === "checking" ? "Проверяем…" : "Оформляем…"
                          : uncertain
                            ? "Узнать результат заказа"
                            : expired
                              ? "Проверить заново"
                              : `Подтвердить · ${money(quote.total)}`}
                      </button>
                    </div>
                  </section>
                )}
                {step === "success" && order && (
                  <section className="booking-panel result-panel">
                    <div
                      className="result-icon success-icon"
                      aria-hidden="true"
                    >
                      ✓
                    </div>
                    <p className="eyebrow">ЗАКАЗ {order.number}</p>
                    <h2 ref={headingRef} tabIndex={-1}>
                      Вы идёте на событие!
                    </h2>
                    <p className="panel-intro">
                      Заказ оформлен. До встречи
                      <br />
                      на «{event.title}».
                    </p>
                    <div className="result-event">
                      {eventDate(event.date)} · МСК<span>{event.venue}</span>
                    </div>
                    <TicketList people={order.participants} />
                    <div className="review-email">
                      <span>Контактный email</span>
                      <strong>{order.email}</strong>
                    </div>
                    <div className="receipt-total">
                      <span>Стоимость заказа</span>
                      <strong>{money(order.total)}</strong>
                    </div>
                    <p className="demo-receipt">
                      Демонстрационное оформление без оплаты и отправки писем.
                    </p>
                    <button className="primary-button" onClick={reset}>
                      Оформить ещё один заказ <span aria-hidden="true">→</span>
                    </button>
                  </section>
                )}
                {step === "sold-out" && (
                  <section className="booking-panel result-panel">
                    <div className="result-icon sold-icon" aria-hidden="true">
                      ⌁
                    </div>
                    <p className="eyebrow">НЕДОСТАТОЧНО МЕСТ</p>
                    <h2 ref={headingRef} tabIndex={-1}>
                      Недостаточно свободных мест
                    </h2>
                    <p className="panel-intro">
                      Свободных мест на всех участников больше нет. Оформить
                      билеты не удалось.
                    </p>
                    <div className="sold-explanation">
                      <strong>Заказ не создан</strong>
                      <p>
                        Имена, возраст и email остались в форме — вводить их
                        заново не нужно.
                      </p>
                    </div>
                    <button className="secondary-button" onClick={edit}>
                      ← Вернуться к участникам
                    </button>
                    {simulate && event.demo_enabled && (
                      <button
                        className="primary-button"
                        onClick={() => {
                          setSimulate(false);
                          edit();
                        }}
                      >
                        Попробовать успешный сценарий
                      </button>
                    )}
                  </section>
                )}
              </div>
              {!finished && (
                <OrderSummary
                  event={event}
                  people={people}
                  total={quote?.total}
                />
              )}
            </div>
            {event.demo_enabled && (
              <details className="demo-controls">
                <summary>Демонстрация сценариев</summary>
                <p>
                  Выберите, что произойдёт при следующем подтверждении. Сценарий
                  нехватки мест не меняет доступность для других заказов.
                </p>
                <div className="demo-options">
                  <button
                    disabled={busy || step !== "participants"}
                    aria-pressed={!simulate}
                    onClick={() => setSimulate(false)}
                  >
                    Успешное оформление
                  </button>
                  <button
                    disabled={busy || step !== "participants"}
                    aria-pressed={simulate}
                    onClick={() => setSimulate(true)}
                  >
                    Места закончатся
                  </button>
                </div>
                {simulate && (
                  <p className="scenario-note">
                    В этом демо места закончатся между проверкой и
                    подтверждением.
                  </p>
                )}
                {step !== "participants" && (
                  <p>Изменить сценарий можно на шаге участников.</p>
                )}
              </details>
            )}
          </>
        )}
      </main>
      <footer>
        <span>Sqoch Ticket</span>
        <span>Впечатления начинаются здесь</span>
      </footer>
    </div>
  );
}
