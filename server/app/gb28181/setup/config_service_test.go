package setup

import (
	"context"
	"net"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newConfigTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SIPConfig{}))
	return db
}

// newSIPConfigService 注入「只有一张真实网卡」的 provider。
//
// ⛔ 别在用例里直接用 NewSIPConfigService 的默认 provider：三个媒体地址是否必填
// 取决于选中的是不是虚拟网卡，用真机网卡会让结果随跑测机器而变。
func newSIPConfigService(t *testing.T, db *gorm.DB) *SIPConfigService {
	t.Helper()
	return NewSIPConfigService(db).WithInterfaces(fakeInterfaceProvider{
		interfaces: []net.Interface{{Index: 1, Name: "en0", Flags: net.FlagUp}},
		addresses:  map[int][]net.Addr{1: {mustCIDR(t, "192.168.1.10/24")}},
	})
}

// validSaveRequest 的 ListenIP 刻意用**具体真实网卡**：这是新规则下的「最小合法请求」
// （三个媒体地址都允许留空）。需要通配地址 0.0.0.0 的用例自己覆盖 ListenIP。
func validSaveRequest(password *string) SaveSIPConfigRequest {
	return SaveSIPConfigRequest{
		DeploymentMode: DeploymentLAN,
		ListenIP:       "192.168.1.10",
		AdvertiseIP:    "192.168.1.10",
		SDPIP:          "192.168.1.10",
		Port:           5061,
		Domain:         "3402000000",
		ServerID:       "34020000002000000001",
		Password:       password,
	}
}

func TestSIPConfigService_SaveNewConfig(t *testing.T) {
	db := newConfigTestDB(t)
	svc := newSIPConfigService(t, db)
	password := "Sec12345Aa!!"

	view, err := svc.Save(context.Background(), validSaveRequest(&password))
	require.NoError(t, err)
	require.True(t, view.HasPassword)
	require.Equal(t, "192.168.1.10", view.AdvertiseIP)

	row, err := NewSIPConfigRepository(db).Get(context.Background())
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, "Sec12345Aa!!", row.Password)
}

func TestSIPConfigService_CustomDomainAndEightCharacterPassword(t *testing.T) {
	db := newConfigTestDB(t)
	svc := newSIPConfigService(t, db)
	password := "K9#nT2xQ"
	req := validSaveRequest(&password)
	req.Domain = "4401000000"
	_, err := svc.Save(context.Background(), req)
	require.NoError(t, err)
	row, err := NewSIPConfigRepository(db).Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, req.Domain, row.Domain)
	require.Equal(t, password, row.Password)
}

func TestSIPConfigService_EditPreservesPassword(t *testing.T) {
	db := newConfigTestDB(t)
	svc := newSIPConfigService(t, db)
	password := "Sec12345Aa!!"
	_, err := svc.Save(context.Background(), validSaveRequest(&password))
	require.NoError(t, err)

	req := validSaveRequest(nil)
	req.Port = 5062
	view, err := svc.Save(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 5062, view.Port)

	row, err := NewSIPConfigRepository(db).Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Sec12345Aa!!", row.Password)
}

func TestSIPConfigService_NewConfigRequiresPassword(t *testing.T) {
	db := newConfigTestDB(t)
	_, err := newSIPConfigService(t, db).Save(context.Background(), validSaveRequest(nil))
	require.ErrorIs(t, err, ErrSIPPasswordRequired)

	var count int64
	require.NoError(t, db.Model(&SIPConfig{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestSIPConfigService_ViewDoesNotExposePassword(t *testing.T) {
	db := newConfigTestDB(t)
	password := "Sec12345Aa!!"
	svc := newSIPConfigService(t, db)
	_, err := svc.Save(context.Background(), validSaveRequest(&password))
	require.NoError(t, err)

	view, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.NotNil(t, view)
	require.True(t, view.HasPassword)
}
