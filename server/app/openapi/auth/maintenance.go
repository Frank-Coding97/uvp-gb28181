package auth

import (
	"context"
	"time"

	"uvplatform.cn/uvp-gb28181/app/openapi/audit"
)

// RunMaintenance belongs to the same single-process lifetime as the Gateway.
// It never performs startup recovery over live requests. Each minute processes
// at most one bounded nonce batch and one terminal-audit batch.
func (g *Gateway) RunMaintenance(ctx context.Context, report func(error)) {
	if g == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	g.runMaintenance(ctx, ticker.C, report)
}

func (g *Gateway) runMaintenance(ctx context.Context, ticks <-chan time.Time, report func(error)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if ctx.Err() != nil {
				return
			}
			batch, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := g.maintain(batch)
			cancel()
			if err != nil && ctx.Err() == nil && report != nil {
				// Never report driver errors/SQL values from cleanup.
				report(ErrUnavailable)
			}
		}
	}
}

func (g *Gateway) maintain(ctx context.Context) error {
	// Share the admission clock: a rollback freezes ingress as well as cleanup.
	if _, err := g.admission.Cleanup(ctx); err != nil {
		return err
	}
	_, err := audit.New(g.db, g.admission.now).Cleanup(ctx)
	return err
}
