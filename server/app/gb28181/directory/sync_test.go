package directory

import "testing"

func TestSyncSnapshots(t *testing.T) {
	cache := NewSyncCache()
	tree := []DirectoryNodeVO{{Key: "root", Children: []DirectoryNodeVO{{Key: "device", Name: "old"}}}}
	first := cache.Sync("dept-a", "", tree)
	if !first.Reset || first.Revision == "" {
		t.Fatal("first sync must reset")
	}
	same := cache.Sync("dept-a", first.Revision, tree)
	if same.Reset || len(same.Upserts) != 0 || len(same.Removed) != 0 {
		t.Fatal("unchanged tree must have empty delta")
	}
	tree[0].Children[0].Name = "new"
	changed := cache.Sync("dept-a", first.Revision, tree)
	if changed.Reset || len(changed.Upserts) != 1 || changed.Upserts[0].Node.Name != "new" {
		t.Fatalf("unexpected change: %+v", changed)
	}
	moved := []DirectoryNodeVO{{Key: "other", Children: tree[0].Children}}
	delta := cache.Sync("dept-a", changed.Revision, moved)
	if delta.Reset || len(delta.Removed) != 1 || delta.Removed[0] != "root" || len(delta.Upserts) != 1 {
		t.Fatalf("unexpected move: %+v", delta)
	}
	if !cache.Sync("dept-b", delta.Revision, moved).Reset {
		t.Fatal("scope change must reset")
	}
	if !cache.Sync("dept-a", "expired", moved).Reset {
		t.Fatal("missing baseline must reset")
	}
}

func TestSyncCacheBounded(t *testing.T) {
	cache := NewSyncCache()
	for i := 0; i < 100; i++ {
		cache.Sync(string(rune(i)), "", nil)
	}
	if len(cache.entries) > 64 {
		t.Fatal("snapshot cache must be bounded")
	}
}
