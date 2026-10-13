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
		if errors.Is(err, repository.ErrInvalidProjection) {
			return fmt.Errorf("%w：%s", ErrInvalidBusinessGroup, strings.TrimPrefix(err.Error(), repository.ErrInvalidProjection.Error()+": "))
		}
		return err
	}
	return nil
}
