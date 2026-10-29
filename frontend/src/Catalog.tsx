import { useEffect, useState } from "react";
import { Header } from "./components/Header";
import { request } from "./api";
import { eventDate, money } from "./types";
import type { Event } from "./types";
import { EventArt } from "./components/OrderSummary";

export default function Catalog() {
  const [events,setEvents]=useState<Event[]|null>(null);
  const [error,setError]=useState("");
  const [attempt,setAttempt]=useState(0);
  useEffect(()=>{let active=true;setError("");request<Event[]>("/api/events").then(v=>{if(active)setEvents(v)}).catch(e=>{if(active)setError(e.message)});return()=>{active=false}},[attempt]);
  return <div className="shell"><Header/><main className="booking-main"><div className="booking-heading"><p className="breadcrumb">Sqoch Live</p><h1>Выберите событие</h1><p>Впечатления начинаются здесь.</p></div>
    {error ? <div role="alert" className="booking-panel"><p>{error}</p><button className="secondary-button" onClick={()=>setAttempt(n=>n+1)}>Попробовать снова</button></div> : events===null ? <p role="status">Загружаем мероприятия…</p> : events.length===0 ? <p>Новых событий пока нет.</p> : <div className="event-catalog">{events.map(event=><a className="catalog-card" key={event.id} href={`/event/${event.id}`}><EventArt event={event}/><div className="summary-content"><p className="eyebrow">{event.category}</p><h2>{event.title}</h2><p className="event-meta">{eventDate(event.date,event.timezone)}</p><p className="event-meta">{event.venue}</p><p className="catalog-description">{event.description}</p><div className="catalog-bottom"><strong>От {money(Math.min(event.full_price,event.child_price))}</strong><span>{event.remaining ? "Выбрать билеты →" : "Нет мест"}</span></div></div></a>)}</div>}
  </main><footer><span>Sqoch Ticket</span><span>Впечатления начинаются здесь</span></footer></div>;
}
