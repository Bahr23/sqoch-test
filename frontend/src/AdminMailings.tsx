import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { api, AuthError } from "./admin-api";
import type { Event } from "./types";
import MailingRun from "./MailingRun";
import MailingConfirmation from "./MailingConfirmation";
import { mailingStatus } from "./mailing-types";
import type { Recipient, Mailing, MailingDetails as Details } from "./mailing-types";

type Audience = { eventID: string; loading: boolean; error: string; values: Recipient[] };
const emptyAudience: Audience = { eventID: "", loading: false, error: "", values: [] };
const updatedDate = (value: string) => new Intl.DateTimeFormat("ru-RU", {
  day: "numeric", month: "long", hour: "2-digit", minute: "2-digit",
}).format(new Date(value));

export default function AdminMailings({ onUnauthorized }: { onUnauthorized: () => void }) {
  const [route] = useState(() => {
    const match = window.location.pathname.match(/^\/mailings\/([^/]+)\/edit\/?$/);
    return { editing: /^\/mailings\/new\/?$/.test(window.location.pathname) || !!match, id: match?.[1] ?? "" };
  });
  const [id, setId] = useState(route.id);
  const [list, setList] = useState<Mailing[]>([]);
  const [events, setEvents] = useState<Event[]>([]);
  const [saved, setSaved] = useState<Details | null>(null);
  const [confirmation, setConfirmation] = useState<Details | null>(null);
  const [launchError, setLaunchError] = useState("");
  const [eventID, setEventID] = useState("");
  const [body, setBody] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [fields, setFields] = useState<Record<string, string>>({});
  const [message, setMessage] = useState("");
  const [dirty, setDirty] = useState(false);
  const [audience, setAudience] = useState<Audience>(emptyAudience);
  const [audienceAttempt, setAudienceAttempt] = useState(0);
  const heading = useRef<HTMLHeadingElement>(null);
  const launchButton = useRef<HTMLButtonElement>(null);
  const unauthorized = useRef(onUnauthorized);
  unauthorized.current = onUnauthorized;

  useEffect(() => {
    let active = true;
    setLoading(true); setLoadError("");
    async function load() {
      try {
        if (route.editing) {
          const [available, draft] = await Promise.all([
            api<Event[]>("events"), route.id ? api<Details>(`mailings/${route.id}`) : Promise.resolve(null),
          ]);
          if (!active) return;
          setEvents(available);
          if (draft) {
            setSaved(draft); setEventID(draft.event_id); setBody(draft.body);
          }
        } else {
          const values = await api<Mailing[]>("mailings");
          if (active) setList(values);
        }
      } catch (err) {
        if (!active) return;
        if (err instanceof AuthError && err.status === 401) unauthorized.current();
        else setLoadError((err as Error).message);
      } finally { if (active) setLoading(false); }
    }
    void load();
    return () => { active = false; };
  }, [route, attempt]);

  useEffect(() => {
    if (!route.editing || !eventID || loading || loadError || (saved && saved.status !== "draft")) return;
    let active = true;
    setAudience({ eventID, loading: true, error: "", values: [] });
    api<Recipient[]>(`events/${eventID}/recipients`).then(values => {
      if (active) setAudience({ eventID, loading: false, error: "", values });
    }).catch((err: Error) => {
      if (!active) return;
      if (err instanceof AuthError && err.status === 401) unauthorized.current();
      else setAudience({ eventID, loading: false, error: err.message, values: [] });
    });
    return () => { active = false; };
  }, [eventID, audienceAttempt, loading, loadError, route.editing, saved?.status]);

  useEffect(() => { if (!loading) heading.current?.focus({ preventScroll: true }); }, [loading]);
  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ""; };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  function changed() { setDirty(true); setMessage(""); setError(""); setFields({}); }
  async function prepareLaunch() {
    if (busy || dirty || !id) return;
    setBusy(true); setError(""); setLaunchError("");
    try {
      const value = await api<Details>(`mailings/${id}`);
      setSaved(value); setEventID(value.event_id); setBody(value.body);
      setAudience({ eventID: value.event_id, loading: false, error: "", values: value.recipients });
      if (value.status === "draft") {
        if (!value.body.trim() || !value.recipients.length || !value.event_available) setError("Для запуска нужны текст, доступный ивент и хотя бы один получатель.");
        else setConfirmation(value);
      }
    } catch (err) { if (err instanceof AuthError && err.status === 401) unauthorized.current(); else setError((err as Error).message); }
    finally { setBusy(false); }
  }
  async function confirmLaunch() {
    if (!confirmation || busy) return;
    setBusy(true); setLaunchError("");
    try {
      const value = await api<Details>(`mailings/${id}/confirm`, { expected_updated_at: confirmation.updated_at, audience_hash: confirmation.audience_hash });
      setSaved(value); setConfirmation(null); setDirty(false);
    } catch (err) { if (err instanceof AuthError && err.status === 401) unauthorized.current(); else setLaunchError((err as Error).message); }
    finally { setBusy(false); }
  }
  async function save(e: FormEvent) {
    e.preventDefault();
    if (busy || !eventID) return;
    setBusy(true); setError(""); setFields({}); setMessage("");
    try {
      const value = await api<Details>(id ? `mailings/${id}` : "mailings", { event_id: eventID, body }, id ? "PUT" : "POST");
      setId(value.id); setSaved(value); setBody(value.body); setDirty(false);
      setAudience({ eventID: value.event_id, loading: false, error: "", values: value.recipients });
      setMessage("Черновик сохранён. Сообщения не отправлялись.");
      window.history.replaceState(null, "", `/mailings/${value.id}/edit`);
    } catch (err) {
      if (err instanceof AuthError && err.status === 401) unauthorized.current();
      else {
        setError((err as Error).message);
        if (err instanceof AuthError) {
          setFields(err.fields);
          requestAnimationFrame(() => document.getElementById(`mailing-${Object.keys(err.fields)[0]}`)?.focus());
        }
      }
    } finally { setBusy(false); }
  }

  const selected = events.find(e => e.id === eventID);
  const currentAudience = audience.eventID === eventID ? audience : { ...emptyAudience, loading: !!eventID };
  if (loading) return <p role="status">Загружаем рассылки…</p>;
  if (loadError) return <section className="admin-card admin-placeholder">
    <h1 ref={heading} tabIndex={-1}>Не удалось загрузить рассылки</h1>
    <p className="error-notice" role="alert">{loadError}</p>
    <button className="secondary-button" onClick={() => setAttempt(n => n + 1)}>Попробовать снова</button>
    <a className="catalog-back" href="/mailings">← Все рассылки</a>
  </section>;
  if (route.editing && saved && saved.status !== "draft") return <MailingRun initial={saved} onUnauthorized={onUnauthorized} />;

  return <section className="mailings-workspace">
    <div className="events-heading">
      <div><p className="eyebrow">ПАНЕЛЬ ОРГАНИЗАТОРА</p><h1 ref={heading} tabIndex={-1}>{route.editing ? id ? "Редактирование рассылки" : "Новая рассылка" : "Рассылки"}</h1></div>
      {route.editing ? <a className="secondary-button" href="/mailings">← Все рассылки</a> : <a className="primary-button" href="/mailings/new">Создать рассылку</a>}
    </div>
    {route.editing ? <>
      {confirmation && <MailingConfirmation value={confirmation} busy={busy} error={launchError} onClose={() => { setConfirmation(null); requestAnimationFrame(() => launchButton.current?.focus()); }} onConfirm={confirmLaunch} />}
      {message && <p className="admin-message" role="status">{message}</p>}
      <div className="mailing-layout">
        <form className="event-editor mailing-editor" onSubmit={save} aria-busy={busy}>
          <div className="mailing-section-heading"><h2>Сообщение</h2><span className="draft-badge">Черновик</span></div>
          <fieldset className="form-fields" disabled={busy}>
            <legend className="sr-only">Содержание рассылки</legend>
            <div className="field">
              <label htmlFor="mailing-event_id">Ивент</label>
              <div className="select-control">
                <select id="mailing-event_id" required value={eventID} onChange={e => { changed(); setEventID(e.target.value); }} aria-invalid={!!fields.event_id} aria-describedby={fields.event_id ? "mailing-event-error" : "mailing-event-help"}>
                  <option value="" disabled>Выберите ивент</option>
                  {saved && !events.some(e => e.id === saved.event_id) && <option value={saved.event_id} disabled>{saved.event_title} — удалён</option>}
                  {events.map(e => <option key={e.id} value={e.id}>{e.title}</option>)}
                </select>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false"><path d="m6 9 6 6 6-6" /></svg>
              </div>
              <p className="field-hint" id="mailing-event-help">После выбора покажем получателей по действующим покупкам.</p>
              {fields.event_id && <p className="field-error" id="mailing-event-error">{fields.event_id}</p>}
              {!events.length && <p className="field-hint">Нет доступных ивентов. <a className="inline-link" href="/events/new">Создать ивент</a></p>}
              {eventID && !selected && <p className="field-error">Ивент удалён. Выберите другой, чтобы сохранить изменения.</p>}
            </div>
            <div className="field">
              <label htmlFor="mailing-body">Текст сообщения</label>
              <textarea id="mailing-body" rows={10} maxLength={10000} value={body} placeholder="Напишите, что хотите сообщить гостям…" onChange={e => { changed(); setBody(e.target.value); }} aria-invalid={!!fields.body} aria-describedby={`mailing-body-help${fields.body ? " mailing-body-error" : ""}`} />
              <div className="mailing-text-hint"><p className="field-hint" id="mailing-body-help">Текст можно дописать позже.</p><span>{[...body].length.toLocaleString("ru-RU")} / 10 000</span></div>
              {fields.body && <p className="field-error" id="mailing-body-error">{fields.body}</p>}
            </div>
          </fieldset>
          {error && <p className="error-notice" role="alert">{error}</p>}
          <p className="mailing-save-state">{dirty ? "Есть несохранённые изменения" : saved ? `Сохранено ${updatedDate(saved.updated_at)}` : "Выберите ивент, чтобы сохранить черновик"}</p>
          <div className="editor-actions">
            <button className="primary-button" type="submit" disabled={busy || !selected}>{busy ? "Сохраняем…" : "Сохранить черновик"}</button>
            <button ref={launchButton} className="secondary-button" type="button" disabled={busy || dirty || !id || !body.trim() || !selected} onClick={prepareLaunch} aria-describedby="mailing-send-help">Запустить рассылку</button>
          </div>
          <p className="field-hint" id="mailing-send-help">Сначала сохраните черновик. Запуск потребует отдельного подтверждения. Реальные письма пока не отправляются.</p>
        </form>
        <aside className="mailing-audience" aria-labelledby="recipients-heading" aria-busy={currentAudience.loading}>
          <div className="mailing-section-heading"><h2 id="recipients-heading">Получатели</h2>{eventID && !currentAudience.loading && !currentAudience.error && <span className="recipient-count">{currentAudience.values.length}</span>}</div>
          <p className="mailing-audience-help">Контактные email действующих заказов. Повторы объединены, отменённые покупки исключены.</p>
          {!eventID ? <div className="mailing-empty"><p>Сначала выберите ивент</p><span>Здесь появятся имена участников и контактные email их заказов.</span></div>
            : currentAudience.loading ? <p className="mailing-empty" role="status">Загружаем получателей…</p>
            : currentAudience.error ? <div className="error-notice" role="alert"><p>{currentAudience.error}</p><button className="secondary-button" type="button" onClick={() => setAudienceAttempt(n => n + 1)}>Повторить загрузку</button></div>
            : <>
              <p className="sr-only" role="status">Получателей: {currentAudience.values.length}</p>
              {!currentAudience.values.length ? <div className="mailing-empty"><p>Пока нет получателей</p><span>У ивента ещё нет действующих покупок. Черновик можно сохранить.</span></div>
                : <ul className="mailing-recipient-list">{currentAudience.values.map(r => <li key={r.email}><p className="recipient-names">{r.names.join(", ")}</p><p className="recipient-email">{r.email}</p></li>)}</ul>}
              <p className="field-hint">Список обновляется при открытии и сохранении черновика. Имена относятся к участникам заказа; email указан один на заказ.</p>
            </>}
        </aside>
      </div>
    </> : <>
      <p className="mailings-intro">Подготовьте сообщение гостям, подтвердите запуск и следите за результатом. Отправка пока работает в тестовом режиме.</p>
      {!list.length ? <div className="admin-empty"><h2>Пока нет рассылок</h2><p>Создайте первую: выберите ивент, проверьте получателей и напишите текст.</p></div>
        : <div className="admin-event-list">{list.map(m => <article className="admin-event-row" key={m.id}>
          <div className="admin-event-info"><span className={`draft-badge status-${m.status}`}>{mailingStatus[m.status]}</span><h2>{m.event_title}</h2><p className="mailing-preview">{m.body || "Текст ещё не добавлен"}</p><p>Изменён {updatedDate(m.updated_at)}{!m.event_available && " · Ивент удалён"}</p></div>
          <div className="event-row-actions"><a className="secondary-button" href={`/mailings/${m.id}/edit`}>{m.status === "draft" ? "Редактировать" : "Результаты"}</a></div>
        </article>)}</div>}
    </>}
  </section>;
}
