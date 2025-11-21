package db

import (
	"context"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/interfaces"

	"gorm.io/gorm"
)

type GormTransactor struct {
	db *gorm.DB
}

func NewGormTransactor(db *gorm.DB) interfaces.Transactor {
	return &GormTransactor{db: db}
}

func (t *GormTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return t.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, contextkeys.TransactionKey, tx)
		return fn(txCtx)
	})
}

func ExtractDB(ctx context.Context, fallbackDB *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(contextkeys.TransactionKey).(*gorm.DB); ok {
		return tx
	}
	return fallbackDB
}
