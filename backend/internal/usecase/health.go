package usecase

import "context"

type DatabaseChecker interface {
	PingContext(context.Context) error
}

type Health struct{ database DatabaseChecker }

func NewHealth(database DatabaseChecker) *Health { return &Health{database: database} }

func (h *Health) Check(ctx context.Context) error { return h.database.PingContext(ctx) }
