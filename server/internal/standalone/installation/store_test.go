package installation

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/util"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	gbzlmrepo "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

const installationCasbinModel = `
[request_definition]
r = sub, obj, act, dom

[policy_definition]
p = sub, obj, act, dom

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act) && (r.dom == p.dom || p.dom == "*")
`

func newInstallationDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "installation.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	_, err = sqlitebootstrap.Initialize(ctx, db)
	require.NoError(t, err)
	require.NoError(t, sqlitebootstrap.Migrate(ctx, db))
	return db, path
}

func installationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func countTable(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM "+table).Scan(&count).Error)
	return count
}

func TestCreateAdminPersistsHashRoleAndCasbinRelation(t *testing.T) {
	db, _ := newInstallationDB(t)
	ctx := installationContext(t)
	password := "Admin!Passw0rd#2026"

	state, err := NewStore(db).CreateAdmin(ctx, "first-admin", password)
	require.NoError(t, err)
	require.Equal(t, PhasePendingSIP, state.Phase)
	require.NotNil(t, state.AdministratorID)
	require.EqualValues(t, 1, *state.AdministratorID)

	var user models.User
	require.NoError(t, db.Unscoped().Where("id = ?", *state.AdministratorID).Take(&user).Error)
	require.NotEqual(t, password, user.Password)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)))
	require.Equal(t, "first-admin", user.Username)
	require.EqualValues(t, 1, user.Status)

	var role models.SysUserRole
	require.NoError(t, db.Where("user_id = ? AND role_id = ?", user.ID, 1).Take(&role).Error)

	var relation struct {
		PType string `gorm:"column:ptype"`
		V0    string `gorm:"column:v0"`
		V1    string `gorm:"column:v1"`
		V2    string `gorm:"column:v2"`
		V3    string `gorm:"column:v3"`
		V4    string `gorm:"column:v4"`
		V5    string `gorm:"column:v5"`
	}
	require.NoError(t, db.Table("sys_casbin_rule").Where("ptype = ? AND v0 = ?", "g", "user_1").Take(&relation).Error)
	require.Equal(t, "g", relation.PType)
	require.Equal(t, "user_1", relation.V0)
	require.Equal(t, "role_1", relation.V1)
	require.Equal(t, "*", relation.V2)
	require.Equal(t, "", relation.V3)
	require.Equal(t, "", relation.V4)
	require.Equal(t, "", relation.V5)

	read, err := NewStore(db).State(ctx)
	require.NoError(t, err)
	require.Equal(t, PhasePendingSIP, read.Phase)
	require.Equal(t, state.AdministratorID, read.AdministratorID)
}

