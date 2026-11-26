package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yz1qaq/asterflow/crossborder/internal/domain"
)

var (
	ErrNotFound     = errors.New("business resource not found")
	ErrInvalidInput = errors.New("invalid business input")
)

type Store interface {
	GetOrder(ctx context.Context, id string) (domain.Order, error)
	ListInventoryBySKU(
		ctx context.Context,
		sku string,
	) ([]domain.InventoryBalance, error)
}

var _ Store = (*PostgresStore)(nil)

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) GetOrder(ctx context.Context, id string) (domain.Order, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.Order{}, fmt.Errorf(
			"%w: order_id is required",
			ErrInvalidInput,
		)
	}

	return s.store.GetOrder(ctx, id)
}

func (s *Service) Inventory(ctx context.Context, sku string) ([]domain.InventoryBalance, error) {
	sku = strings.TrimSpace(sku)
	if sku == "" {
		return nil, fmt.Errorf(
			"%w: sku is required",
			ErrInvalidInput,
		)
	}

	return s.store.ListInventoryBySKU(ctx, sku)
}
