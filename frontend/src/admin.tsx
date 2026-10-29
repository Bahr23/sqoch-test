import { StrictMode, Suspense, lazy, useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { createRoot } from "react-dom/client";
import { api, AuthError } from "./admin-api";
import AdminEvents from "./AdminEvents";
import AdminMailings from "./AdminMailings";
const AdminChat = lazy(() => import("./AdminChat"));
import { Header } from "./components/Header";
import "./styles.css";
import "./booking.css";
import "./admin.css";

type Session = { email: string; expires_at: string };

function Admin() {
  const [session, setSession] = useState<Session | null>(null);
  const [checking, setChecking] = useState(true);
  const [checkError, setCheckError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [visible, setVisible] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  const emailInput = useRef<HTMLInputElement>(null);
  const passwordInput = useRef<HTMLInputElement>(null);
  const emailInvalid = !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());

  useEffect(() => {
    let active = true;
    setChecking(true); setCheckError("");
    api<Session>("me").then((value) => { if (active) setSession(value); })
      .catch((err: Error) => {
        if (!active) return;
        setSession(null);
        if (!(err instanceof AuthError && err.status === 401)) setCheckError(err.message);
      }).finally(() => { if (active) setChecking(false); });
    return () => { active = false; };
  }, [attempt]);

  useEffect(() => {
    if (!checking) heading.current?.focus({ preventScroll: true });
  }, [checking, !!session]);

  useEffect(() => {
    if (!session) return;
    let active = true;
    const expire = () => {
      setSession(null); setPassword(""); setError("");
      setMessage("Сессия завершена. Войдите снова, чтобы продолжить.");
    };
    const timer = window.setTimeout(expire, Math.max(0, Date.parse(session.expires_at) - Date.now()));
    const recheck = () => {
      api<Session>("me").catch((err: Error) => {
        if (!active) return;
        if (err instanceof AuthError && err.status === 401) expire();
        else setError(err.message);
      });
    };
    window.addEventListener("focus", recheck);
    return () => { active = false; clearTimeout(timer); window.removeEventListener("focus", recheck); };
  }, [session]);

  async function login(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setSubmitted(true); setError(""); setMessage("");
    if (emailInvalid || !password) {
      (emailInvalid ? emailInput : passwordInput).current?.focus();
      return;
    }
    setBusy(true);
    try {
      const value = await api<Session>("login", { email: email.trim(), password });
      setSession(value); setPassword(""); setVisible(false); setSubmitted(false);
    } catch (err) {
      setError((err as Error).message);
    } finally { setBusy(false); }
  }
  async function logout() {
    if (busy) return;
    setBusy(true); setError("");
    try {
      await api<void>("logout", {});
      setSession(null); setPassword(""); setMessage("Вы вышли из панели организатора.");
    } catch (err) { setError((err as Error).message); }
    finally { setBusy(false); }
  }

  return <div className="shell admin-shell">
    <a className="skip-link" href="#admin-main">Перейти к содержимому</a>
    <Header label="Для организаторов" navigation={session && (
      <nav className="admin-nav" aria-label="Разделы панели">
        <a href="/events" aria-current={!window.location.pathname.startsWith("/mailings") && !window.location.pathname.startsWith("/chat") ? "page" : undefined}>Ивенты</a>
        <a href="/mailings" aria-current={window.location.pathname.startsWith("/mailings") ? "page" : undefined}>Рассылки</a>
        <a href="/chat" aria-current={window.location.pathname.startsWith("/chat") ? "page" : undefined}>Помощник</a>
      </nav>
    )} />
    <main id="admin-main" className={`admin-main${session ? " has-workspace" : ""}`} tabIndex={-1}>
      {checking ? <section className="admin-card" role="status"><p className="eyebrow">SQOCH TICKET · ОРГАНИЗАТОРАМ</p><h1>Проверяем вход…</h1><p className="admin-description">Подождите, загружаем вашу сессию.</p></section>
      : checkError ? <section className="admin-card"><h1 ref={heading} tabIndex={-1}>Не удалось проверить вход</h1><p className="error-notice" role="alert">{checkError}</p><button className="primary-button" onClick={() => setAttempt(n => n + 1)}>Попробовать снова</button></section>
      : session ? <div className="admin-workspace">
        <div className="admin-account"><span>{session.email}</span><button className="secondary-button" onClick={logout} disabled={busy}>{busy ? "Выходим…" : "Выйти"}</button></div>
        {error && <p className="error-notice" role="alert">{error}</p>}
        {window.location.pathname.startsWith("/chat") ? <Suspense fallback={<p role="status">Загружаем помощника…</p>}><AdminChat onUnauthorized={()=>{setSession(null);setMessage("Сессия завершена. Войдите снова.")}}/></Suspense> : window.location.pathname.startsWith("/mailings") ? <AdminMailings onUnauthorized={()=>{setSession(null);setMessage("Сессия завершена. Войдите снова.")}}/> : <AdminEvents onUnauthorized={()=>{setSession(null);setMessage("Сессия завершена. Войдите снова.")}}/>}
      </div> : <section className="admin-card">
        <p className="eyebrow">SQOCH TICKET · ОРГАНИЗАТОРАМ</p>
        <h1 ref={heading} tabIndex={-1}>Вход в панель</h1>
        <p className="admin-description">Войдите с учётной записью организатора.</p>
        {message && <p className="admin-message" role="status">{message}</p>}
        <form onSubmit={login} noValidate aria-busy={busy}>
          <fieldset className="form-fields" disabled={busy}><legend className="sr-only">Данные для входа</legend>
            <div className="field"><label htmlFor="admin-email">Email</label><input ref={emailInput} id="admin-email" type="email" autoComplete="username" inputMode="email" autoCapitalize="none" spellCheck={false} required maxLength={254} value={email} placeholder="name@example.ru" onChange={e=>{setEmail(e.target.value);setError("");}} aria-invalid={submitted && emailInvalid} aria-describedby={submitted && emailInvalid ? "admin-email-error" : undefined}/>{submitted && emailInvalid && <p id="admin-email-error" className="field-error">Укажите email в формате name@example.ru.</p>}</div>
            <div className="field"><label htmlFor="admin-password">Пароль</label><div className="admin-password"><input ref={passwordInput} id="admin-password" type={visible ? "text" : "password"} autoComplete="current-password" required value={password} onChange={e=>{setPassword(e.target.value);setError("");}} aria-invalid={submitted && !password} aria-describedby={submitted && !password ? "admin-password-error" : undefined}/><button className="admin-show" type="button" aria-label={visible ? "Скрыть пароль" : "Показать пароль"} aria-pressed={visible} onClick={()=>setVisible(!visible)}>{visible ? "Скрыть" : "Показать"}</button></div>{submitted && !password && <p id="admin-password-error" className="field-error">Введите пароль.</p>}</div>
          </fieldset>
          {error && <p className="error-notice" role="alert">{error}</p>}
          <button className="primary-button" disabled={busy} type="submit">{busy ? "Входим…" : "Войти"}</button>
        </form>
        <p className="admin-help">Доступ выдаёт администратор сервиса.</p>
      </section>}
    </main>
    <footer><span>Sqoch Ticket</span><span>Панель организатора</span></footer>
  </div>;
}

createRoot(document.getElementById("root")!).render(<StrictMode><Admin /></StrictMode>);
