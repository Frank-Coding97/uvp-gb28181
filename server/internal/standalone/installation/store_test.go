package installation

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

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
		DeploymentMode: gbmodels.DeploymentLAN,
		ListenIP:       "0.0.0.0",
		AdvertiseIP:    "192.168.1.11",
		Port:           5061,
		Domain:         "3402000000",
		ServerID:       "34020000002000000001",
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

	require.NoError(t, db.Unscoped().Where("id = ?", *state.AdministratorID).Delete(&models.User{}).Error)
	read, err := store.State(ctx)
	require.NoError(t, err)
	require.Equal(t, PhaseComplete, read.Phase)
	require.Equal(t, state.AdministratorID, read.AdministratorID)
	_, err = store.CreateAdmin(ctx, "second-admin", "Admin!Passw0rd#2026")
	require.ErrorIs(t, err, ErrAlreadyInitialized)
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
