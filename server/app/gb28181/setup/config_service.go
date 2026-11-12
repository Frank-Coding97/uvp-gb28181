package setup

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

var ErrSIPPasswordRequired = errors.New("sip password is required")

type SaveSIPConfigRequest struct {
	DeploymentMode      DeploymentMode
	ListenIP            string
	AdvertiseIP         string
	AdvertiseIPInferred bool
	HookIP              string
	SDPIP               string
	StreamIP            string
	Port                int
	Domain              string
	ServerID            string
	Password            *string
}

// SIPConfigView 平台内部展示用视图.
// Password 明文回传给已认证 + 有 SIP 配置权限的调用方 —— 用户需要抄给设备录入.
// 认证/权限拦在 controller 层,该 view 本身不做过滤.
type SIPConfigView struct {
	DeploymentMode      DeploymentMode `json:"deploymentMode"`
	ListenIP            string         `json:"listenIp"`
	AdvertiseIP         string         `json:"advertiseIp"`
	AdvertiseIPInferred bool           `json:"advertiseIpInferred"`
	HookIP              string         `json:"hookIp"`
	SDPIP               string         `json:"sdpIp"`
	StreamIP            string         `json:"streamIp"`
	Port                int            `json:"port"`
	Domain              string         `json:"domain"`
	ServerID            string         `json:"serverId"`
	Password            string         `json:"password"`
	HasPassword         bool           `json:"hasPassword"`
}

type SIPConfigService struct {
	db         *gorm.DB
	interfaces InterfaceProvider
}

func NewSIPConfigService(db *gorm.DB) *SIPConfigService {
	return &SIPConfigService{db: db, interfaces: SystemInterfaceProvider{}}
}

// WithInterfaces 注入网卡枚举实现：controller 传自己那份 provider（与向导页同源），
// 测试传 fake —— 判定「选中的是不是虚拟网卡」要枚举本机网卡，不能靠机器环境决定用例结果。
func (s *SIPConfigService) WithInterfaces(provider InterfaceProvider) *SIPConfigService {
	if provider != nil {
		s.interfaces = provider
	}
	return s
}

func (s *SIPConfigService) Get(ctx context.Context) (*SIPConfigView, error) {
	row, err := NewSIPConfigRepository(s.db).Get(ctx)
	if err != nil || row == nil {
		return nil, err
	}
	view := sipConfigView(row)
	return &view, nil
}

func (s *SIPConfigService) Save(ctx context.Context, req SaveSIPConfigRequest) (SIPConfigView, error) {
	var saved SIPConfig
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := NewSIPConfigRepository(tx)
		current, err := repo.Get(ctx)
		if err != nil {
			return err
		}
		// 三个媒体地址「是否必填」取决于当前选中的监听网卡：真实网卡可留空
		// （运行时自动跟随该网卡），虚拟网卡 / 0.0.0.0 必须手填。判据唯一实现在
		// ResolveMediaAddressPolicy，⛔ 别在这里另写一份条件。
		policy := ResolveMediaAddressPolicy(req.DeploymentMode, req.ListenIP, req.AdvertiseIP, s.interfaces)
		if err := ValidateSIPConfigRequest(req, current != nil && current.Password != "", policy.Requirements()); err != nil {
			return err
		}

		password := ""
		if current != nil {
			password = current.Password
		}
		if req.Password != nil {
			password = *req.Password
		}
		if password == "" {
			return ErrSIPPasswordRequired
		}

		saved = SIPConfig{
			ID:                  SingletonID,
			DeploymentMode:      req.DeploymentMode,
			ListenIP:            req.ListenIP,
			AdvertiseIP:         req.AdvertiseIP,
			AdvertiseIPInferred: req.AdvertiseIPInferred,
			HookIP:              req.HookIP,
			SDPIP:               req.SDPIP,
			StreamIP:            req.StreamIP,
			Port:                req.Port,
			Domain:              req.Domain,
			ServerID:            req.ServerID,
			Password:            password,
		}
		if current != nil {
			saved.CreatedAt = current.CreatedAt
		}
		return repo.Save(ctx, &saved)
	})
	if err != nil {
		return SIPConfigView{}, err
	}
	return sipConfigView(&saved), nil
}

func sipConfigView(row *SIPConfig) SIPConfigView {
	return SIPConfigView{
		DeploymentMode:      row.DeploymentMode,
		ListenIP:            row.ListenIP,
		AdvertiseIP:         row.AdvertiseIP,
		AdvertiseIPInferred: row.AdvertiseIPInferred,
		HookIP:              row.HookIP,
		SDPIP:               row.SDPIP,
		StreamIP:            row.StreamIP,
		Port:                row.Port,
		Domain:              row.Domain,
		ServerID:            row.ServerID,
		Password:            row.Password,
		HasPassword:         row.Password != "",
	}
}
