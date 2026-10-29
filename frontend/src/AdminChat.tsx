import { createContext, useContext, useEffect, useMemo, useRef, useState } from "react";
import { AssistantRuntimeProvider, makeAssistantToolUI, useExternalMessageConverter, useExternalStoreRuntime } from "@assistant-ui/react";
import type { AppendMessage, ThreadMessageLike, ToolCallMessagePartProps } from "@assistant-ui/react";
import { Thread as NativeThread } from "./components/assistant-ui/elements/thread.aui";
import { ThreadList } from "./components/assistant-ui/elements/thread-list.aui";
import { ToolFallback } from "./components/assistant-ui/elements/tool-fallback.aui";
import { Button } from "./components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "./components/ui/alert";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "./components/ui/card";
import NativeChatSurface from "./NativeChatSurface";
import { api, AuthError } from "./admin-api";

type EventInput = { title: string; description: string; category: string; tagline: string; cover_url: string; date: string; timezone: string; venue: string; full_price: number; child_price: number; capacity: number };
type Proposal = { id: string; thread_id: string; event_id: string; before: EventInput; after: EventInput; mailing_body: string; mailing_id: string; status: "pending" | "applied" | "rejected" };
type Message = { id: string; role: "user" | "assistant" | "tool"; content: string; tool_call_id?: string; tool_calls?: { id: string; function: { name: string; arguments: string } }[] };
type Run = { id: string; status: "queued" | "running" | "failed" | "completed"; error: string };
type ChatThread = { id: string; title: string; updated_at: string };
type Details = ChatThread & { messages: Message[]; proposals: Proposal[]; runs: Run[] };
type Config = { enabled: boolean; model: string };
const labels: Record<keyof EventInput, string> = { title: "Название", description: "Описание", category: "Категория", tagline: "Подзаголовок", cover_url: "Обложка", date: "Дата и время", timezone: "Часовой пояс", venue: "Место", full_price: "Полный билет, ₽", child_price: "Детский билет, ₽", capacity: "Вместимость" };
const ProposalContext = createContext<{ proposals: Proposal[]; locked: boolean; decide: (id: string, action: "apply" | "reject") => void }>({ proposals: [], locked: false, decide: () => {} });
const asMessage = (message: ThreadMessageLike) => message;
const suggestions = [
  { title: "Какие мероприятия запланированы?", prompt: "Какие мероприятия у нас запланированы?" },
  { title: "Подготовить уведомление покупателям", prompt: "Помоги подготовить уведомление покупателям мероприятия." },
];
function eventValue(key: keyof EventInput, event: EventInput) {
  if (key !== "date") return String(event[key]);
  return new Intl.DateTimeFormat("ru-RU", { dateStyle: "long", timeStyle: "short", timeZone: event.timezone }).format(new Date(event.date)) + ` · ${event.timezone}`;
}
function ProposalTool(props: ToolCallMessagePartProps) {
  const { proposals, locked, decide } = useContext(ProposalContext);
  const value = props.result as { id?: string } | undefined;
  const proposal = proposals.find(p => p.id === value?.id);
  if (!proposal) return <ToolFallback {...props} />;
  const changes = (Object.keys(labels) as (keyof EventInput)[]).filter(key => proposal.before[key] !== proposal.after[key]);
  return <Card className="my-3 min-w-0" aria-label="Предложение помощника">
    <CardHeader><CardDescription>{proposal.status === "pending" ? "На проверке" : proposal.status === "applied" ? "Применено" : "Отклонено"}</CardDescription><CardTitle>{proposal.after.title}</CardTitle></CardHeader>
    <CardContent className="space-y-4 overflow-hidden">
      {changes.length > 0 && <dl className="space-y-3 text-sm">{changes.map(key => <div key={key}><dt className="font-medium">{labels[key]}</dt><dd className="mt-1 space-y-1 whitespace-pre-wrap break-words"><p className="text-muted-foreground">Было: {eventValue(key, proposal.before)}</p><p>{proposal.status === "applied" ? "Стало" : "Будет"}: {eventValue(key, proposal.after)}</p></dd></div>)}</dl>}
      {proposal.mailing_body && <div className="space-y-2 text-sm"><h3 className="font-medium">Черновик уведомления</h3><p className="whitespace-pre-wrap break-words">{proposal.mailing_body}</p></div>}
      <p className="text-muted-foreground text-sm">{proposal.status === "pending" ? "Подтверждение сохранит предложенные изменения. Отправка писем запускается отдельно." : proposal.status === "applied" ? "Изменения сохранены. Отправка писем не запускалась." : "Изменения не применены."}</p>
    </CardContent>
    {proposal.status === "pending" ? <CardFooter className="flex-wrap gap-2"><Button disabled={locked} onClick={() => decide(proposal.id, "apply")}>{changes.length ? "Применить изменения" : "Создать черновик"}</Button><Button variant="outline" disabled={locked} onClick={() => decide(proposal.id, "reject")}>Отклонить</Button></CardFooter> : proposal.status === "applied" && proposal.mailing_id && <CardFooter><Button variant="outline" asChild><a href={`/mailings/${proposal.mailing_id}/edit`}>Открыть рассылку</a></Button></CardFooter>}
  </Card>;
}
const ProposalUI = makeAssistantToolUI({ toolName: "prepare_change", display: "standalone", render: ProposalTool });