func TestCreateAdminGrantsHomepageAdministratorPermissions(t *testing.T) {
	db, _ := newInstallationDB(t)
	ctx := installationContext(t)
	var guest models.SysRole
	require.NoError(t, db.Where("name = ? AND deleted_at IS NULL", "游客").Take(&guest).Error)
	guestMenuCount := countTable(t, db, fmt.Sprintf("sys_role_menu WHERE role_id = %d", guest.ID))
	guestPolicyCount := countTable(t, db, fmt.Sprintf("sys_casbin_rule WHERE ptype = 'p' AND v0 = 'role_%d'", guest.ID))

	state, err := NewStore(db).CreateAdmin(ctx, "first-admin", "Admin!Passw0rd#2026")
	require.NoError(t, err)
	require.NotNil(t, state.AdministratorID)

	enforcer := newInstallationCasbinEnforcer(t, db)
	subject := fmt.Sprintf("user_%d", *state.AdministratorID)
	for _, permission := range []string{
		"gb28181:home:view",
		"gb28181:home:layout:save",
		"gb28181:home:layout:reset",
		"gb28181:channel:recording:update",
		"gb28181:recording:view",
	} {
		var count int64
		require.NoError(t, db.Table("sys_role_menu rm").Joins("JOIN sys_menu m ON m.id = rm.menu_id").Where("rm.role_id = ? AND m.permission = ?", adminRoleID, permission).Count(&count).Error)
		require.EqualValues(t, 1, count, "system administrator menu permission %s", permission)
	}

	for _, request := range []struct {
		path   string
		method string
	}{
		{path: "/api/gb28181/home/layout", method: http.MethodGet},
		{path: "/api/gb28181/home/layout", method: http.MethodPut},
		{path: "/api/gb28181/home/layout", method: http.MethodDelete},
		{path: "/api/gb28181/home/summary", method: http.MethodGet},
		{path: "/api/gb28181/home/drilldown/play", method: http.MethodGet},
		{path: "/api/gb28181/home/drilldown/sip", method: http.MethodGet},
		{path: "/api/gb28181/home/drilldown/traffic", method: http.MethodGet},
		{path: "/api/gb28181/sip/dashboard/snapshot", method: http.MethodGet},
		{path: "/api/gb28181/sip/dashboard/stream", method: http.MethodGet},
		{path: "/api/gb28181/sip/platform", method: http.MethodGet},
		{path: "/api/gb28181/zlm/overview", method: http.MethodGet},
		{path: "/api/gb28181/device/list", method: http.MethodGet},
		{path: "/api/gb28181/device-mgmt/directory/tree", method: http.MethodGet},
		{path: "/api/gb28181/device-mgmt/channels", method: http.MethodGet},
		{path: "/api/gb28181/device/34020000001320000901/channels", method: http.MethodGet},
		{path: "/api/gb28181/play/34020000001320000901/34020000001320000132", method: http.MethodPost},
		{path: "/api/gb28181/play/test-stream", method: http.MethodDelete},
		{path: "/api/gb28181/device-mgmt/channel/1/cloud-recording", method: http.MethodPatch},
		{path: "/api/gb28181/cloud-recordings/files", method: http.MethodGet},
		{path: "/api/gb28181/cloud-recordings/files/1/access", method: http.MethodPost},
	} {
		allowed, err := enforcer.Enforce(subject, request.path, request.method, "*")
		require.NoError(t, err)
		require.True(t, allowed, "system administrator must be allowed %s %s", request.method, request.path)
	}
	for _, request := range []struct {
		path   string
		method string
	}{
		{path: "/api/gb28181/sip/dashboard/stream", method: http.MethodPost},
		{path: "/api/gb28181/sip/dashboard/stream/other", method: http.MethodGet},
	} {
		allowed, err := enforcer.Enforce(subject, request.path, request.method, "*")
		require.NoError(t, err)
		require.False(t, allowed, "homepage grant must remain exact for %s %s", request.method, request.path)
	}
	guestAllowed, err := enforcer.Enforce(fmt.Sprintf("role_%d", guest.ID), "/api/gb28181/sip/dashboard/stream", http.MethodGet, "*")
	require.NoError(t, err)
	require.False(t, guestAllowed, "guest must not inherit the administrator-only stream grant")
	require.Equal(t, guestMenuCount, countTable(t, db, fmt.Sprintf("sys_role_menu WHERE role_id = %d", guest.ID)))
	require.Equal(t, guestPolicyCount, countTable(t, db, fmt.Sprintf("sys_casbin_rule WHERE ptype = 'p' AND v0 = 'role_%d'", guest.ID)))
}

func newInstallationCasbinEnforcer(t *testing.T, db *gorm.DB) *casbin.Enforcer {
	t.Helper()
	m, err := model.NewModelFromString(installationCasbinModel)
	require.NoError(t, err)
	adapter, err := gormadapter.NewAdapterByDBUseTableName(db, "", "sys_casbin_rule")
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m, adapter)
	require.NoError(t, err)
	enforcer.AddNamedDomainMatchingFunc("g", "KeyMatch2", util.KeyMatch2)
	require.NoError(t, enforcer.LoadPolicy())
	return enforcer
}

