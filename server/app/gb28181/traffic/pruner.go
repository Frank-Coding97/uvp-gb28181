package traffic

import (
	"context"
	"time"
)

const (
	settledSessionRetention = 90 * 24 * time.Hour
	pruneInterval           = 24 * time.Hour
	pruneBatchSize          = 500
)

func StartSessionPruner(ctx context.Context, repo *GormRepository, now func() time.Time, onError func(error)) {
	if repo == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	go func() {
		ticker := time.NewTicker(pruneInterval)
		defer ticker.Stop()
		for {
			for {
				deleted, err := repo.PruneSettledBefore(ctx, now().Add(-settledSessionRetention), pruneBatchSize)
				if err != nil {
					if onError != nil {
						onError(err)
					}
					break
				}
				if deleted < pruneBatchSize {
					break
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