type ConversationProps = {
  id: string; config: Config; threads: ChatThread[]; creating: boolean;
  onUnauthorized: () => void; onUpdate: (t: ChatThread) => void;
  onSelect: (id: string) => void; onCreate: () => Promise<void>;
};
function Conversation({ id, config, threads, creating, onUnauthorized, onUpdate, onSelect, onCreate }: ConversationProps) {
  const [data, setData] = useState<Details | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [sending, setSending] = useState(false);
  const [connectionError, setConnectionError] = useState("");
  const [unsent, setUnsent] = useState<{ id: string; message: string } | null>(null);
  const alive = useRef(true);
  const callbacks = useRef({ onUnauthorized, onUpdate }); callbacks.current = { onUnauthorized, onUpdate };
  const base = `assistant/threads/${id}`;
  function failure(err: unknown) { if (!alive.current) return; if (err instanceof AuthError && err.status === 401) callbacks.current.onUnauthorized(); else setError((err as Error).message); }
  async function refresh() {
    const d = await api<Details>(base);
    if (alive.current) { setData(d); setConnectionError(""); callbacks.current.onUpdate(d); }
    return d;
  }
  useEffect(() => {
    alive.current = true; let timer = 0; let stopped = false;
    async function poll() {
      try { await refresh(); } catch (err) { if (!stopped) { if (err instanceof AuthError && err.status === 401) callbacks.current.onUnauthorized(); else setConnectionError((err as Error).message); } }
      if (!stopped) timer = window.setTimeout(poll, 2000);
    }
    void poll();
    return () => { stopped = true; alive.current = false; window.clearTimeout(timer); };
  }, [id]);
  const active = data?.runs.find(r => r.status !== "completed");
  const running = active?.status === "queued" || active?.status === "running";
  async function send(payload: { id: string; message: string }) {
    setSending(true); setError(""); setUnsent(payload);
    try { await api(base + "/messages", payload); if (alive.current) setUnsent(null); await refresh(); }
    catch (err) { failure(err); }
    finally { if (alive.current) setSending(false); }
  }
  async function onNew(message: AppendMessage) {
    if (!config.enabled || active || sending) return;
    const content = message.content.filter(p => p.type === "text").map(p => p.text).join("\n");
    await send({ id: crypto.randomUUID(), message: content });
  }
  async function decide(proposal: string, action: "apply" | "reject") {
    setBusy(true); setError("");
    try { await api(`${base}/proposals/${proposal}/decision`, { action }); await refresh(); }
    catch (err) { failure(err); }
    finally { if (alive.current) setBusy(false); }
  }
  async function retry() {
    if (!active) return;
    setBusy(true); setError("");
    try { await api(`${base}/runs/${active.id}/retry`, {}); await refresh(); }
    catch (err) { failure(err); }
    finally { if (alive.current) setBusy(false); }
  }
  const messages = useMemo<ThreadMessageLike[]>(() => {
    if (!data) return [];
    const results = new Map<string, string>();
    let batchID = "";
    for (const message of data.messages) {
      if (message.role === "assistant") batchID = message.id;
      if (message.role === "tool") results.set(`${batchID}:${message.tool_call_id}`, message.content);
    }
    return data.messages.filter(m => m.role !== "tool" && !m.id.startsWith("decision:")).map(m => {
      const content: ThreadMessageLike["content"] = [ ...(m.content ? [{ type: "text" as const, text: m.content }] : []), ...(m.tool_calls ?? []).map(call => {
        let args = {}; let result: unknown;
        try { args = JSON.parse(call.function.arguments); } catch { /* Server displays invalid tool arguments as a failed call. */ }
        try { if (results.has(`${m.id}:${call.id}`)) result = JSON.parse(results.get(`${m.id}:${call.id}`)!); } catch { /* Keep the rest of the conversation readable. */ }
        return { type: "tool-call" as const, toolCallId: `${m.id}:${call.id}`, toolName: call.function.name, args, result, isError: !!(result && typeof result === "object" && "error" in result) };
      }) ];
      return { id: m.id, role: m.role as "user" | "assistant", content };
    });
  }, [data]);
  const joinedMessages = useExternalMessageConverter({ messages, callback: asMessage, isRunning: running || sending, joinStrategy: "concat-content" });
  const runtime = useExternalStoreRuntime({
    messages: joinedMessages, isRunning: running || sending, onNew, isLoading: !data,
    isDisabled: !config.enabled || !data, isSendDisabled: !!active || !!unsent || busy,
    suggestions: data?.messages.length ? [] : suggestions,
    adapters: { threadList: {
      threadId: id, threads: threads.map(t => ({ id: t.id, title: t.title, status: "regular" as const })),
      onSwitchToThread: async (next: string) => onSelect(next),
      onSwitchToNewThread: async () => { if (!creating) await onCreate(); },
    } },
  });
  return <AssistantRuntimeProvider runtime={runtime}><ProposalContext.Provider value={{ proposals: data?.proposals ?? [], locked: !!active || busy, decide }}>
    <ProposalUI />
    <div className="grid h-full min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)] md:grid-cols-[220px_minmax(0,1fr)] md:grid-rows-1">
      <aside className="min-w-0 overflow-y-auto border-b p-3 md:border-r md:border-b-0" aria-label="Диалоги"><div className="max-h-40 md:max-h-none"><ThreadList /></div></aside>
      <div className="flex min-h-0 min-w-0 flex-col">
        <div className="min-h-0 flex-1"><NativeThread autoFocus={false} /></div>
        {(active?.status === "failed" || error || connectionError || unsent) && <div className="space-y-2 px-4 pb-3">
          {active?.status === "failed" && <Alert variant="destructive"><AlertTitle>Ответ не завершён</AlertTitle><AlertDescription><p>{active.error}</p><Button variant="outline" onClick={retry} disabled={busy || !config.enabled}>Продолжить обработку</Button></AlertDescription></Alert>}
          {(error || connectionError) && <Alert variant="destructive"><AlertDescription><p>{error || connectionError}</p><Button variant="outline" onClick={() => { setError(""); void refresh().catch(failure); }}>Обновить диалог</Button></AlertDescription></Alert>}
          {unsent && !sending && <Alert><AlertTitle>Сообщение не подтверждено сервером</AlertTitle><AlertDescription><p className="break-words">{unsent.message}</p><div className="flex flex-wrap gap-2"><Button variant="outline" onClick={() => void send(unsent)}>Повторить отправку</Button><Button variant="ghost" onClick={() => setUnsent(null)}>Убрать</Button></div></AlertDescription></Alert>}
        </div>}
        <p className="text-muted-foreground px-4 pb-3 text-center text-xs">Проверяйте изменения перед подтверждением. Рассылка запускается отдельно.</p>
      </div>
    </div>
  </ProposalContext.Provider></AssistantRuntimeProvider>;
}

