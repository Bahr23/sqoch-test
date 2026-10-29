package usecase

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sqoch-ticket/backend/internal/dispatch"
	"sqoch-ticket/backend/internal/notifications/domain"
	"time"
)

type Repository interface {
	Claim(context.Context, time.Time, time.Duration) (domain.Job, error)
	Finish(context.Context, domain.Job, string, string, time.Time) error
}
type Sender interface {
	Lookup(context.Context, dispatch.Command) (bool, error)
	Send(context.Context, dispatch.Command) error
}
type Eligibility interface {
	Eligible(context.Context, string) (bool, error)
}
type Worker struct {
	Repository                          Repository
	Sender                              Sender
	Eligibility                         Eligibility
	Lease, Timeout, RetryBase, Interval time.Duration
	MaxAttempts                         int
}

func (w *Worker) Step(ctx context.Context, now time.Time) (bool, error) {
	j, err := w.Repository.Claim(ctx, now, w.Lease)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, w.Timeout)
	accepted, err := w.Sender.Lookup(requestCtx, j.Command)
	cancel()
	if err != nil {
		return true, w.retry(ctx, j, now, "Не удалось проверить результат предыдущей попытки.")
	}
	if accepted {
		return true, w.Repository.Finish(ctx, j, "sent", "", now)
	}
	requestCtx, cancel = context.WithTimeout(ctx, w.Timeout)
	eligible, err := w.Eligibility.Eligible(requestCtx, j.Command.ID)
	cancel()
	if err != nil {
		return true, w.retry(ctx, j, now, "Не удалось проверить действующую покупку. Отправка отложена.")
	}
	if !eligible {
		return true, w.Repository.Finish(ctx, j, "skipped", "Нет действующей покупки или ивент недоступен.", now)
	}
	if j.CycleAttempts > w.MaxAttempts {
		return true, w.Repository.Finish(ctx, j, "failed", "Попытки прерваны сбоями. Можно продолжить обработку.", now)
	}
	requestCtx, cancel = context.WithTimeout(ctx, w.Timeout)
	err = w.Sender.Send(requestCtx, j.Command)
	cancel()
	if err != nil {
		return true, w.retry(ctx, j, now, "Отправитель не подтвердил результат. Проверим его при повторной попытке.")
	}
	return true, w.Repository.Finish(ctx, j, "sent", "", now)
}
func (w *Worker) retry(ctx context.Context, j domain.Job, now time.Time, message string) error {
	status := "retry_wait"
	if j.CycleAttempts >= w.MaxAttempts {
		status = "failed"
	}
	delay := min(w.RetryBase*time.Duration(1<<min(j.CycleAttempts-1, 6)), time.Hour)
	return w.Repository.Finish(ctx, j, status, message, now.Add(delay))
}
func (w *Worker) Run(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		processed, err := w.Step(ctx, time.Now())
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Error("notification processing failed; durable job will be recovered")
		}
		if processed && err == nil {
			continue
		}
		t := time.NewTimer(w.Interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
