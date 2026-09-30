package balance

import (
	"context"
	"errors"
)

var ErrNoHealthyBackends = errors.New("no healthy backends available")

type Algorithm interface {
	Next(ctx context.Context, backends []*Backend) (*Backend, error)
	Name() string
}