export default function AdminChat({ onUnauthorized }: { onUnauthorized: () => void }) {
  const [config, setConfig] = useState<Config | null>(null);
  const [threads, setThreads] = useState<ChatThread[]>([]);
  const [selected, setSelected] = useState(new URLSearchParams(location.search).get("thread") ?? "");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const createID = useRef("");
  const unauthorized = useRef(onUnauthorized); unauthorized.current = onUnauthorized;
  function fail(err: unknown) { if (err instanceof AuthError && err.status === 401) unauthorized.current(); else setError((err as Error).message); }
  function select(id: string) { setSelected(id); history.replaceState(null, "", `/chat?thread=${encodeURIComponent(id)}`); }
  async function create() {
    if (creating) return;
    setCreating(true); setError(""); createID.current ||= crypto.randomUUID();
    try { const t = await api<ChatThread>("assistant/threads", { id: createID.current }); createID.current = ""; setThreads(prev => [t, ...prev.filter(x => x.id !== t.id)]); select(t.id); }
    catch (err) { fail(err); } finally { setCreating(false); }
  }
  useEffect(() => {
    let alive = true;
    Promise.all([api<Config>("assistant/config"), api<ChatThread[]>("assistant/threads")]).then(([c, t]) => { if (!alive) return; setConfig(c); setThreads(t); if (!new URLSearchParams(location.search).get("thread") && t[0]) select(t[0].id); }).catch(err => { if (alive) fail(err); }).finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, []);
  return <section aria-label="Помощник">
    <NativeChatSurface><div className="bg-background text-foreground flex h-full min-h-0 min-w-0 flex-col overflow-hidden rounded-lg border font-sans antialiased">
      {config && !config.enabled && <div className="p-4"><Alert><AlertTitle>Подключите модель, чтобы начать</AlertTitle><AlertDescription>Укажите OPENROUTER_API_KEY и OPENROUTER_MODEL в корневом .env, затем выполните docker compose up -d --build backend.</AlertDescription></Alert></div>}
      {error && <div className="p-4"><Alert variant="destructive"><AlertDescription>{error}<Button variant="outline" onClick={() => location.reload()}>Обновить</Button></AlertDescription></Alert></div>}
      {loading ? <div className="p-6" role="status">Загружаем помощника…</div> : config && <div className="min-h-0 flex-1">{selected ? <Conversation key={selected} id={selected} config={config} threads={threads} creating={creating} onCreate={create} onSelect={select} onUnauthorized={onUnauthorized} onUpdate={t => setThreads(prev => prev.map(x => x.id === t.id ? t : x))} /> : <div className="flex h-full flex-col items-center justify-center gap-4 p-6"><h2 className="text-xl font-medium">Чем помочь с мероприятиями?</h2><Button disabled={creating} onClick={create}>{creating ? "Создаём…" : "Начать диалог"}</Button></div>}</div>}
    </div></NativeChatSurface>
  </section>;
}
