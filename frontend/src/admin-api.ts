export class AuthError extends Error {
  constructor(public status: number, message: string, public fields: Record<string,string> = {}) { super(message); }
}
export async function api<T>(path: string, body?: unknown, method?: string): Promise<T> {
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 10000);
  try {
    const response = await fetch(`/api/admin/${path}`, {
      method: method ?? (body === undefined ? "GET" : "POST"),
      credentials: "same-origin",
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: controller.signal,
      cache: "no-store",
    });
    if (response.status === 204) return undefined as T;
    const result = await response.json().catch(() => null);
    if (!response.ok) throw new AuthError(response.status,
      response.status === 429 ? "Слишком много попыток входа. Подождите минуту и попробуйте снова." :
      result?.message ?? "Сервис временно недоступен. Попробуйте снова.", result?.fields ?? {});
    return result as T;
  } catch (error) {
    if (error instanceof AuthError) throw error;
    throw new Error("Не удалось связаться с сервером. Проверьте соединение и попробуйте снова.");
  } finally { window.clearTimeout(timeout); }
}

