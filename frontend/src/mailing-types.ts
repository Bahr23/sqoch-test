export type Recipient = { email: string; names: string[]; order_count: number };
export type MailingStatus = "draft" | "running" | "completed" | "completed_with_errors";
export type Mailing = {
  id: string; event_id: string; event_title: string; event_available: boolean;
  body: string; status: MailingStatus; created_at: string; updated_at: string; confirmed_at: string;
};
export type Delivery = {
  id: string; email: string; names: string[];
  status: "pending" | "processing" | "retry_wait" | "sent" | "skipped" | "failed";
  attempts: number; error: string; updated_at: string;
};
export type Progress = { total: number; pending: number; processing: number; retry_wait: number; sent: number; skipped: number; failed: number };
export type MailingDetails = Mailing & { recipients: Recipient[]; audience_hash: string; progress?: Progress; deliveries?: Delivery[] };
export const mailingStatus = { draft: "Черновик", running: "В обработке", completed: "Завершена", completed_with_errors: "Есть ошибки" };
export const deliveryStatus = { pending: "В очереди", processing: "Обрабатывается", retry_wait: "Ожидает повтора", sent: "Отправлено (мок)", skipped: "Пропущено", failed: "Ошибка" };
