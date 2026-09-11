package standalone

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var (
	errRecoveryAuthorization            = errors.New("recovery authorization failed")
	errRecoveryAuthorizationUnsupported = errors.New("recovery authorization is unsupported")
)

const (
	recoveryAdministratorRoleID    int64 = 1
	recoveryAdministratorRoleName        = "系统管理员"
	recoveryAdministratorDataScope int64 = 1

	recoveryAuthorizationUserStateReason      = "账户状态将恢复到备份值"
	recoveryAuthorizationReenableReason       = "恢复后将重新启用账户"
	recoveryAuthorizationRoleBindingReason    = "角色绑定将恢复到备份值"
	recoveryAuthorizationRolePermissionReason = "角色权限将恢复到备份值"
	recoveryAuthorizationPasswordReason       = "密码将恢复到备份值"
)

// authenticateRecoveryAdministrator authenticates one local administrator
// against a stopped recovery database. It never opens the live application DB
// through GORM and never modifies the source SQLite file family.
func authenticateRecoveryAdministrator(ctx context.Context, dbPath, username, password string) (int64, error) {
	if ctx == nil || username == "" || password == "" {
		return 0, errRecoveryAuthorization
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	db, cleanup, err := openRecoveryAuthorizationDatabase(ctx, dbPath)
	if err != nil {
		return 0, errRecoveryAuthorization
	}
	defer cleanup()
	if err := rejectKnownRecoveryAuthorizationTables(ctx, db); err != nil {
		return 0, err
	}

	var (
		id         int64
		storedHash string
		status     int64
		deletedAt  sql.NullString
		matches    int
	)
	rows, err := db.QueryContext(ctx, `
		SELECT id, password, status, deleted_at
		FROM sys_users
		WHERE username = ?`, username)
	if err != nil {
		return 0, errRecoveryAuthorization
	}
	for rows.Next() {
		matches++
		if matches > 1 {
			_ = rows.Close()
			return 0, errRecoveryAuthorization
		}
		if err := rows.Scan(&id, &storedHash, &status, &deletedAt); err != nil {
			_ = rows.Close()
			return 0, errRecoveryAuthorization
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, errRecoveryAuthorization
	}
	if err := rows.Close(); err != nil {
		return 0, errRecoveryAuthorization
	}
	if matches != 1 || id <= 0 || status != 1 || deletedAt.Valid {
		return 0, errRecoveryAuthorization
	}

	var roleMatches int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sys_user_role AS ur
		JOIN sys_role AS r ON r.id = ur.role_id
		WHERE ur.user_id = ?
		  AND ur.role_id = ?
		  AND r.name = ?
		  AND r.status = 1
		  AND r.deleted_at IS NULL
		  AND r.data_scope = ?`, id, recoveryAdministratorRoleID, recoveryAdministratorRoleName, recoveryAdministratorDataScope).Scan(&roleMatches)
	if err != nil || roleMatches != 1 {
		return 0, errRecoveryAuthorization
	}
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)); err != nil {
		return 0, errRecoveryAuthorization
	}
	return id, nil
}

// recoveryAuthorizationImpacts compares the stopped restored and failed
// SQLite authorization state. The result contains usernames and safe reasons;
// password hashes and any other secret material cannot reach the prompt.
func recoveryAuthorizationImpacts(ctx context.Context, restoredDB, failedDB string) ([]string, error) {
	if ctx == nil {
		return nil, errRecoveryAuthorization
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	restored, restoredCleanup, err := openRecoveryAuthorizationDatabase(ctx, restoredDB)
	if err != nil {
		return nil, errRecoveryAuthorization
	}
	defer restoredCleanup()
	failed, failedCleanup, err := openRecoveryAuthorizationDatabase(ctx, failedDB)
	if err != nil {
		return nil, errRecoveryAuthorization
	}
	defer failedCleanup()
	if err := rejectKnownRecoveryAuthorizationTables(ctx, restored); err != nil {
		return nil, err
	}
	if err := rejectKnownRecoveryAuthorizationTables(ctx, failed); err != nil {
		return nil, err
	}

	restoredState, err := readRecoveryAuthorizationSnapshot(ctx, restored)
	if err != nil {
		return nil, errRecoveryAuthorization
	}
	failedState, err := readRecoveryAuthorizationSnapshot(ctx, failed)
	if err != nil {
		return nil, errRecoveryAuthorization
	}

	reasons := make(map[string]map[string]struct{})
	for username, restoredUser := range restoredState.users {
		failedUser, ok := failedState.users[username]
		if !ok {
			addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationUserStateReason)
			continue
		}
		if restoredUser.password != failedUser.password {
			addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationPasswordReason)
		}
		if restoredUser.id != failedUser.id || restoredUser.status != failedUser.status || restoredUser.deletedAt != failedUser.deletedAt {
			if restoredUser.status == 1 && !restoredUser.deletedAt.Valid && (failedUser.status != 1 || failedUser.deletedAt.Valid) {
				addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationReenableReason)
			} else {
				addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationUserStateReason)
			}
		}
	}
	for username := range failedState.users {
		if _, ok := restoredState.users[username]; !ok {
			addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationUserStateReason)
		}
	}

	changedRoleIDs := make(map[int64]struct{})
	for roleID, restoredRole := range restoredState.roles {
		failedRole, ok := failedState.roles[roleID]
		if !ok || restoredRole != failedRole {
			changedRoleIDs[roleID] = struct{}{}
		}
	}
	for roleID := range failedState.roles {
		if _, ok := restoredState.roles[roleID]; !ok {
			changedRoleIDs[roleID] = struct{}{}
		}
	}
	for roleID := range changedRoleIDs {
		for username := range restoredState.usernamesForRole(roleID) {
			addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationRolePermissionReason)
		}
		for username := range failedState.usernamesForRole(roleID) {
			addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationRolePermissionReason)
		}
	}

	changedUserIDs := make(map[int64]struct{})
	for binding := range restoredState.bindings {
		if _, ok := failedState.bindings[binding]; !ok {
			changedUserIDs[binding.userID] = struct{}{}
		}
	}
	for binding := range failedState.bindings {
		if _, ok := restoredState.bindings[binding]; !ok {
			changedUserIDs[binding.userID] = struct{}{}
		}
	}
	for userID := range changedUserIDs {
		if username, ok := restoredState.usersByID[userID]; ok {
			addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationRoleBindingReason)
		}
		if username, ok := failedState.usersByID[userID]; ok {
			addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationRoleBindingReason)
		}
	}

	permissionRoleIDs, allPermissionRoles := recoveryAuthorizationPermissionChanges(restoredState, failedState)
	if allPermissionRoles {
		for username := range restoredState.allBoundUsernames() {
			addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationRolePermissionReason)
		}
		for username := range failedState.allBoundUsernames() {
			addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationRolePermissionReason)
		}
	}
	for roleID := range permissionRoleIDs {
		for username := range restoredState.usernamesForRole(roleID) {
			addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationRolePermissionReason)
		}
		for username := range failedState.usernamesForRole(roleID) {
			addRecoveryAuthorizationImpact(reasons, username, recoveryAuthorizationRolePermissionReason)
		}
	}

	if len(reasons) == 0 {
		return nil, nil
	}
	usernames := make([]string, 0, len(reasons))
	for username := range reasons {
		if username != "" {
			usernames = append(usernames, username)
		}
	}
	sort.Strings(usernames)
	result := make([]string, 0, len(usernames))
	for _, username := range usernames {
		for _, reason := range []string{
			recoveryAuthorizationReenableReason,
			recoveryAuthorizationUserStateReason,
			recoveryAuthorizationRoleBindingReason,
			recoveryAuthorizationRolePermissionReason,
			recoveryAuthorizationPasswordReason,
		} {
			if _, ok := reasons[username][reason]; ok {
				result = append(result, fmt.Sprintf("用户 %q：%s", username, reason))
			}
		}
	}
	return result, nil
}

func addRecoveryAuthorizationImpact(reasons map[string]map[string]struct{}, username, reason string) {
	if username == "" {
		return
	}
	userReasons := reasons[username]
	if userReasons == nil {
		userReasons = make(map[string]struct{})
		reasons[username] = userReasons
	}
	userReasons[reason] = struct{}{}
}

type recoveryAuthorizationUser struct {
	id        int64
	password  string
	status    int64
	deletedAt sql.NullString
}

type recoveryAuthorizationRole struct {
	name      string
	status    int64
	dataScope int64
	deletedAt sql.NullString
}

type recoveryAuthorizationBinding struct {
	userID int64
	roleID int64
}

type recoveryAuthorizationRoleMenu struct {
	roleID int64
	menuID int64
}

type recoveryAuthorizationMenuAPI struct {
	menuID int64
	apiID  int64
}

type recoveryAuthorizationMenu struct {
	path       sql.NullString
	permission sql.NullString
	disable    int64
	typeID     int64
	deletedAt  sql.NullString
}

type recoveryAuthorizationAPI struct {
	path      sql.NullString
	method    sql.NullString
	deletedAt sql.NullString
}

type recoveryAuthorizationPolicy struct {
	ptype sql.NullString
	v0    sql.NullString
	v1    sql.NullString
	v2    sql.NullString
	v3    sql.NullString
	v4    sql.NullString
	v5    sql.NullString
}

type recoveryAuthorizationSnapshot struct {
	users         map[string]recoveryAuthorizationUser
	usersByID     map[int64]string
	roles         map[int64]recoveryAuthorizationRole
	bindings      map[recoveryAuthorizationBinding]struct{}
	roleUsernames map[int64]map[string]struct{}
	roleMenus     map[recoveryAuthorizationRoleMenu]struct{}
	menuAPIs      map[recoveryAuthorizationMenuAPI]struct{}
	menus         map[int64]recoveryAuthorizationMenu
	apis          map[int64]recoveryAuthorizationAPI
	policies      map[recoveryAuthorizationPolicy]struct{}
}

func (s recoveryAuthorizationSnapshot) usernamesForRole(roleID int64) map[string]struct{} {
	return s.roleUsernames[roleID]
}

func (s recoveryAuthorizationSnapshot) allBoundUsernames() map[string]struct{} {
	usernames := make(map[string]struct{})
	for _, roleUsernames := range s.roleUsernames {
		for username := range roleUsernames {
			usernames[username] = struct{}{}
		}
	}
	return usernames
}

func recoveryAuthorizationPermissionChanges(restored, failed recoveryAuthorizationSnapshot) (map[int64]struct{}, bool) {
	changedRoles := make(map[int64]struct{})
	changedMenus := make(map[int64]struct{})
	changedAPIs := make(map[int64]struct{})
	allRoles := false
	for menuID, restoredMenu := range restored.menus {
		failedMenu, ok := failed.menus[menuID]
		if !ok || restoredMenu != failedMenu {
			changedMenus[menuID] = struct{}{}
		}
	}
	for menuID := range failed.menus {
		if _, ok := restored.menus[menuID]; !ok {
			changedMenus[menuID] = struct{}{}
		}
	}
	for apiID, restoredAPI := range restored.apis {
		failedAPI, ok := failed.apis[apiID]
		if !ok || restoredAPI != failedAPI {
			changedAPIs[apiID] = struct{}{}
		}
	}
	for apiID := range failed.apis {
		if _, ok := restored.apis[apiID]; !ok {
			changedAPIs[apiID] = struct{}{}
		}
	}
	for binding := range restored.roleMenus {
		if _, ok := failed.roleMenus[binding]; !ok {
			changedRoles[binding.roleID] = struct{}{}
		}
	}
	for binding := range failed.roleMenus {
		if _, ok := restored.roleMenus[binding]; !ok {
			changedRoles[binding.roleID] = struct{}{}
		}
	}
	for binding := range restored.menuAPIs {
		if _, ok := failed.menuAPIs[binding]; !ok {
			changedMenus[binding.menuID] = struct{}{}
		}
	}
	for binding := range failed.menuAPIs {
		if _, ok := restored.menuAPIs[binding]; !ok {
			changedMenus[binding.menuID] = struct{}{}
		}
	}
	for apiID := range changedAPIs {
		found := false
		for binding := range restored.menuAPIs {
			if binding.apiID == apiID {
				changedMenus[binding.menuID] = struct{}{}
				found = true
			}
		}
		for binding := range failed.menuAPIs {
			if binding.apiID == apiID {
				changedMenus[binding.menuID] = struct{}{}
				found = true
			}
		}
		if !found {
			allRoles = true
		}
	}
	for menuID := range changedMenus {
		found := false
		for binding := range restored.roleMenus {
			if binding.menuID == menuID {
				changedRoles[binding.roleID] = struct{}{}
				found = true
			}
		}
		for binding := range failed.roleMenus {
			if binding.menuID == menuID {
				changedRoles[binding.roleID] = struct{}{}
				found = true
			}
		}
		if !found {
			allRoles = true
		}
	}

	changedPolicies := false
	for policy := range restored.policies {
		if _, ok := failed.policies[policy]; !ok {
			changedPolicies = true
			if roleID, ok := recoveryAuthorizationPolicyRoleID(policy); ok {
				changedRoles[roleID] = struct{}{}
			} else {
				allRoles = true
			}
		}
	}
	for policy := range failed.policies {
		if _, ok := restored.policies[policy]; !ok {
			changedPolicies = true
			if roleID, ok := recoveryAuthorizationPolicyRoleID(policy); ok {
				changedRoles[roleID] = struct{}{}
			} else {
				allRoles = true
			}
		}
	}
	if changedPolicies && len(changedRoles) == 0 {
		allRoles = true
	}
	return changedRoles, allRoles
}

func recoveryAuthorizationPolicyRoleID(policy recoveryAuthorizationPolicy) (int64, bool) {
	if !policy.ptype.Valid || policy.ptype.String != "p" || !policy.v0.Valid || !strings.HasPrefix(policy.v0.String, "role_") {
		return 0, false
	}
	roleID, err := strconv.ParseInt(strings.TrimPrefix(policy.v0.String, "role_"), 10, 64)
	return roleID, err == nil && roleID > 0
}

func readRecoveryAuthorizationSnapshot(ctx context.Context, db *sql.DB) (recoveryAuthorizationSnapshot, error) {
	snapshot := recoveryAuthorizationSnapshot{
		users:         make(map[string]recoveryAuthorizationUser),
		usersByID:     make(map[int64]string),
		roles:         make(map[int64]recoveryAuthorizationRole),
		bindings:      make(map[recoveryAuthorizationBinding]struct{}),
		roleUsernames: make(map[int64]map[string]struct{}),
		roleMenus:     make(map[recoveryAuthorizationRoleMenu]struct{}),
		menuAPIs:      make(map[recoveryAuthorizationMenuAPI]struct{}),
		menus:         make(map[int64]recoveryAuthorizationMenu),
		apis:          make(map[int64]recoveryAuthorizationAPI),
		policies:      make(map[recoveryAuthorizationPolicy]struct{}),
	}
	rows, err := db.QueryContext(ctx, `SELECT id, username, password, status, deleted_at FROM sys_users`)
	if err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}
	for rows.Next() {
		var (
			id       int64
			username string
			user     recoveryAuthorizationUser
		)
		if err := rows.Scan(&id, &username, &user.password, &user.status, &user.deletedAt); err != nil {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, err
		}
		user.id = id
		if _, exists := snapshot.users[username]; exists {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, errRecoveryAuthorization
		}
		snapshot.users[username] = user
		snapshot.usersByID[id] = username
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return recoveryAuthorizationSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}

	rows, err = db.QueryContext(ctx, `SELECT id, name, status, data_scope, deleted_at FROM sys_role`)
	if err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}
	for rows.Next() {
		var (
			id   int64
			role recoveryAuthorizationRole
		)
		if err := rows.Scan(&id, &role.name, &role.status, &role.dataScope, &role.deletedAt); err != nil {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, err
		}
		if _, exists := snapshot.roles[id]; exists {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, errRecoveryAuthorization
		}
		snapshot.roles[id] = role
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return recoveryAuthorizationSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}

	rows, err = db.QueryContext(ctx, `SELECT user_id, role_id FROM sys_user_role`)
	if err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}
	for rows.Next() {
		var binding recoveryAuthorizationBinding
		if err := rows.Scan(&binding.userID, &binding.roleID); err != nil {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, err
		}
		snapshot.bindings[binding] = struct{}{}
		username, ok := snapshot.usersByID[binding.userID]
		if !ok {
			continue
		}
		usernames := snapshot.roleUsernames[binding.roleID]
		if usernames == nil {
			usernames = make(map[string]struct{})
			snapshot.roleUsernames[binding.roleID] = usernames
		}
		usernames[username] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return recoveryAuthorizationSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}

	rows, err = db.QueryContext(ctx, `SELECT id, path, permission, disable, type, deleted_at FROM sys_menu`)
	if err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}
	for rows.Next() {
		var (
			id   int64
			menu recoveryAuthorizationMenu
		)
		if err := rows.Scan(&id, &menu.path, &menu.permission, &menu.disable, &menu.typeID, &menu.deletedAt); err != nil {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, err
		}
		if _, exists := snapshot.menus[id]; exists {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, errRecoveryAuthorization
		}
		snapshot.menus[id] = menu
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return recoveryAuthorizationSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}

	rows, err = db.QueryContext(ctx, `SELECT id, path, method, deleted_at FROM sys_api`)
	if err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}
	for rows.Next() {
		var (
			id  int64
			api recoveryAuthorizationAPI
		)
		if err := rows.Scan(&id, &api.path, &api.method, &api.deletedAt); err != nil {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, err
		}
		if _, exists := snapshot.apis[id]; exists {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, errRecoveryAuthorization
		}
		snapshot.apis[id] = api
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return recoveryAuthorizationSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}

	rows, err = db.QueryContext(ctx, `SELECT role_id, menu_id FROM sys_role_menu`)
	if err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}
	for rows.Next() {
		var binding recoveryAuthorizationRoleMenu
		if err := rows.Scan(&binding.roleID, &binding.menuID); err != nil {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, err
		}
		snapshot.roleMenus[binding] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return recoveryAuthorizationSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}

	rows, err = db.QueryContext(ctx, `SELECT menu_id, api_id FROM sys_menu_api`)
	if err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}
	for rows.Next() {
		var binding recoveryAuthorizationMenuAPI
		if err := rows.Scan(&binding.menuID, &binding.apiID); err != nil {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, err
		}
		snapshot.menuAPIs[binding] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return recoveryAuthorizationSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}

	rows, err = db.QueryContext(ctx, `SELECT ptype, v0, v1, v2, v3, v4, v5 FROM sys_casbin_rule`)
	if err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}
	for rows.Next() {
		var policy recoveryAuthorizationPolicy
		if err := rows.Scan(&policy.ptype, &policy.v0, &policy.v1, &policy.v2, &policy.v3, &policy.v4, &policy.v5); err != nil {
			_ = rows.Close()
			return recoveryAuthorizationSnapshot{}, err
		}
		snapshot.policies[policy] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return recoveryAuthorizationSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return recoveryAuthorizationSnapshot{}, err
	}
	return snapshot, nil
}

func rejectKnownRecoveryAuthorizationTables(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type IN ('table', 'view')
		  AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return errRecoveryAuthorization
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return errRecoveryAuthorization
		}
		if knownRecoveryAuthorizationSecretTable(name) {
			return errRecoveryAuthorizationUnsupported
		}
	}
	if err := rows.Err(); err != nil {
		return errRecoveryAuthorization
	}
	return nil
}

func knownRecoveryAuthorizationSecretTable(name string) bool {
	compact := strings.ToLower(strings.Map(func(r rune) rune {
		switch r {
		case '_', '-', ' ', '.':
			return -1
		default:
			return r
		}
	}, name))
	if compact == "ak" || compact == "sk" {
		return true
	}
	for _, exact := range []string{"sysopenapiclient", "sysopenapiclientscope"} {
		if compact == exact {
			return true
		}
	}
	for _, marker := range []string{"accesskey", "apikey", "secretkey", "akcredential", "skcredential"} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func openRecoveryAuthorizationDatabase(ctx context.Context, sourcePath string) (*sql.DB, func(), error) {
	copyPath, temporaryDir, err := copyRecoverySQLiteFamily(sourcePath)
	if err != nil {
		return nil, func() {}, err
	}
	db, err := sql.Open("sqlite", recoveryReadOnlySQLiteDSN(copyPath))
	if err != nil {
		_ = os.RemoveAll(temporaryDir)
		return nil, func() {}, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		_ = os.RemoveAll(temporaryDir)
		return nil, func() {}, err
	}
	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(temporaryDir)
	}
	return db, cleanup, nil
}

func recoveryReadOnlySQLiteDSN(path string) string {
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	query := url.Values{}
	query.Set("mode", "ro")
	return (&url.URL{Scheme: "file", Path: uriPath, RawQuery: query.Encode()}).String()
}

type recoverySQLiteFamilyFile struct {
	suffix string
	path   string
	info   os.FileInfo
}

func copyRecoverySQLiteFamily(sourcePath string) (string, string, error) {
	if !filepath.IsAbs(sourcePath) {
		return "", "", errRecoveryAuthorization
	}
	sourcePath = filepath.Clean(sourcePath)
	family, err := listRecoverySQLiteFamily(sourcePath)
	if err != nil {
		return "", "", err
	}
	temporaryDir, err := os.MkdirTemp("", "uvp-recovery-authorization-")
	if err != nil {
		return "", "", err
	}
	removeTemporaryDir := func() {
		_ = os.RemoveAll(temporaryDir)
	}
	if err := protectConfigDir(temporaryDir, true); err != nil {
		removeTemporaryDir()
		return "", "", err
	}
	for _, file := range family {
		destination := filepath.Join(temporaryDir, filepath.Base(sourcePath)+file.suffix)
		if err := copyStableRecoverySQLiteFile(file, destination); err != nil {
			removeTemporaryDir()
			return "", "", err
		}
	}
	currentFamily, err := listRecoverySQLiteFamily(sourcePath)
	if err != nil || !sameRecoverySQLiteFamily(family, currentFamily) {
		removeTemporaryDir()
		if err != nil {
			return "", "", err
		}
		return "", "", errors.New("recovery SQLite family changed while copying")
	}
	return filepath.Join(temporaryDir, filepath.Base(sourcePath)), temporaryDir, nil
}

func listRecoverySQLiteFamily(sourcePath string) ([]recoverySQLiteFamilyFile, error) {
	var family []recoverySQLiteFamilyFile
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		path := sourcePath + suffix
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			if suffix == "" {
				return nil, errRecoveryAuthorization
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("recovery SQLite family contains a non-regular file")
		}
		family = append(family, recoverySQLiteFamilyFile{suffix: suffix, path: path, info: info})
	}
	return family, nil
}

func sameRecoverySQLiteFamily(left, right []recoverySQLiteFamilyFile) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].suffix != right[index].suffix || !sameRecoverySQLiteFileInfo(left[index].info, right[index].info) {
			return false
		}
	}
	return true
}

func sameRecoverySQLiteFileInfo(left, right os.FileInfo) bool {
	return left.Size() == right.Size() && left.Mode() == right.Mode() && left.ModTime() == right.ModTime()
}

func copyStableRecoverySQLiteFile(source recoverySQLiteFamilyFile, destination string) error {
	input, err := os.Open(source.path)
	if err != nil {
		return err
	}
	defer input.Close()
	inputInfo, err := input.Stat()
	if err != nil || !sameRecoverySQLiteFileInfo(source.info, inputInfo) {
		if err != nil {
			return err
		}
		return errors.New("recovery SQLite file changed before copying")
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(output, hash), input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	outputHash := hash.Sum(nil)
	sourceHash, err := hashRecoverySQLiteFile(source.path)
	if err != nil {
		return err
	}
	if !bytes.Equal(outputHash, sourceHash) {
		return errors.New("recovery SQLite file changed while copying")
	}
	currentInfo, err := os.Stat(source.path)
	if err != nil {
		return err
	}
	if !sameRecoverySQLiteFileInfo(source.info, currentInfo) {
		return errors.New("recovery SQLite file changed after copying")
	}
	return nil
}

func hashRecoverySQLiteFile(path string) ([]byte, error) {
	input, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, input); err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}
