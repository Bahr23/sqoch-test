import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { api, AuthError } from "./admin-api";
import { eventDate, money } from "./types";
import type { Event } from "./types";

type Input = Pick<Event,"title"|"description"|"category"|"tagline"|"cover_url"|"date"|"timezone"|"venue"|"full_price"|"child_price"|"capacity">;
type Draft = Omit<Input,"full_price"|"child_price"|"capacity"> & {full_price:string;child_price:string;capacity:string};
const blank: Draft = {title:"",description:"",category:"",tagline:"",cover_url:"",date:"",timezone:"Europe/Moscow",venue:"",full_price:"",child_price:"",capacity:""};
function localDate(value:string) { const d=new Date(value); return new Date(d.getTime()-d.getTimezoneOffset()*60000).toISOString().slice(0,16); }
function draftOf(e:Event):Draft { return {title:e.title,description:e.description,category:e.category,tagline:e.tagline,cover_url:e.cover_url,date:localDate(e.date),timezone:e.timezone,venue:e.venue,full_price:String(e.full_price),child_price:String(e.child_price),capacity:String(e.capacity)}; }
function eventLink(id:string) { const origin=new URL(window.location.origin);origin.hostname=origin.hostname.replace(/^admin\./,"");return new URL(`/event/${id}`,origin).href; }