func TestCreateAdminSerializesConcurrentFirstUse(t *testing.T) {
	db, path := newInstallationDB(t)
	other, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	otherRaw, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = otherRaw.Close() })
	ctx := installationContext(t)
	start := make(chan struct{})
	type result struct {
		state State
		err   error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i, client := range []*gorm.DB{db, other} {
		wg.Add(1)
		go func(client *gorm.DB, username string) {
			defer wg.Done()
			<-start
			state, err := NewStore(client).CreateAdmin(ctx, username, "Admin!Passw0rd#2026")
			results <- result{state: state, err: err}
		}(client, "admin-"+string(rune('a'+i)))
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	for result := range results {
		if result.err == nil {
			successes++
			require.Equal(t, PhasePendingSIP, result.state.Phase)
			continue
		}
		require.ErrorIs(t, result.err, ErrAlreadyInitialized)
	}
	require.Equal(t, 1, successes)
	require.EqualValues(t, 1, countTable(t, db, "sys_users"))
	require.EqualValues(t, 1, countTable(t, db, "sys_user_role"))
	require.EqualValues(t, 1, countTable(t, db, "sys_casbin_rule WHERE ptype = 'g' AND v0 LIKE 'user_%'"))
}

func TestCreateAdminRejectsInvalidCredentialsWithoutWriting(t *testing.T) {
	db, _ := newInstallationDB(t)
	store := NewStore(db)
	ctx := installationContext(t)

	for _, test := range []struct {
		name     string
		username string
		password string
	}{
		{name: "empty username", username: "", password: "Admin!Passw0rd#2026"},
		{name: "short password", username: "admin", password: "short"},
		{name: "bcrypt password limit", username: "admin", password: string(make([]byte, 73))},
		{name: "username database limit", username: string(make([]byte, 51)), password: "Admin!Passw0rd#2026"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.CreateAdmin(ctx, test.username, test.password)
			require.Error(t, err)
			require.Zero(t, countTable(t, db, "sys_users"))
		})
	}
}

func TestCreateAdminRollsBackUserRoleAndStateOnRelationFailure(t *testing.T) {
	db, _ := newInstallationDB(t)
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_installation_role BEFORE INSERT ON sys_user_role BEGIN SELECT RAISE(ABORT, 'role insert failed'); END`).Error)

	state, err := NewStore(db).CreateAdmin(installationContext(t), "first-admin", "Admin!Passw0rd#2026")
	require.Error(t, err)
	require.Zero(t, state.Phase)
	require.Zero(t, countTable(t, db, "sys_users"))
	require.Zero(t, countTable(t, db, "sys_user_role"))
	require.Zero(t, countTable(t, db, "sys_casbin_rule WHERE ptype = 'g' AND v0 LIKE 'user_%'"))
	state, err = NewStore(db).State(installationContext(t))
	require.NoError(t, err)
	require.Equal(t, PhasePendingAdmin, state.Phase)
}

func TestCreateAdminRollsBackAllWritesOnStateFailure(t *testing.T) {
	db, _ := newInstallationDB(t)
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_installation_state BEFORE UPDATE ON standalone_installation WHEN NEW.phase = 'pending_sip' BEGIN SELECT RAISE(ABORT, 'state update failed'); END`).Error)

	state, err := NewStore(db).CreateAdmin(installationContext(t), "first-admin", "Admin!Passw0rd#2026")
	require.Error(t, err)
	require.Zero(t, state.Phase)
	require.Zero(t, countTable(t, db, "sys_users"))
	require.Zero(t, countTable(t, db, "sys_user_role"))
	require.Zero(t, countTable(t, db, "sys_casbin_rule WHERE ptype = 'g' AND v0 LIKE 'user_%'"))
	require.Zero(t, countTable(t, db, "sys_role_menu WHERE role_id = 1 AND menu_id IN (SELECT id FROM sys_menu WHERE permission IN ('gb28181:home:view','gb28181:home:layout:save','gb28181:home:layout:reset'))"))
	require.Zero(t, countTable(t, db, "sys_casbin_rule WHERE ptype = 'p' AND v0 = 'role_1' AND v1 IN ('/api/gb28181/home/layout','/api/gb28181/home/summary','/api/gb28181/sip/dashboard/snapshot')"))
	state, err = NewStore(db).State(installationContext(t))
	require.NoError(t, err)
	require.Equal(t, PhasePendingAdmin, state.Phase)
}

