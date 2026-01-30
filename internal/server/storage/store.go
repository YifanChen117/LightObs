package storage

import (
	"context"

	"lightobs/pkg/model"
)

type Store interface {
	Insert(ctx context.Context, logEntry *model.TrafficLog) error
	QueryByIP(ctx context.Context, ip string, limit int) ([]model.TrafficLog, error)
	Close() error
}
