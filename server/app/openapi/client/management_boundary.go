package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"gorm.io/gorm"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

const (
	ManagementActionRead   = "read"
	ManagementActionCreate = "create"
	ManagementActionGrant  = "grant"
	ManagementActionRotate = "rotate"
	ManagementActionStatus = "status"
	ManagementActionAudit  = "audit"
)

var ErrManagementBoundaryDenied = errors.New("OpenAPI management boundary denied")

const managementClientRoute = "/api/gb28181/openapi-clients"

type managementPermission struct {
	path              string
	method            string
	allowInactiveDept bool
}

// ManagementPermissionAuthorizer is the narrow part of the existing Casbin
// permission service needed by this boundary. The production wiring passes
// app.CasbinV2; tests may inject a real Casbin-backed adapter without making
// authorization depend on a package global. obj and act are an existing
// sys_api path and HTTP method, not a second OpenAPI permission vocabulary.
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
	return b.authorize(ctx, actorID, managementPermissionForCategory(ManagementActionCreate), ownerDeptID)
}

func (b *ManagementScopeBoundary) AuthorizeRead(ctx context.Context, actorID uint, ownerDeptID uint) error {
	return b.authorize(ctx, actorID, managementPermissionForCategory(ManagementActionRead), ownerDeptID)
}

func (b *ManagementScopeBoundary) AuthorizeGrant(ctx context.Context, actorID uint, ownerDeptID uint) error {
	return b.authorize(ctx, actorID, managementPermissionForCategory(ManagementActionGrant), ownerDeptID)
}

func (b *ManagementScopeBoundary) AuthorizeRotate(ctx context.Context, actorID uint, ownerDeptID uint) error {
	return b.authorize(ctx, actorID, managementPermissionForCategory(ManagementActionRotate), ownerDeptID)
}

func (b *ManagementScopeBoundary) AuthorizeStatus(ctx context.Context, actorID uint, ownerDeptID uint) error {
	return b.authorize(ctx, actorID, managementPermissionForCategory(ManagementActionStatus), ownerDeptID)
}

func (b *ManagementScopeBoundary) AuthorizeAudit(ctx context.Context, actorID uint, ownerDeptID uint) error {
	return b.authorize(ctx, actorID, managementPermissionForCategory(ManagementActionAudit), ownerDeptID)
}

// AuthorizeClient maps only the business actions emitted by client.Service to
// the corresponding existing management route and HTTP method. Read/create/
// audit are exposed by the dedicated methods above because Service does not
// emit those action strings.
func (b *ManagementScopeBoundary) AuthorizeClient(ctx context.Context, actorID uint, action string, ownerDeptID uint) error {
	permission, ok := managementPermissionForServiceAction(action)
	if !ok {
		return ErrManagementBoundaryDenied
	}
	return b.authorize(ctx, actorID, permission, ownerDeptID)
}

func (b *ManagementScopeBoundary) authorize(ctx context.Context, actorID uint, permission managementPermission, ownerDeptID uint) error {
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

	if !access.FullAccess && !containsDepartment(access.DeptIDs, ownerDeptID) {
		return ErrManagementBoundaryDenied
	}
	if !permission.allowInactiveDept {
		if err := requireActiveDepartment(ctx, b.db, ownerDeptID); err != nil {
			if errors.Is(err, ErrManagementBoundaryDenied) {
				return err
			}
			return fmt.Errorf("%w: resolve target department: %v", ErrAuthorizationUnavailable, err)
		}
	}

	allowed, err := b.authorizer.Enforce(managementSubject(actorID), permission.path, permission.method, "*")
	if err != nil {
		return fmt.Errorf("%w: enforce management permission: %v", ErrAuthorizationUnavailable, err)
	}
	if !allowed {
		return ErrManagementBoundaryDenied
	}
	return nil
}

func managementPermissionForCategory(action string) managementPermission {
	switch action {
	case ManagementActionRead:
		return managementPermission{path: managementClientRoute, method: http.MethodGet, allowInactiveDept: true}
	case ManagementActionCreate:
		return managementPermission{path: managementClientRoute, method: http.MethodPost}
	case ManagementActionGrant:
		return managementPermission{path: managementClientRoute + "/:id/scopes", method: http.MethodPut}
	case ManagementActionRotate:
		return managementPermission{path: managementClientRoute + "/:id/rotate-secret", method: http.MethodPost}
	case ManagementActionStatus:
		return managementPermission{path: managementClientRoute + "/:id/disable", method: http.MethodPost, allowInactiveDept: true}
	case ManagementActionAudit:
		return managementPermission{path: managementClientRoute + "/:id/audits", method: http.MethodGet, allowInactiveDept: true}
	default:
		return managementPermission{}
	}
}

func managementPermissionForServiceAction(action string) (managementPermission, bool) {
	switch action {
	case "scope.set":
		return managementPermissionForCategory(ManagementActionGrant), true
	case "client.rotate":
		return managementPermissionForCategory(ManagementActionRotate), true
	case "client.active":
		return managementPermission{path: managementClientRoute + "/:id/enable", method: http.MethodPost}, true
	case "client.disabled":
		return managementPermission{path: managementClientRoute + "/:id/disable", method: http.MethodPost, allowInactiveDept: true}, true
	case "client.revoked":
		return managementPermission{path: managementClientRoute + "/:id/revoke", method: http.MethodPost, allowInactiveDept: true}, true
	default:
		return managementPermission{}, false
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
