import { useEffect, useRef, useState } from "react";
import { api, AuthError } from "./admin-api";
import { mailingStatus, deliveryStatus } from "./mailing-types";
import type { MailingDetails } from "./mailing-types";

export default function MailingRun({ initial, onUnauthorized }: { initial: MailingDetails; onUnauthorized: () => void }) {
  const [value, setValue] = useState(initial);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const unauthorized = useRef(onUnauthorized); unauthorized.current = onUnauthorized;
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus({ preventScroll: true }); }, []);
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      let again = true;
      try {
        const latest = await api<MailingDetails>(`mailings/${initial.id}`);
        if (!active) return;
        setValue(latest); setError(""); again = latest.status === "running";
      } catch (err) {
        if (!active) return;
        if (err instanceof AuthError && err.status === 401) { unauthorized.current(); return; }
        setError("Не удалось обновить прогресс. Обработка продолжается на сервере; повторим запрос.");
      }
      if (active && again) timer = setTimeout(poll, 2000);
    }
    void poll();
    return () => { active = false; clearTimeout(timer); };
  }, [initial.id, refresh]);
  async function resume() {
    if (busy) return;
    setBusy(true); setError("");
    try {
      const latest = await api<MailingDetails>(`mailings/${value.id}/resume`, {});
      setValue(latest); setRefresh(n => n + 1);
    } catch (err) {
      if (err instanceof AuthError && err.status === 401) unauthorized.current();
      else setError((err as Error).message);
    } finally { setBusy(false); }
  }
  const p = value.progress;
  const finished = p ? p.sent + p.skipped + p.failed : 0;
  return <section className="mailings-workspace">
    <div className="events-heading"><div><p className="eyebrow">ПАНЕЛЬ ОРГАНИЗАТОРА</p><h1 ref={heading} tabIndex={-1}>Результаты рассылки</h1></div><a className="secondary-button" href="/mailings">← Все рассылки</a></div>
    <section className="mailing-progress-panel" aria-labelledby="run-title">
      <div className="mailing-section-heading"><h2 id="run-title">{value.event_title}</h2><span className={`draft-badge status-${value.status}`}>{mailingStatus[value.status]}</span></div>
      <p className="mailing-audience-help">Работает моковый отправитель. Реальные письма не отправляются. Страницу можно закрыть — обработка сохранится.</p>
      {p && <>
        <p className="progress-label" role="status">Обработано {finished} из {p.total} получателей</p>
        <progress className="mailing-progress" aria-label="Прогресс обработки" max={Math.max(1, p.total)} value={finished} />
        <dl className="mailing-metrics"><div><dt>Отправлено</dt><dd>{p.sent}</dd></div><div><dt>Пропущено</dt><dd>{p.skipped}</dd></div><div><dt>Ошибки</dt><dd>{p.failed}</dd></div><div><dt>Осталось</dt><dd>{p.pending + p.processing + p.retry_wait}</dd></div></dl>
        {p.failed > 0 && <div className="mailing-resume"><p>Можно продолжить обработку ошибочных адресов. Успешные отправки не повторятся.</p><button className="primary-button" disabled={busy} onClick={resume}>{busy ? "Продолжаем…" : "Продолжить обработку"}</button></div>}
      </>}
      {error && <div className="error-notice" role="alert"><p>{error}</p><button className="secondary-button" onClick={() => setRefresh(n => n + 1)}>Обновить статус</button></div>}
      <details className="mailing-frozen"><summary>Текст рассылки</summary><p>{value.body}</p></details>
    </section>
    <section className="mailing-results" aria-labelledby="delivery-heading"><h2 id="delivery-heading">Результат каждому получателю</h2>
      <ul className="mailing-deliveries">{value.deliveries?.map(d => <li key={d.id}>
        <div className="delivery-person"><strong>{d.names.join(", ")}</strong><span>{d.email}</span></div>
        <div className="delivery-result"><span className={`draft-badge delivery-${d.status}`}>{deliveryStatus[d.status]}</span><span className="field-hint">Попыток: {d.attempts}</span></div>
        {d.error && <p className="delivery-error">{d.error}</p>}
      </li>)}</ul>
    </section>
  </section>;
}
