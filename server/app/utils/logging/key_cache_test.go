package logging

import (
	"fmt"
	"go.uber.org/zap"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unsafe"
)

var classificationSink bool

func BenchmarkLoggingKeyClassification(b *testing.B) {
	keys := []string{"event", "device_id", "scanned", "changed", "duration_ms", "status", "operation", "count"}
	for _, mode := range []string{"Direct", "Hit", "Miss"} {
		b.Run(mode, func(b *testing.B) {
			cache := &fieldKeyCache{}
			activeKeys := keys
			if mode == "Miss" {
				activeKeys = []string{"field", cacheCollision(b, "field", "field_")}
			}
			for _, key := range keys {
				cache.lookup(key)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := activeKeys[i%len(activeKeys)]
				if mode == "Direct" {
					classificationSink = sensitiveKey(key)
					if i%2 == 0 {
						classificationSink = classificationSink || stringFieldPolicy(key) != stringFieldPlain
					}
				} else {

					result := cache.lookup(key)
					classificationSink = result.sensitive || result.rule != stringFieldPlain
				}
			}
		})
	}
}

func TestLoggingKeyCacheClassification(t *testing.T) {
	cache := &fieldKeyCache{}
	keys := []string{"event", "device_id", "Access_Token", "secret", "uri", "requestURL", "reason", "Error", "a.p.i-k_e_y", "label\xff", strings.Repeat("x", 128), strings.Repeat("x", 129)}
	for _, key := range keys {
		for attempt := 0; attempt < 2; attempt++ {
			got := cache.lookup(key)
			if got.key != key || got.sensitive != sensitiveKey(key) || got.rule != stringFieldPolicy(key) {
				t.Fatalf("cache changed classification on attempt %d", attempt)
			}
		}
	}
	for i := range cache.slots {
		if entry := cache.slots[i].Load(); entry != nil && len(entry.key) > 128 {
			t.Fatal("oversized key retained")
		}
	}
}

func cacheCollision(t testing.TB, first, prefix string) string {
	t.Helper()
	for i := 0; i < 100000; i++ {
		candidate := fmt.Sprintf("%s%d", prefix, i)
		if candidate != first && fieldKeySlot(candidate) == fieldKeySlot(first) {
			return candidate
		}
	}
	t.Fatal("collision fixture unavailable")
	return ""
}
func TestLoggingKeyCacheCollisionAndConcurrentReplacement(t *testing.T) {
	for _, pair := range [][2]string{{"event", cacheCollision(t, "event", "secret")}, {"label", cacheCollision(t, "label", "url")}} {
		cache := &fieldKeyCache{}
		var wg sync.WaitGroup
		for worker := 0; worker < 32; worker++ {
			wg.Add(1)
			go func(offset int) {
				defer wg.Done()
				for i := 0; i < 500; i++ {
					key := pair[(i+offset)%2]
					got := cache.lookup(key)
					if got.key != key || got.sensitive != sensitiveKey(key) || got.rule != stringFieldPolicy(key) {
						t.Error("hash collision reused another key's policy")
						return
					}
				}
			}(worker)
		}
		wg.Wait()
	}
}
func TestLoggingKeyCacheBoundedOwnership(t *testing.T) {
	cache := &fieldKeyCache{}
	for i := 0; i < 10000; i++ {
		cache.lookup(fmt.Sprintf("field_%d", i))
	}
	count, bytes := 0, 0
	for i := range cache.slots {
		if entry := cache.slots[i].Load(); entry != nil {
			count++
			bytes += len(entry.key)
		}
	}
	if count > 256 || bytes > 256*128 {
		t.Fatalf("cache exceeded bound: %d/%d", count, bytes)
	}
	backing := strings.Repeat("x", 1<<20)
	key := backing[:21]
	entry := cache.lookup(key)
	if unsafe.StringData(entry.key) == unsafe.StringData(key) {
		t.Fatal("short key retained caller backing allocation")
	}
	typ := reflect.TypeOf(fieldKeyMetadata{})
	if typ.NumField() != 3 || typ.Field(0).Type.Kind() != reflect.String || typ.Field(1).Type.Kind() != reflect.Bool || typ.Field(2).Type.Kind() != reflect.Uint8 {
		t.Fatal("cache entry unexpectedly retains values or caller fields")
	}
	var none *fieldKeyCache
	if none.lookup("password").sensitive != true {
		t.Fatal("nil cache changed safety")
	}
}
func TestLoggingKeyCacheRuntimeOwnership(t *testing.T) {
	first, _ := runtimeForTest(t, nil)
	second, _ := runtimeForTest(t, nil)
	a := first.Root.Core().(*runtimeCore)
	b := second.Root.Core().(*runtimeCore)
	child := first.Root.With(zap.String("event", "fixture")).Core().(*runtimeCore)
	if a.keyPolicies == nil || a.keyPolicies == b.keyPolicies || child.keyPolicies != a.keyPolicies {
		t.Fatal("cache must be shared by With and isolated across runtimes")
	}
}
