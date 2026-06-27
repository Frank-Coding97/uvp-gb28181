package stream_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

func TestLocationMap_BindLookupUnbind(t *testing.T) {
	m := stream.NewLocationMap()

	_, ok := m.Lookup("nope")
	require.False(t, ok)

	m.Bind("s1", 100)
	id, ok := m.Lookup("s1")
	require.True(t, ok)
	require.Equal(t, int64(100), id)
	require.Equal(t, 1, m.Size())

	m.Unbind("s1")
	_, ok = m.Lookup("s1")
	require.False(t, ok)
	require.Equal(t, 0, m.Size())
}

func TestLocationMap_BindOverwrite(t *testing.T) {
	m := stream.NewLocationMap()
	m.Bind("s1", 1)
	m.Bind("s1", 2)
	id, _ := m.Lookup("s1")
	require.Equal(t, int64(2), id)
	require.Equal(t, 1, m.Size())
}

func TestLocationMap_Concurrent(t *testing.T) {
	m := stream.NewLocationMap()
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sid := fmt.Sprintf("s%d", i%50) // 50 个 stream,1000 个 op,故意冲突
			switch i % 3 {
			case 0:
				m.Bind(sid, int64(i))
			case 1:
				_, _ = m.Lookup(sid)
			case 2:
				m.Unbind(sid)
			}
		}(i)
	}
	wg.Wait()
	// 不报具体 Size,只要无 panic / -race 无报错就 OK
	_ = m.Size()
}

func TestLocationMap_UnbindNonexistent_NoOp(t *testing.T) {
	m := stream.NewLocationMap()
	m.Unbind("nope")
	require.Equal(t, 0, m.Size())
}