func TestStateAndCreateAdminFailClosedWhenInstallationStateMissing(t *testing.T) {
	db, _ := newInstallationDB(t)
	require.NoError(t, db.Exec("DROP TABLE standalone_installation").Error)
	store := NewStore(db)

	_, err := store.State(installationContext(t))
	require.ErrorIs(t, err, ErrInvalidState)
	_, err = store.CreateAdmin(installationContext(t), "first-admin", "Admin!Passw0rd#2026")
	require.ErrorIs(t, err, ErrInvalidState)
	require.Zero(t, countTable(t, db, "sys_users"))
}

func TestCreateAdminFailsClosedWhenUsersExistWithoutInstallationState(t *testing.T) {
	db, _ := newInstallationDB(t)
	require.NoError(t, db.Exec("DROP TABLE standalone_installation").Error)
	require.NoError(t, db.Exec(`INSERT INTO sys_users(username,password,status,dept_id,nick_name,sex,description) VALUES('existing','existing-hash',1,1,'existing','1','existing user')`).Error)

	_, err := NewStore(db).CreateAdmin(installationContext(t), "first-admin", "Admin!Passw0rd#2026")
	require.ErrorIs(t, err, ErrInvalidState)
	require.EqualValues(t, 1, countTable(t, db, "sys_users"))
}

func TestCreateAdminFailsClosedForExistingUsersWithPendingState(t *testing.T) {
	db, _ := newInstallationDB(t)
	require.NoError(t, db.Exec(`INSERT INTO sys_users(username,password,status,dept_id,nick_name,sex,description) VALUES('existing','existing-hash',1,1,'existing','1','existing user')`).Error)
	store := NewStore(db)

	_, err := store.State(installationContext(t))
	require.ErrorIs(t, err, ErrInvalidState)
	_, err = store.CreateAdmin(installationContext(t), "first-admin", "Admin!Passw0rd#2026")
	require.ErrorIs(t, err, ErrInvalidState)
	require.EqualValues(t, 1, countTable(t, db, "sys_users"))
}

func TestCompleteSIPResumesAndPreservesExistingPassword(t *testing.T) {
	db, _ := newInstallationDB(t)
	require.NoError(t, db.Exec(`INSERT INTO gb_sip_config(id,deployment_mode,listen_ip,advertise_ip,advertise_ip_inferred,port,domain,server_id,password) VALUES(1,'lan','0.0.0.0','192.168.1.10',0,5060,'3402000000','34020000002000000001','Old!Passw0rd#2026')`).Error)
	store := NewStore(db)
	ctx := installationContext(t)

	state, err := store.CreateAdmin(ctx, "first-admin", "Admin!Passw0rd#2026")
	require.NoError(t, err)
	require.Equal(t, PhasePendingSIP, state.Phase)
	var before gbmodels.SIPConfig
	require.NoError(t, db.Where("id = ?", 1).Take(&before).Error)

	view, err := store.CompleteSIP(ctx, gbmodels.SaveSIPConfigRequest{
		DeploymentMode:    gbmodels.DeploymentLAN,
		ListenIP:          "0.0.0.0",
		AdvertiseIP:       "192.168.1.11",
		Port:              5061,
		Domain:            "3402000000",
		ServerID:          "34020000002000000001",
		MediaReceiveHost:  "192.168.1.20",
		MediaPlaybackHost: "192.168.1.21",
	})
	require.NoError(t, err)
	require.Equal(t, 5061, view.Port)
	require.Equal(t, "192.168.1.11", view.AdvertiseIP)
	state, err = store.State(ctx)
	require.NoError(t, err)
	require.Equal(t, PhaseComplete, state.Phase)
	require.NotNil(t, state.CompletedAt)

	var after gbmodels.SIPConfig
	require.NoError(t, db.Where("id = ?", 1).Take(&after).Error)
	require.Equal(t, "Old!Passw0rd#2026", after.Password)
	require.Equal(t, 5061, after.Port)
	require.Equal(t, "192.168.1.11", after.AdvertiseIP)
	require.Zero(t, countTable(t, db, "meta_node"), "legacy CompleteSIP must not seed a media node")

	require.NoError(t, db.Unscoped().Where("id = ?", *state.AdministratorID).Delete(&models.User{}).Error)
	read, err := store.State(ctx)
	require.NoError(t, err)
	require.Equal(t, PhaseComplete, read.Phase)
	require.Equal(t, state.AdministratorID, read.AdministratorID)
	_, err = store.CreateAdmin(ctx, "second-admin", "Admin!Passw0rd#2026")
	require.ErrorIs(t, err, ErrAlreadyInitialized)
}

