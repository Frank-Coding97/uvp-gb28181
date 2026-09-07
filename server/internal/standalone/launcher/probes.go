package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"uvplatform.cn/uvp-gb28181/internal/standalone/readiness"
)

// Locked custom build with stdin control and MP4 finalization tracking.
const mediaCommit = "86059fe79984cd50b00d98ec6813cdc1558213dd"

var requiredMediaAPIs = []string{
	"/index/api/getApiList", "/index/api/getServerConfig", "/index/api/openRtpServer",
	"/index/api/listRtpServer", "/index/api/closeRtpServer", "/index/api/getMediaTrafficStatistic",
	"/index/api/getMediaPlayerList", "/index/api/addProbe", "/index/api/loadMP4File",
	"/index/api/startRecord", "/index/api/stopRecord", "/index/api/isRecording",
}

func checkRedis(ctx context.Context, address, password string) error {
	client := redis.NewClient(&redis.Options{Addr: address, Password: password, MaxRetries: -1, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second})
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis authentication or PING failed: %w", err)
	}
	nonce, err := readiness.NewChallenge()
	if err != nil {
		return err
	}
	key := "uvp:standalone:readiness:" + nonce
	// PING alone succeeds under OOM/MISCONF. A unique expiring write checks the
	// actual persistence/write boundary without modifying business cache keys.
	if err = client.Set(ctx, key, "1", time.Minute).Err(); err != nil {
		return fmt.Errorf("Redis is not writable: %w", err)
	}
	if err = client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("Redis readiness cleanup failed: %w", err)
	}
	return nil
}

func checkMedia(ctx context.Context, baseURL, secret string) error {
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(path string, target any) error {
		form := url.Values{"secret": []string{secret}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, strings.NewReader(form.Encode()))
		if err != nil {
			return errors.New("invalid media readiness request")
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, err := client.Do(req)
		if err != nil {
			return errors.New("media readiness request failed")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("media readiness HTTP status %d", response.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
		if err != nil || len(body) > 65536 {
			return errors.New("invalid media readiness response size")
		}
		var result struct {
			Code *int            `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(body, &result) != nil || result.Code == nil || *result.Code != 0 {
			return errors.New("media readiness authentication or API failed")
		}
		if json.Unmarshal(result.Data, target) != nil {
			return errors.New("invalid media readiness data")
		}
		return nil
	}
	var version struct {
		Commit string `json:"commitHash"`
	}
	if err := call("/index/api/version", &version); err != nil {
		return err
	}
	if len(version.Commit) < 7 || !strings.HasPrefix(mediaCommit, version.Commit) {
		return errors.New("media build does not match the locked custom commit")
	}
	var apis []string
	if err := call("/index/api/getApiList", &apis); err != nil {
		return err
	}
	available := map[string]bool{}
	for _, api := range apis {
		available[api] = true
	}
	for _, api := range requiredMediaAPIs {
		if !available[api] {
			return fmt.Errorf("required media capability missing: %s", api)
		}
	}
	return nil
}
