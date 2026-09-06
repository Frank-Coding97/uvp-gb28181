package client

import (
	"context"
	"sort"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func SupportedScopes() []string {
	scopes := make([]string, 0, len(supportedScopes))
	for scope := range supportedScopes {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

// SetScopes replaces the enabled capability set atomically. The outer transaction
// owns all nested SetScope savepoints, audit writes and durable revocation intents.
func (s *Service) SetScopes(ctx context.Context, id int64, scopes []string, version int64, actor uint) (ClientView, error) {
	if actor == 0 {
		return ClientView{}, ErrAuthorizationUnavailable
	}
	if id <= 0 || version <= 0 || len(scopes) > len(supportedScopes) {
		return ClientView{}, ErrInvalidArgument
	}
	wanted := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		if !isSupportedScope(scope) {
			return ClientView{}, ErrUnknownScope
		}
		if wanted[scope] {
			return ClientView{}, ErrInvalidArgument
		}
		wanted[scope] = true
	}
	var view ClientView
	err := s.db.WithContext(normalizeContext(ctx)).Transaction(func(tx *gorm.DB) error {
		nested := *s
		nested.db = tx
		row, err := loadClient(tx, normalizeContext(ctx), id)
		if err != nil {
			return err
		}
		if row.Status == models.StatusRevoked {
			return ErrRevoked
		}
		if row.Status != models.StatusActive {
			return ErrClientDisabled
		}
		if row.RowVersion != version {
			return ErrConflict
		}
		if err := s.authorizeClient(ctx, actor, "scope.set", row.OwnerDeptID); err != nil {
			return err
		}
		existing, err := nested.ListScopes(ctx, id)
		if err != nil {
			return err
		}
		current := make(map[string]bool, len(existing))
		for _, scope := range existing {
			current[scope.Scope] = scope.Enabled
		}
		keys := make([]string, 0, len(supportedScopes))
		for scope := range supportedScopes {
			keys = append(keys, scope)
		}
		sort.Strings(keys)
		view = toClientView(row)
		for _, scope := range keys {
			if current[scope] == wanted[scope] {
				continue
			}
			view, err = nested.SetScope(ctx, id, scope, wanted[scope], view.RowVersion, actor)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ClientView{}, err
	}
	return view, nil
}
