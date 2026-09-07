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

func StartSessionPruner(ctx context.Context, repo *GormRepository, now func() time.Time, onError func(error)) <-chan struct{} {
	done := make(chan struct{})
	if repo == nil {
		close(done)
		return done
	}
	if now == nil {
		now = time.Now
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(pruneInterval)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			for {
				if ctx.Err() != nil {
					return
				}
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
	return done
}
