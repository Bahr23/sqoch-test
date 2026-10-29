import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import Catalog from "./Catalog";
import "./styles.css";
import "./booking.css";

const eventMatch = window.location.pathname.match(/^\/event\/([0-9a-f-]{36})\/?$/);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <>{eventMatch ? <App eventID={eventMatch[1]} /> : window.location.pathname === "/" ? <Catalog /> : <main className="loading-panel"><h1>Страница не найдена</h1><a href="/">К мероприятиям</a></main>}</>
  </StrictMode>,
);
