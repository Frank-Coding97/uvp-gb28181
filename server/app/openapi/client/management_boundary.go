package client

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

const ManagementPermissionObject = "openapi:client"

const (
	ManagementActionRead   = "read"
	ManagementActionCreate = "create"
	ManagementActionGrant  = "grant"
	ManagementActionRotate = "rotate"
	ManagementActionStatus = "status"
	ManagementActionAudit  = "audit"
)

var ErrManagementBoundaryDenied = errors.New("OpenAPI management boundary denied")

// ManagementPermissionAuthorizer is the narrow part of the existing Casbin
// permission service needed by this boundary. The production wiring passes
// app.CasbinV2; tests may inject a real Casbin-backed adapter without making
// authorization depend on a package global.
type ManagementPermissionAuthorizer interface {
	Enforce(sub, obj, act string, domain ...string) (bool, error)
}

// ManagementScopeBoundary implements the client.ManagementBoundary contract.
// It combines current trusted personnel scope with an independent permission
// check. It deliberately has no "allow all" or caller-supplied boolean path.
type ManagementScopeBoundary struct {
	db         *gorm.DB
	authorizer ManagementPermissionAuthorizer
}

var _ ManagementBoundary = (*ManagementScopeBoundary)(nil)

func NewManagementScopeBoundary(db *gorm.DB, authorizer ManagementPermissionAuthorizer) *ManagementScopeBoundary {
	return &ManagementScopeBoundary{db: db, authorizer: authorizer}
}

func (b *ManagementScopeBoundary) AuthorizeCreate(ctx context.Context, actorID uint, ownerDeptID uint) error {
	return b.authorize(ctx, actorID, ManagementActionCreate, ownerDeptID)
}

func (b *ManagementScopeBoundary) AuthorizeRead(ctx context.Context, actorID uint, ownerDeptID uint) error {
	return b.authorize(ctx, actorID, ManagementActionRead, ownerDeptID)
}

func (b *ManagementScopeBoundary) AuthorizeGrant(ctx context.Context, actorID uint, ownerDeptID uint) error {
	return b.authorize(ctx, actorID, ManagementActionGrant, ownerDeptID)
}

func (b *ManagementScopeBoundary) AuthorizeRotate(ctx context.Context, actorID uint, ownerDeptID uint) error {
	return b.authorize(ctx, actorID, ManagementActionRotate, ownerDeptID)
}

func (b *ManagementScopeBoundary) AuthorizeStatus(ctx context.Context, actorID uint, ownerDeptID uint) error {
	return b.authorize(ctx, actorID, ManagementActionStatus, ownerDeptID)
}

func (b *ManagementScopeBoundary) AuthorizeAudit(ctx context.Context, actorID uint, ownerDeptID uint) error {
	return b.authorize(ctx, actorID, ManagementActionAudit, ownerDeptID)
}

// AuthorizeClient maps the service's stable business actions to independent
// management permissions. Status transitions share only the status action;
// grant, rotate, read and audit never inherit one another.
func (b *ManagementScopeBoundary) AuthorizeClient(ctx context.Context, actorID uint, action string, ownerDeptID uint) error {
	permissionAction, ok := managementPermissionAction(action)
	if !ok {
		return ErrManagementBoundaryDenied
	}
	return b.authorize(ctx, actorID, permissionAction, ownerDeptID)
}

func (b *ManagementScopeBoundary) authorize(ctx context.Context, actorID uint, action string, ownerDeptID uint) error {
	if b == nil || b.db == nil || b.authorizer == nil || actorID == 0 || ownerDeptID == 0 {
		return ErrAuthorizationUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}

	access, err := datascope.ResolveOwnerDeptAccessByUserID(ctx, b.db, actorID)
	if err != nil {
		if errors.Is(err, datascope.ErrOwnerDeptAccessDenied) {
			return ErrManagementBoundaryDenied
		}
		return fmt.Errorf("%w: resolve trusted management scope: %v", ErrAuthorizationUnavailable, err)
	}

	if err := requireActiveDepartment(ctx, b.db, ownerDeptID); err != nil {
		if errors.Is(err, ErrManagementBoundaryDenied) {
			return err
		}
		return fmt.Errorf("%w: resolve target department: %v", ErrAuthorizationUnavailable, err)
	}
	if !access.FullAccess && !containsDepartment(access.DeptIDs, ownerDeptID) {
		return ErrManagementBoundaryDenied
	}

	allowed, err := b.authorizer.Enforce(managementSubject(actorID), ManagementPermissionObject, action, "*")
	if err != nil {
		return fmt.Errorf("%w: enforce management permission: %v", ErrAuthorizationUnavailable, err)
	}
	if !allowed {
		return ErrManagementBoundaryDenied
	}
	return nil
}

func managementPermissionAction(action string) (string, bool) {
	switch action {
	case ManagementActionRead, "client.read":
		return ManagementActionRead, true
	case ManagementActionCreate, "client.create":
		return ManagementActionCreate, true
	case ManagementActionGrant, "client.grant", "scope.set":
		return ManagementActionGrant, true
	case ManagementActionRotate, "client.rotate":
		return ManagementActionRotate, true
	case ManagementActionStatus, "client.status", "client.active", "client.disabled", "client.revoked":
		return ManagementActionStatus, true
	case ManagementActionAudit, "client.audit":
		return ManagementActionAudit, true
	default:
		return "", false
	}
}

func managementSubject(actorID uint) string {
	return fmt.Sprintf("user_%d", actorID)
}

func requireActiveDepartment(ctx context.Context, db *gorm.DB, departmentID uint) error {
	var department appmodels.SysDepartment
	result := db.WithContext(ctx).
		Where("id = ? AND status = ?", departmentID, int8(1)).
		First(&department)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrManagementBoundaryDenied
		}
		return result.Error
	}
	if result.RowsAffected == 0 || department.ID == 0 {
		return ErrManagementBoundaryDenied
	}
	return nil
}

func containsDepartment(departmentIDs []uint, wanted uint) bool {
	for _, departmentID := range departmentIDs {
		if departmentID == wanted {
			return true
		}
	}
	return false
}
