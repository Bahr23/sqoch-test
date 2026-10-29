package domain

import "sqoch-ticket/backend/internal/dispatch"

type Job struct {
	Command                          dispatch.Command
	Status                           string
	Version, Attempts, CycleAttempts int
	LeaseToken                       string
	Error                            string
	UpdatedAt                        string
}
