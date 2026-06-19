package variables

import (
	"context"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type Row = persistence.VariableRow
type Store = persistence.VariableStore

type Resolver struct {
	store Store
}

func NewResolver(store Store) *Resolver {
	return &Resolver{store: store}
}

func (r *Resolver) Resolve(ctx context.Context) (map[string]any, error) {
	if r == nil || r.store == nil {
		return map[string]any{}, nil
	}
	return r.store.Resolve(ctx)
}

func Resolve(ctx context.Context, store Store) (map[string]any, error) {
	return NewResolver(store).Resolve(ctx)
}
