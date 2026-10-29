import { useEffect, useRef } from "react";
import type { MailingDetails } from "./mailing-types";

export default function MailingConfirmation({ value, busy, error, onClose, onConfirm }: {
  value: MailingDetails; busy: boolean; error: string; onClose: () => void; onConfirm: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => { const el = dialog.current!; el.showModal(); return () => el.close(); }, []);
  return <dialog ref={dialog} className="mailing-confirm-dialog" aria-labelledby="confirm-mailing-title" onCancel={e => { e.preventDefault(); if (!busy) onClose(); }}>
    <h2 id="confirm-mailing-title">Подтвердить запуск рассылки?</h2>
    <p><strong>{value.event_title}</strong></p>
    <p>Получателей: <strong>{value.recipients.length}</strong>. Перед отправкой ещё раз проверим действующие покупки.</p>
    <div className="mailing-confirm-text">{value.body}</div>
    <p className="field-hint">После подтверждения текст и исходный список адресатов нельзя изменить. Сейчас отправка выполняется только моком.</p>
    {error && <p className="error-notice" role="alert">{error}</p>}
    <div className="editor-actions"><button className="secondary-button" autoFocus disabled={busy} onClick={onClose}>Отмена</button><button className="primary-button" disabled={busy} onClick={onConfirm}>{busy ? "Запускаем…" : "Подтвердить запуск"}</button></div>
  </dialog>;
}
