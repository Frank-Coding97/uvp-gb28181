package node_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type staleListRepo struct {
	mu          sync.Mutex
	row         node.Node
	listStarted chan struct{}
	listRelease chan struct{}
}

func (r *staleListRepo) List(context.Context) ([]node.Node, error) {
	r.mu.Lock()
	row := r.row
	r.mu.Unlock()
	close(r.listStarted)
	<-r.listRelease
	return []node.Node{row}, nil
}

func (r *staleListRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.row.ID != id {
		return nil, nil
	}
	row := r.row
	return &row, nil
}

func (r *staleListRepo) Create(_ context.Context, n node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n.ID = 1
	r.row = n
	return n.ID, nil
}

func (r *staleListRepo) Update(context.Context, node.Node) error {
	panic("legacy Update must not be called by a CAS-capable Registry")
}

func (r *staleListRepo) UpdateCAS(_ context.Context, n node.Node, expectedRevision uint64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.row.Revision != expectedRevision {
		return false, nil
	}
	n.Revision = expectedRevision + 1
	r.row = n
	return true, nil
}

func (r *staleListRepo) Delete(context.Context, int64) error { return nil }

func TestRegistryT13_LoadAllDoesNotRewindCommittedCAS(t *testing.T) {
	repo := &staleListRepo{
		listStarted: make(chan struct{}),
		listRelease: make(chan struct{}),
	}
	reg := node.NewRegistry(repo)
	added, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "old.example", MediaServerUUID: "uuid-1", State: node.StateActive,
	})
	require.NoError(t, err)

	loadDone := make(chan error, 1)
	go func() { loadDone <- reg.LoadAll(context.Background()) }()
	<-repo.listStarted

	candidate := *added
	candidate.Weight = 80
	require.NoError(t, reg.Update(context.Background(), candidate))
	close(repo.listRelease)
	require.NoError(t, <-loadDone)

	got, ok := reg.Get(added.ID)
	require.True(t, ok)
	require.Equal(t, 80, got.Weight)
	require.EqualValues(t, 2, got.Revision)
}