export default function AdminEvents({onUnauthorized}:{onUnauthorized:()=>void}) {
 const match=window.location.pathname.match(/^\/events\/([^/]+)\/edit\/?$/);
 const [editing]=useState(window.location.pathname==="/events/new" || !!match);
 const [id,setId]=useState(match?.[1] ?? "");
 const [list,setList]=useState<Event[]>([]);
 const [event,setEvent]=useState<Event|null>(null);
 const [draft,setDraft]=useState<Draft>(blank);
 const [loading,setLoading]=useState(true);
 const [attempt,setAttempt]=useState(0);
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState("");
 const [fields,setFields]=useState<Record<string,string>>({});
 const [message,setMessage]=useState("");
 const [remove,setRemove]=useState<Event|null>(null);
 const dirty=useRef(false);
 const heading=useRef<HTMLHeadingElement>(null);
 const unauthorized=useRef(onUnauthorized); unauthorized.current=onUnauthorized;
 const handleError=(err:unknown)=>{if(err instanceof AuthError && err.status===401)unauthorized.current();else setError((err as Error).message)};
 useEffect(()=>{let active=true;setLoading(true);setError("");
  const load=async()=>{
   try {
    if(editing && id){const value=await api<Event>(`events/${id}`);if(active){setEvent(value);setDraft(draftOf(value));}}
    else if(!editing){const values=await api<Event[]>("events");if(active)setList(values);}
   }catch(err){if(active)handleError(err)}finally{if(active)setLoading(false)}
  };void load();return()=>{active=false};
 },[attempt]);
 useEffect(()=>{if(!loading)heading.current?.focus({preventScroll:true})},[loading]);
 useEffect(()=>{const warn=(e:BeforeUnloadEvent)=>{if(dirty.current){e.preventDefault();e.returnValue=""}};window.addEventListener("beforeunload",warn);return()=>window.removeEventListener("beforeunload",warn)},[]);
 function change(key:keyof Draft,value:string){dirty.current=true;setDraft(d=>({...d,[key]:value}));setFields(f=>({...f,[key]:""}));setMessage("");setError("")}
 async function save(e:FormEvent){
  e.preventDefault();if(busy)return;setBusy(true);setError("");setFields({});setMessage("");
  try {
   const input:Input={...draft,date:new Date(draft.date).toISOString(),full_price:Number(draft.full_price),child_price:Number(draft.child_price),capacity:Number(draft.capacity)};
   const value=await api<Event>(id?`events/${id}`:"events",input,id?"PUT":"POST");
   dirty.current=false;setEvent(value);setDraft(draftOf(value));setId(value.id);setMessage("Изменения сохранены. Страница мероприятия обновлена.");
   window.history.replaceState(null,"",`/events/${value.id}/edit`);
  }catch(err){handleError(err);if(err instanceof AuthError){setFields(err.fields);const first=Object.keys(err.fields)[0];requestAnimationFrame(()=>document.getElementById(`event-${first}`)?.focus())}}
  finally{setBusy(false)}
 }
 async function deleteEvent(){if(!remove||busy)return;setBusy(true);setError("");try{await api<void>(`events/${remove.id}`,{},"DELETE");setList(values=>values.filter(e=>e.id!==remove.id));setRemove(null);setMessage("Мероприятие удалено из каталога. История заказов сохранена.")}catch(err){handleError(err)}finally{setBusy(false)}}
 function field(key:keyof Draft,label:string,options:{type?:string;required?:boolean;maxLength?:number;help?:string}={}){
  const numeric=["full_price","child_price","capacity"].includes(key);
  return <div className="field" key={key}><label htmlFor={`event-${key}`}>{label}</label><input id={`event-${key}`} value={draft[key]} type={options.type??"text"} required={options.required??true} maxLength={options.maxLength} min={numeric?1:undefined} max={numeric?1000000:undefined} step={numeric?1:undefined} inputMode={numeric?"numeric":undefined} onChange={e=>change(key,e.target.value)} aria-invalid={!!fields[key]} aria-describedby={fields[key]?`error-${key}`:options.help?`help-${key}`:undefined}/>{options.help&&<p className="field-hint" id={`help-${key}`}>{options.help}</p>}{fields[key]&&<p className="field-error" id={`error-${key}`}>{fields[key]}</p>}</div>
 }
 if(loading)return <p role="status">Загружаем мероприятия…</p>;
 if(error && editing && id && !event)return <section className="admin-card admin-placeholder"><h1>Не удалось загрузить ивент</h1><p role="alert">{error}</p><button className="secondary-button" onClick={()=>setAttempt(n=>n+1)}>Попробовать снова</button><a className="catalog-back" href="/events">← Все ивенты</a></section>;
 return <section className="events-workspace">
  <div className="events-heading"><div><p className="eyebrow">ПАНЕЛЬ ОРГАНИЗАТОРА</p><h1 ref={heading} tabIndex={-1}>{editing ? id ? "Редактирование ивента" : "Новый ивент" : "Ивенты"}</h1></div>{editing?<a className="secondary-button" href="/events">← Все ивенты</a>:<a className="primary-button" href="/events/new">Создать ивент</a>}</div>
  {message&&<p className="admin-message" role="status">{message}</p>}
  {error&&<div className="error-notice" role="alert"><p>{error}</p>{!editing&&<button className="secondary-button" onClick={()=>setAttempt(n=>n+1)}>Обновить список</button>}</div>}
  {editing?<form className="event-editor" onSubmit={save} aria-busy={busy}>
   <fieldset disabled={busy} className="form-fields"><legend className="sr-only">Данные мероприятия</legend>
    <div className="editor-section"><h2>О мероприятии</h2>{field("title","Название",{maxLength:160})}
     <div className="field"><label htmlFor="event-description">Описание</label><textarea id="event-description" rows={5} required maxLength={10000} value={draft.description} onChange={e=>change("description",e.target.value)} aria-invalid={!!fields.description} aria-describedby={fields.description?"error-description":undefined}/>{fields.description&&<p id="error-description" className="field-error">{fields.description}</p>}</div>
     <div className="editor-grid">{field("category","Категория",{maxLength:100})}{field("tagline","Подпись на обложке",{required:false,maxLength:120})}</div>
     {field("cover_url","Ссылка на обложку",{type:"url",required:false,maxLength:2048,help:"HTTPS-адрес изображения. Если оставить пустым, покажем фирменную графику."})}
    </div>
    <div className="editor-section"><h2>Где и когда</h2>{field("venue","Площадка и адрес",{maxLength:300})}<div className="editor-grid">{field("date","Дата и время",{type:"datetime-local",help:"В часовом поясе вашего устройства. На странице событие показывается в выбранном ниже поясе."})}{field("timezone","Часовой пояс мероприятия",{maxLength:80,help:"Формат IANA, например Europe/Moscow или Asia/Ho_Chi_Minh."})}</div></div>
    <div className="editor-section"><h2>Билеты и места</h2><div className="editor-grid">{field("full_price","Полный билет, ₽",{type:"number",help:"Для участников от 14 лет."})}{field("child_price","Детский билет, ₽",{type:"number",help:"Для участников младше 14 лет."})}</div>{field("capacity","Общая вместимость",{type:"number",help:event?`Продано: ${event.sold}. Свободно: ${event.remaining}. Вместимость не может быть меньше проданных билетов.`:"От 1 до 1 000 000 мест."})}<p className="field-hint">Новые цены действуют для новых оформлений. Сумма уже проверенного заказа сохраняется до истечения его срока.</p></div>
   </fieldset>
   <div className="editor-actions"><button className="primary-button" disabled={busy}>{busy?"Сохраняем…":"Сохранить ивент"}</button>{id&&<a className="secondary-button" href={eventLink(id)} target="_blank" rel="noreferrer">Открыть страницу ↗</a>}</div>
  </form>:<>
   {remove&&<section className="delete-confirm" aria-label="Подтверждение удаления"><h2>Удалить «{remove.title}»?</h2><p>Страница и новые продажи станут недоступны. История заказов сохранится.</p><div className="editor-actions"><button className="secondary-button danger-button" disabled={busy} onClick={deleteEvent}>{busy?"Удаляем…":"Удалить мероприятие"}</button><button className="secondary-button" disabled={busy} onClick={()=>setRemove(null)}>Отмена</button></div></section>}
   {!list.length&&!error?<div className="admin-empty"><h2>Пока нет ивентов</h2><p>Создайте первое мероприятие, чтобы открыть продажу билетов.</p></div>:<div className="admin-event-list">{list.map(e=><article className="admin-event-row" key={e.id}><div className="admin-event-info"><p className="eyebrow">{e.category}</p><h2><a href={`/events/${e.id}/edit`}>{e.title}</a></h2><p>{eventDate(e.date,e.timezone)}</p><p>{e.venue}</p><div className="event-metrics"><span>Продано <strong>{e.sold}/{e.capacity}</strong></span><span>Свободно <strong>{e.remaining}</strong></span><span>Билеты от <strong>{money(Math.min(e.full_price,e.child_price))}</strong></span></div></div><div className="event-row-actions"><a className="secondary-button" href={`/events/${e.id}/edit`}>Редактировать</a><a className="text-button" href={eventLink(e.id)} target="_blank" rel="noreferrer">Страница ивента ↗</a><button className="text-button danger-button" disabled={busy} onClick={()=>{setRemove(e);setMessage("");window.scrollTo({top:0,behavior:"instant"})}}>Удалить</button></div></article>)}</div>}
  </>}
 </section>;
}