func TestCompleteSIPWithMediaSeedsLocalNodeAtomically(t *testing.T) {
	db, _ := newInstallationDB(t)
	store := NewStore(db)
	ctx := installationContext(t)
	require.NoError(t, createPendingSIPAdmin(store, ctx))

	view, err := store.CompleteSIPWithMedia(ctx, validMediaSIPRequest(), localZLMConfig())
	require.NoError(t, err)
	require.Equal(t, 5061, view.Port)
	require.EqualValues(t, 1, countTable(t, db, "gb_sip_config"))
	require.EqualValues(t, 1, countTable(t, db, "meta_node"))

	var row gbzlmrepo.MetaNode
	require.NoError(t, db.Where("id = ?", 1).Take(&row).Error)
	require.Equal(t, "zlm-default", row.Name)
	require.Equal(t, "127.0.0.1", row.Host)
	require.Equal(t, "192.168.1.20", row.ReceiveHost)
	require.Equal(t, "192.168.1.21", row.PlaybackHost)
	require.Equal(t, 18080, row.APIPort)
	require.Equal(t, "zlm-secret", row.APISecret)
	require.Equal(t, "active", row.State)
	require.Equal(t, 30000, row.RTPPortStart)
	require.Equal(t, 35000, row.RTPPortEnd)
	_, err = uuid.Parse(row.MediaServerUUID)
	require.NoError(t, err)
}

func TestCompleteSIPWithMediaAcceptsLoopbackHosts(t *testing.T) {
	db, _ := newInstallationDB(t)
	store := NewStore(db)
	ctx := installationContext(t)
	require.NoError(t, createPendingSIPAdmin(store, ctx))
	req := validMediaSIPRequest()
	req.MediaReceiveHost = "127.0.0.1"
	req.MediaPlaybackHost = "127.0.0.2"

	_, err := store.CompleteSIPWithMedia(ctx, req, localZLMConfig())
	require.NoError(t, err)
	require.EqualValues(t, 1, countTable(t, db, "gb_sip_config"))
	require.EqualValues(t, 1, countTable(t, db, "meta_node"))
}

