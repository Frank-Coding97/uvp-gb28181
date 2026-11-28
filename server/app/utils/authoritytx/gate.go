// Package authoritytx serializes share issuance and authority changes across
// application instances. Acquire must be the first statement in the transaction.
package authoritytx

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
)

type Gate struct {
	ID       uint   `gorm:"primaryKey;autoIncrement:false"`
	Revision uint64 `gorm:"not null"`
}

func (Gate) TableName() string { return "gb_video_share_authority" }

func Acquire(ctx context.Context, tx *gorm.DB) error {
	if ctx == nil || tx == nil || tx.Statement == nil {
		return errors.New("authority transaction is required")
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errors.New("authority gate requires an explicit transaction")
	}
	result := tx.WithContext(ctx).Model(&Gate{}).Where("id = ?", 1).UpdateColumn("revision", gorm.Expr("revision + 1"))
	if result.Error != nil {
		return fmt.Errorf("lock authority gate: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.New("authority gate singleton is missing")
	}
	return nil
}
