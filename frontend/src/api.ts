import type { FieldErrors } from "./types";
export class ApiError extends Error {
  constructor(
    public code: string,
    message: string,
    public fields: FieldErrors = {},
  ) {
    super(message);
  }
}
export async function request<T>(path: string, body?: unknown): Promise<T> {
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 12000);
  try {
    const response = await fetch(path, {
      method: body === undefined ? "GET" : "POST",
      headers:
        body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: controller.signal,
      cache: "no-store",
    });
    const result = await response.json();
    if (!response.ok)
      throw new ApiError(
        result.code ?? "unavailable",
        result.message ?? "Сервис временно недоступен.",
        result.fields ?? {},
      );
    return result as T;
  } catch (error) {
    if (error instanceof ApiError) throw error;
    throw new ApiError(
      "network",
      "Не удалось получить ответ сервиса. Проверьте соединение и попробуйте снова.",
    );
  } finally {
    window.clearTimeout(timeout);
  }
}