func TestCompleteSIPWithMediaRejectsInvalidMediaHostsWithoutWrites(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		mutate func(*gbmodels.SaveSIPConfigRequest)
	}{
		{name: "empty receive", field: "mediaReceiveHost", mutate: func(req *gbmodels.SaveSIPConfigRequest) { req.MediaReceiveHost = "" }},
		{name: "wildcard receive", field: "mediaReceiveHost", mutate: func(req *gbmodels.SaveSIPConfigRequest) { req.MediaReceiveHost = "0.0.0.0" }},
		{name: "multicast playback", field: "mediaPlaybackHost", mutate: func(req *gbmodels.SaveSIPConfigRequest) { req.MediaPlaybackHost = "224.0.0.1" }},
		{name: "IPv6 playback", field: "mediaPlaybackHost", mutate: func(req *gbmodels.SaveSIPConfigRequest) { req.MediaPlaybackHost = "2001:db8::1" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := newInstallationDB(t)
			store := NewStore(db)
			ctx := installationContext(t)
			require.NoError(t, createPendingSIPAdmin(store, ctx))
			req := validMediaSIPRequest()
			test.mutate(&req)

			_, err := store.CompleteSIPWithMedia(ctx, req, localZLMConfig())
			var validation *gbmodels.ValidationError
			require.ErrorAs(t, err, &validation)
			require.Contains(t, validation.Fields, test.field)
			require.Zero(t, countTable(t, db, "gb_sip_config"))
			require.Zero(t, countTable(t, db, "meta_node"))
			state, stateErr := store.State(ctx)
			require.NoError(t, stateErr)
			require.Equal(t, PhasePendingSIP, state.Phase)
		})
	}
}

func TestCompleteSIPWithMediaPreservesMatchingNodeAndRejectsConflict(t *testing.T) {
	t.Run("matching endpoint preserves node identity and addresses", func(t *testing.T) {
		db, _ := newInstallationDB(t)
		store := NewStore(db)
		ctx := installationContext(t)
		require.NoError(t, db.Create(&gbzlmrepo.MetaNode{
			Name:            "configured-by-user",
			Host:            "localhost",
			ReceiveHost:     "192.168.1.20",
			PlaybackHost:    "192.168.1.21",
			APIPort:         18080,
			APISecret:       "zlm-secret",
			MediaServerUUID: "existing-uuid",
			Weight:          80,
			State:           "maintenance",
		}).Error)
		require.NoError(t, createPendingSIPAdmin(store, ctx))

		_, err := store.CompleteSIPWithMedia(ctx, validMediaSIPRequest(), localZLMConfig())
		require.NoError(t, err)
		require.EqualValues(t, 1, countTable(t, db, "meta_node"))
		var row gbzlmrepo.MetaNode
		require.NoError(t, db.Where("id = ?", 1).Take(&row).Error)
		require.Equal(t, "configured-by-user", row.Name)
		require.Equal(t, "existing-uuid", row.MediaServerUUID)
		require.Equal(t, 80, row.Weight)
		require.Equal(t, "maintenance", row.State)
	})

	t.Run("matching endpoint rejects address conflict", func(t *testing.T) {
		db, _ := newInstallationDB(t)
		store := NewStore(db)
		ctx := installationContext(t)
		require.NoError(t, db.Create(&gbzlmrepo.MetaNode{
			Name:            "configured-by-user",
			Host:            "127.0.0.1",
			ReceiveHost:     "192.168.1.99",
			PlaybackHost:    "192.168.1.21",
			APIPort:         18080,
			APISecret:       "zlm-secret",
			MediaServerUUID: "existing-uuid",
			State:           "active",
		}).Error)
		require.NoError(t, createPendingSIPAdmin(store, ctx))

		_, err := store.CompleteSIPWithMedia(ctx, validMediaSIPRequest(), localZLMConfig())
		var validation *gbmodels.ValidationError
		require.ErrorAs(t, err, &validation)
		require.Contains(t, validation.Fields, "mediaReceiveHost")
		require.Zero(t, countTable(t, db, "gb_sip_config"))
		require.EqualValues(t, 1, countTable(t, db, "meta_node"))
		var row gbzlmrepo.MetaNode
		require.NoError(t, db.Where("id = ?", 1).Take(&row).Error)
		require.Equal(t, "192.168.1.99", row.ReceiveHost)
	})

	t.Run("multiple matching identities fail closed", func(t *testing.T) {
		db, _ := newInstallationDB(t)
		store := NewStore(db)
		ctx := installationContext(t)
		for _, mediaUUID := range []string{"existing-uuid-a", "existing-uuid-b"} {
			require.NoError(t, db.Create(&gbzlmrepo.MetaNode{
				Name:            "configured-by-user",
				Host:            "localhost",
				ReceiveHost:     "192.168.1.20",
				PlaybackHost:    "192.168.1.21",
				APIPort:         18080,
				APISecret:       "zlm-secret",
				MediaServerUUID: mediaUUID,
				State:           "active",
			}).Error)
		}
		require.NoError(t, createPendingSIPAdmin(store, ctx))

		_, err := store.CompleteSIPWithMedia(ctx, validMediaSIPRequest(), localZLMConfig())
		var validation *gbmodels.ValidationError
		require.ErrorAs(t, err, &validation)
		require.Contains(t, validation.Fields, "mediaNode")
		require.Zero(t, countTable(t, db, "gb_sip_config"))
		require.EqualValues(t, 2, countTable(t, db, "meta_node"))
	})
}

