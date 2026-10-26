import { useEffect, useState } from "react";
export function Header() {
  const [theme, setTheme] = useState<"dark" | "light">(() => {
    try {
      return localStorage.getItem("sqoch-theme") === "light" ? "light" : "dark";
    } catch {
      return "dark";
    }
  });
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute("content", theme === "dark" ? "#0d0915" : "#fbfaff");
    try {
      localStorage.setItem("sqoch-theme", theme);
    } catch {
      /* Theme remains usable without storage. */
    }
  }, [theme]);
  return (
    <header className="site-header">
      <a className="brand" href="/" aria-label="Sqoch Ticket — главная">
        <img
          className="brand-icon"
          src="/sqoch-logo.svg"
          alt=""
          width="34"
          height="34"
        />
        Sqoch<span className="brand-light">ticket</span>
      </a>
      <div className="header-actions">
        <span className="tag">
          <span className="dot" />
          Демо-мероприятие
        </span>
        <button
          className="theme-toggle"
          aria-label={
            theme === "dark" ? "Включить светлую тему" : "Включить тёмную тему"
          }
          onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
        >
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            aria-hidden="true"
          >
            {theme === "dark" ? (
              <>
                <circle cx="12" cy="12" r="4" />
                <path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5" />
              </>
            ) : (
              <path d="M20.5 14A8.5 8.5 0 0 1 10 3.5 8.5 8.5 0 1 0 20.5 14Z" />
            )}
          </svg>
        </button>
      </div>
    </header>
  );
}
