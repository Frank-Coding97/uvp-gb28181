package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
)

// ReplaceBusinessGroup changes directory placement only; shared resources remain
// the authorization source of truth and are checked in the repository transaction.
func (s *ManagementService) ReplaceBusinessGroup(ctx context.Context, platformID, revision uint64, nodes []repository.BusinessNodeInput, bindings []repository.BusinessBindingInput) error {
	if err := s.store.ReplaceBusinessGroup(ctx, platformID, revision, nodes, bindings); err != nil {
		return mapBusinessGroupError(err)
	}
	return s.reloadBusinessGroupRuntime(ctx)
}

func (s *ManagementService) ReplaceBusinessGroupWithShares(ctx context.Context, platformID, revision uint64, nodes []repository.BusinessNodeInput, bindings []repository.BusinessBindingInput, shareChannelIDs []uint64, authorize repository.BusinessGroupSourceAuthorizer) error {
	store, ok := s.store.(repository.BusinessGroupShareStore)
	if !ok {
		return fmt.Errorf("%w：业务分组存储不支持原子共享", ErrInvalidBusinessGroup)
	}
	if err := store.ReplaceBusinessGroupWithShares(ctx, platformID, revision, nodes, bindings, shareChannelIDs, authorize); err != nil {
		return mapBusinessGroupError(err)
	}
	return s.reloadBusinessGroupRuntime(ctx)
}

func (s *ManagementService) ReplaceBusinessGroupWithResourceChanges(ctx context.Context, platformID, revision uint64, nodes []repository.BusinessNodeInput, bindings []repository.BusinessBindingInput, legacyShareChannelIDs []uint64, changes repository.BusinessGroupResourceChanges, authorize repository.BusinessGroupSourceAuthorizer) error {
	store, ok := s.store.(repository.BusinessGroupResourceStore)
	if !ok {
		return fmt.Errorf("%w：业务分组存储不支持资源变更", ErrInvalidBusinessGroup)
	}
	if err := store.ReplaceBusinessGroupWithResourceChanges(ctx, platformID, revision, nodes, bindings, legacyShareChannelIDs, changes, authorize); err != nil {
		return mapBusinessGroupError(err)
	}
	return s.reloadBusinessGroupRuntime(ctx)
}

func (s *ManagementService) ReplaceBusinessGroupWithOptions(ctx context.Context, platformID, revision uint64, nodes []repository.BusinessNodeInput, bindings []repository.BusinessBindingInput, legacyShareChannelIDs []uint64, changes repository.BusinessGroupResourceChanges, publication repository.BusinessGroupPublicationUpdate, authorize repository.BusinessGroupSourceAuthorizer, authorizePublication repository.BusinessGroupPublicationAuthorizer) error {
	store, ok := s.store.(repository.BusinessGroupOptionsStore)
	if !ok {
		return fmt.Errorf("%w：业务分组存储不支持原子发布模式变更", ErrInvalidBusinessGroup)
	}
	if err := store.ReplaceBusinessGroupWithOptions(ctx, platformID, revision, nodes, bindings, legacyShareChannelIDs, changes, publication, authorize, authorizePublication); err != nil {
		return mapBusinessGroupError(err)
	}
	return s.reloadBusinessGroupRuntime(ctx)
}

func (s *ManagementService) reloadBusinessGroupRuntime(ctx context.Context) error {
	if s.runtime == nil {
		return nil
	}
	if err := s.runtime.Reload(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrRuntimeSyncFailed, err)
	}
	return nil
}

func mapBusinessGroupError(err error) error {
	if errors.Is(err, repository.ErrInvalidProjection) {
		return fmt.Errorf("%w：%s", ErrInvalidBusinessGroup, strings.TrimPrefix(err.Error(), repository.ErrInvalidProjection.Error()+": "))
	}
	return err
}

var _ repository.BusinessGroupShareStore = (*repository.GormRepository)(nil)
var _ repository.BusinessGroupResourceStore = (*repository.GormRepository)(nil)
var _ repository.BusinessGroupOptionsStore = (*repository.GormRepository)(nil)