func TestCompleteSIPWithMediaRejectsUnmatchedExistingNodeWithoutWrites(t *testing.T) {
	db, _ := newInstallationDB(t)
	store := NewStore(db)
	ctx := installationContext(t)
	require.NoError(t, db.Create(&gbzlmrepo.MetaNode{
		Name:            "another-node",
		Host:            "192.168.1.50",
		APIPort:         18080,
		APISecret:       "other-secret",
		MediaServerUUID: "other-uuid",
		State:           "active",
	}).Error)
	require.NoError(t, createPendingSIPAdmin(store, ctx))

	_, err := store.CompleteSIPWithMedia(ctx, validMediaSIPRequest(), localZLMConfig())
	var validation *gbmodels.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Contains(t, validation.Fields, "mediaNode")
	require.Zero(t, countTable(t, db, "gb_sip_config"))
	require.EqualValues(t, 1, countTable(t, db, "meta_node"))
}

func TestCompleteSIPWithMediaRollsBackSIPAndNodeOnStateFailure(t *testing.T) {
	db, _ := newInstallationDB(t)
	store := NewStore(db)
	ctx := installationContext(t)
	require.NoError(t, createPendingSIPAdmin(store, ctx))
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_installation_complete_media BEFORE UPDATE ON standalone_installation WHEN NEW.phase = 'complete' BEGIN SELECT RAISE(ABORT, 'complete state update failed'); END`).Error)

	_, err := store.CompleteSIPWithMedia(ctx, validMediaSIPRequest(), localZLMConfig())
	require.Error(t, err)
	require.Zero(t, countTable(t, db, "gb_sip_config"))
	require.Zero(t, countTable(t, db, "meta_node"))
	state, stateErr := store.State(ctx)
	require.NoError(t, stateErr)
	require.Equal(t, PhasePendingSIP, state.Phase)
}

func TestCompleteSIPWithMediaSerializesConcurrentFirstUse(t *testing.T) {
	db, path := newInstallationDB(t)
	other, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	otherRaw, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = otherRaw.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	store := NewStore(db)
	require.NoError(t, createPendingSIPAdmin(store, ctx))

	start := make(chan struct{})
	type result struct{ err error }
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, client := range []*gorm.DB{db, other} {
		wg.Add(1)
		go func(client *gorm.DB) {
			defer wg.Done()
			<-start
			_, err := NewStore(client).CompleteSIPWithMedia(ctx, validMediaSIPRequest(), localZLMConfig())
			results <- result{err: err}
		}(client)
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	for result := range results {
		if result.err == nil {
			successes++
			continue
		}
		require.ErrorIs(t, result.err, ErrAlreadyInitialized)
	}
	require.Equal(t, 1, successes)
	require.EqualValues(t, 1, countTable(t, db, "gb_sip_config"))
	require.EqualValues(t, 1, countTable(t, db, "meta_node"))
}

func TestCompleteSIPWithMediaSurvivesDatabaseReopen(t *testing.T) {
	db, path := newInstallationDB(t)
	store := NewStore(db)
	ctx := installationContext(t)
	require.NoError(t, createPendingSIPAdmin(store, ctx))
	_, err := store.CompleteSIPWithMedia(ctx, validMediaSIPRequest(), localZLMConfig())
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	reopened, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	reopenedRaw, err := reopened.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopenedRaw.Close() })
	reopenedStore := NewStore(reopened)
	state, err := reopenedStore.State(ctx)
	require.NoError(t, err)
	require.Equal(t, PhaseComplete, state.Phase)
	require.EqualValues(t, 1, countTable(t, reopened, "meta_node"))
}

func createPendingSIPAdmin(store *Store, ctx context.Context) error {
	_, err := store.CreateAdmin(ctx, "first-admin", "Admin!Passw0rd#2026")
	return err
}

func validMediaSIPRequest() gbmodels.SaveSIPConfigRequest {
	return gbmodels.SaveSIPConfigRequest{
		DeploymentMode:    gbmodels.DeploymentLAN,
		ListenIP:          "0.0.0.0",
		AdvertiseIP:       "192.168.1.10",
		Port:              5061,
		Domain:            "3402000000",
		ServerID:          "34020000002000000001",
		Password:          stringPtr("Sip!Passw0rd#2026"),
		MediaReceiveHost:  "192.168.1.20",
		MediaPlaybackHost: "192.168.1.21",
	}
}

func localZLMConfig() gbconfig.ZLMConfig {
	return gbconfig.ZLMConfig{Host: "127.0.0.1", HTTPPort: 18080, Secret: "zlm-secret"}
}

func TestCompleteSIPRollsBackConfigAndStateOnStateFailure(t *testing.T) {
	db, _ := newInstallationDB(t)
	store := NewStore(db)
	ctx := installationContext(t)
	_, err := store.CreateAdmin(ctx, "first-admin", "Admin!Passw0rd#2026")
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_installation_complete BEFORE UPDATE ON standalone_installation WHEN NEW.phase = 'complete' BEGIN SELECT RAISE(ABORT, 'complete state update failed'); END`).Error)

	_, err = store.CompleteSIP(ctx, gbmodels.SaveSIPConfigRequest{
		DeploymentMode: gbmodels.DeploymentLAN,
		ListenIP:       "0.0.0.0",
		AdvertiseIP:    "192.168.1.10",
		Port:           5060,
		Domain:         "3402000000",
		ServerID:       "34020000002000000001",
		Password:       stringPtr("Sip!Passw0rd#2026"),
	})
	require.Error(t, err)
	require.Zero(t, countTable(t, db, "gb_sip_config"))
	read, err := store.State(ctx)
	require.NoError(t, err)
	require.Equal(t, PhasePendingSIP, read.Phase)
}

