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
	Port                int
	Domain              string
	ServerID            string
	Password            *string
	// Media hosts are consumed only by the standalone first-install transaction.
	// The regular SIP configuration service deliberately does not persist them.
	MediaReceiveHost  string
	MediaPlaybackHost string
}

// SIPConfigView 平台内部展示用视图.
// Password 明文回传给已认证 + 有 SIP 配置权限的调用方 —— 用户需要抄给设备录入.
// 认证/权限拦在 controller 层,该 view 本身不做过滤.
type SIPConfigView struct {
	DeploymentMode      DeploymentMode `json:"deploymentMode"`
	ListenIP            string         `json:"listenIp"`
	AdvertiseIP         string         `json:"advertiseIp"`
	AdvertiseIPInferred bool           `json:"advertiseIpInferred"`
	Port                int            `json:"port"`
	Domain              string         `json:"domain"`
	ServerID            string         `json:"serverId"`
	Password            string         `json:"password"`
	HasPassword         bool           `json:"hasPassword"`
}

type SIPConfigService struct {
	db *gorm.DB
}

func NewSIPConfigService(db *gorm.DB) *SIPConfigService {
	return &SIPConfigService{db: db}
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
		if err := ValidateSIPConfigRequest(req, current != nil && current.Password != ""); err != nil {
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
		Port:                row.Port,
		Domain:              row.Domain,
		ServerID:            row.ServerID,
		Password:            row.Password,
		HasPassword:         row.Password != "",
	}
}
