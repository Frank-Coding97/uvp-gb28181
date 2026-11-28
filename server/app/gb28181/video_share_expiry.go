package gb28181

import (
	"context"
	"time"

	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/logging"
)

type videoShareExpiryScanner interface {
	ScanExpired(context.Context, int) (int, error)
}

func startVideoShareExpiryRuntime(scanner videoShareExpiryScanner, interval time.Duration) (context.CancelFunc, <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	if interval <= 0 {
		interval = 5 * time.Second
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			scanCtx, stop := context.WithTimeout(ctx, 2*time.Second)
			_, err := scanner.ScanExpired(scanCtx, 100)
			stop()
			if err != nil && ctx.Err() == nil {
				app.Log(ctx).Named("audit").Warn("视频分享到期审计稍后重试", zap.String("event", "video_share.expiry_scan_failed"), logging.Error(err))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return cancel, done
}
