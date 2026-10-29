package rabbit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	amqp "github.com/rabbitmq/amqp091-go"
	"log/slog"
	"sqoch-ticket/backend/internal/dispatch"
	"sqoch-ticket/backend/internal/infrastructure/outbox"
	"time"
)

type Config struct {
	URL               string
	Commands, Results string
	Interval          time.Duration
}

func Wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
func connect(c Config) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.DialConfig(c.URL, amqp.Config{Heartbeat: 10 * time.Second, Dial: amqp.DefaultDial(5 * time.Second)})
	if err != nil {
		return nil, nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	for _, name := range []string{c.Commands, c.Results} {
		if _, err = ch.QueueDeclare(name+".dead", true, false, false, false, nil); err != nil {
			conn.Close()
			return nil, nil, err
		}
		args := amqp.Table{"x-queue-type": "quorum", "x-dead-letter-exchange": "", "x-dead-letter-routing-key": name + ".dead", "x-delivery-limit": int32(-1), "x-overflow": "reject-publish"}
		if _, err = ch.QueueDeclare(name, true, false, false, false, args); err != nil {
			conn.Close()
			return nil, nil, err
		}
	}
	return conn, ch, nil
}
func RunPublisher(ctx context.Context, c Config, store outbox.Store, logger *slog.Logger) {
	for ctx.Err() == nil {
		err := publishSession(ctx, c, store)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("outbox publisher disconnected; messages remain in database")
		}
		if !Wait(ctx, 2*time.Second) {
			return
		}
	}
}
func publishSession(ctx context.Context, c Config, store outbox.Store) error {
	conn, ch, err := connect(c)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err = ch.Confirm(false); err != nil {
		return err
	}
	returns := ch.NotifyReturn(make(chan amqp.Return, 1))
	for ctx.Err() == nil {
		m, err := store.Next(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			if !Wait(ctx, c.Interval) {
				return ctx.Err()
			}
			continue
		}
		if err != nil {
			return err
		}
		publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		confirmation, err := ch.PublishWithDeferredConfirmWithContext(publishCtx, "", m.Queue, true, false, amqp.Publishing{DeliveryMode: amqp.Persistent, ContentType: "application/json", MessageId: m.ID, Body: m.Payload})
		if err == nil {
			var ack bool
			ack, err = confirmation.WaitContext(publishCtx)
			if err == nil && !ack {
				err = fmt.Errorf("broker rejected publication")
			}
		}
		cancel()
		if err != nil {
			return err
		}
		select {
		case <-returns:
			return fmt.Errorf("unroutable publication")
		default:
		}
		if err = store.Published(ctx, m.ID); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func RunConsumer(ctx context.Context, c Config, queue string, handle func(context.Context, []byte) error, logger *slog.Logger) {
	for ctx.Err() == nil {
		err := consumeSession(ctx, c, queue, handle, logger)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("queue consumer disconnected; unacknowledged messages will return", "queue", queue)
		}
		if !Wait(ctx, 2*time.Second) {
			return
		}
	}
}
func consumeSession(ctx context.Context, c Config, queue string, handle func(context.Context, []byte) error, logger *slog.Logger) error {
	conn, ch, err := connect(c)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err = ch.Qos(20, 0, false); err != nil {
		return err
	}
	messages, err := ch.ConsumeWithContext(ctx, queue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for m := range messages {
		if len(m.Body) > 128*1024 {
			err = dispatch.ErrPoison
		} else {
			err = handle(ctx, m.Body)
		}
		if errors.Is(err, dispatch.ErrPoison) {
			logger.Error("invalid message moved to dead-letter queue", "queue", queue, "message_id", m.MessageId)
			if err = m.Nack(false, false); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		} // Closing the channel returns the unacknowledged message.
		if err = m.Ack(false); err != nil {
			return err
		}
	}
	return fmt.Errorf("consumer channel closed")
}