func TestCompleteSIPRejectsInvalidTransitionAndRepeat(t *testing.T) {
	db, _ := newInstallationDB(t)
	store := NewStore(db)
	ctx := installationContext(t)
	request := gbmodels.SaveSIPConfigRequest{Password: stringPtr("Sip!Passw0rd#2026")}

	_, err := store.CompleteSIP(ctx, request)
	require.ErrorIs(t, err, ErrInvalidState)
	_, err = store.CreateAdmin(ctx, "first-admin", "Admin!Passw0rd#2026")
	require.NoError(t, err)
	_, err = store.CompleteSIP(ctx, request)
	require.Error(t, err)
	_, err = store.CompleteSIP(ctx, gbmodels.SaveSIPConfigRequest{
		DeploymentMode: gbmodels.DeploymentLAN,
		ListenIP:       "0.0.0.0",
		AdvertiseIP:    "192.168.1.10",
		Port:           5060,
		Domain:         "3402000000",
		ServerID:       "34020000002000000001",
		Password:       stringPtr("Sip!Passw0rd#2026"),
	})
	require.NoError(t, err)
	state, err := store.State(ctx)
	require.NoError(t, err)
	require.Equal(t, PhaseComplete, state.Phase)
	_, err = store.CompleteSIP(ctx, request)
	require.ErrorIs(t, err, ErrAlreadyInitialized)
}

func stringPtr(value string) *string { return &value }
