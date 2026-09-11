package standalone

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	recoveryAccountLockedPrefix = "account_locked:"
	recoveryLoginFailPrefix     = "login_fail_count:"
)

type recoveryRestriction struct {
	Key             string
	Value           string
	ExpiresAtMillis int64
}

// exportRecoveryRestrictions copies only the two security-related Redis
// keyspaces needed by offline recovery. Expiry is read as an absolute Redis
// timestamp so a restore does not extend a lock or failure window.
func exportRecoveryRestrictions(ctx context.Context, source *redis.Client) ([]recoveryRestriction, error) {
	if source == nil {
		return nil, errors.New("standalone recovery Redis source is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	seen := make(map[string]struct{})
	items := make([]recoveryRestriction, 0)
	for _, prefix := range []string{recoveryAccountLockedPrefix, recoveryLoginFailPrefix} {
		var cursor uint64
		for {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("export recovery Redis restrictions: %w", err)
			}
			keys, next, err := source.Scan(ctx, cursor, prefix+"*", 100).Result()
			if err != nil {
				return nil, errors.New("export recovery Redis restrictions: SCAN failed")
			}
			for _, key := range keys {
				if _, exists := seen[key]; exists {
					// SCAN may repeat a key while the keyspace changes. The
					// standalone coordinator has stopped writers, so retaining the
					// first observation is sufficient and avoids a false failure.
					continue
				}
				seen[key] = struct{}{}
				if !validRecoveryKey(key) {
					return nil, errors.New("export recovery Redis restrictions: invalid key")
				}
				value, err := source.Get(ctx, key).Result()
				if errors.Is(err, redis.Nil) {
					continue
				}
				if err != nil {
					return nil, errors.New("export recovery Redis restrictions: GET failed")
				}
				if !validRecoveryValue(key, value) {
					return nil, errors.New("export recovery Redis restrictions: invalid value")
				}
				expiresAt, err := source.Do(ctx, "PEXPIRETIME", key).Int64()
				if err != nil {
					return nil, errors.New("export recovery Redis restrictions: PEXPIRETIME failed")
				}
				if expiresAt == -1 {
					return nil, errors.New("export recovery Redis restrictions: persistent key")
				}
				if expiresAt < -2 {
					return nil, errors.New("export recovery Redis restrictions: invalid expiry")
				}
				if expiresAt == -2 || expiresAt <= time.Now().UnixMilli() {
					continue
				}
				items = append(items, recoveryRestriction{Key: key, Value: value, ExpiresAtMillis: expiresAt})
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}

// importRecoveryRestrictions restores the already validated whitelist into a
// fresh Redis database. It intentionally fails when the target is non-empty;
// the coordinator owns destruction of that isolated target after any failure.
func importRecoveryRestrictions(ctx context.Context, target *redis.Client, items []recoveryRestriction) error {
	if target == nil {
		return errors.New("standalone recovery Redis target is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("import recovery Redis restrictions: %w", err)
	}
	databaseSize, err := target.DBSize(ctx).Result()
	if err != nil {
		return errors.New("import recovery Redis restrictions: DBSIZE failed")
	}
	if databaseSize != 0 {
		return errors.New("import recovery Redis restrictions: target is not empty")
	}

	seen := make(map[string]struct{}, len(items))
	active := make([]recoveryRestriction, 0, len(items))
	now := time.Now().UnixMilli()
	for _, item := range items {
		if _, exists := seen[item.Key]; exists {
			return errors.New("import recovery Redis restrictions: duplicate key")
		}
		seen[item.Key] = struct{}{}
		if !validRecoveryKey(item.Key) {
			return errors.New("import recovery Redis restrictions: invalid key")
		}
		if !validRecoveryValue(item.Key, item.Value) {
			return errors.New("import recovery Redis restrictions: invalid value")
		}
		if item.ExpiresAtMillis < 0 {
			return errors.New("import recovery Redis restrictions: invalid expiry")
		}
		if item.ExpiresAtMillis > now {
			active = append(active, item)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].Key < active[j].Key })
	for _, item := range active {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("import recovery Redis restrictions: %w", err)
		}
		if err := target.Do(ctx, "SET", item.Key, item.Value, "PXAT", strconv.FormatInt(item.ExpiresAtMillis, 10)).Err(); err != nil {
			return errors.New("import recovery Redis restrictions: SET failed")
		}
	}
	return verifyRecoveryRestrictions(ctx, target, active)
}

func verifyRecoveryRestrictions(ctx context.Context, target *redis.Client, expected []recoveryRestriction) error {
	verified := make(map[string]recoveryRestriction, len(expected))
	for _, item := range expected {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("verify recovery Redis restrictions: %w", err)
		}
		value, err := target.Get(ctx, item.Key).Result()
		if errors.Is(err, redis.Nil) {
			if time.Now().UnixMilli() >= item.ExpiresAtMillis {
				continue
			}
			return errors.New("verify recovery Redis restrictions: key disappeared")
		}
		if err != nil {
			return errors.New("verify recovery Redis restrictions: GET failed")
		}
		if value != item.Value || !validRecoveryValue(item.Key, value) {
			return errors.New("verify recovery Redis restrictions: value mismatch")
		}
		expiresAt, err := target.Do(ctx, "PEXPIRETIME", item.Key).Int64()
		if err != nil {
			return errors.New("verify recovery Redis restrictions: PEXPIRETIME failed")
		}
		if expiresAt == -2 {
			if time.Now().UnixMilli() >= item.ExpiresAtMillis {
				continue
			}
			return errors.New("verify recovery Redis restrictions: key disappeared")
		}
		if expiresAt == -1 || expiresAt != item.ExpiresAtMillis {
			return errors.New("verify recovery Redis restrictions: expiry mismatch")
		}
		verified[item.Key] = item
	}

	keys, err := scanRecoveryKeys(ctx, target)
	if err != nil {
		return errors.New("verify recovery Redis restrictions: SCAN failed")
	}
	for key := range keys {
		if _, ok := verified[key]; !ok {
			return errors.New("verify recovery Redis restrictions: unexpected key")
		}
	}
	for key, item := range verified {
		if _, ok := keys[key]; ok {
			continue
		}
		if time.Now().UnixMilli() < item.ExpiresAtMillis {
			return errors.New("verify recovery Redis restrictions: key disappeared")
		}
	}
	return nil
}

func scanRecoveryKeys(ctx context.Context, client *redis.Client) (map[string]struct{}, error) {
	keys := make(map[string]struct{})
	var cursor uint64
	for {
		batch, next, err := client.Scan(ctx, cursor, "*", 100).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range batch {
			keys[key] = struct{}{}
		}
		cursor = next
		if cursor == 0 {
			return keys, nil
		}
	}
}

func validRecoveryKey(key string) bool {
	if strings.HasPrefix(key, recoveryAccountLockedPrefix) {
		return len(key) > len(recoveryAccountLockedPrefix)
	}
	if strings.HasPrefix(key, recoveryLoginFailPrefix) {
		return len(key) > len(recoveryLoginFailPrefix)
	}
	return false
}

func validRecoveryValue(key, value string) bool {
	if strings.HasPrefix(key, recoveryAccountLockedPrefix) {
		return value == "1"
	}
	if !strings.HasPrefix(key, recoveryLoginFailPrefix) || value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed >= 0
}
