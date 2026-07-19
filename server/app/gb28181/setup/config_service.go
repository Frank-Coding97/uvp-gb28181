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
}

type SIPConfigView struct {
	DeploymentMode      DeploymentMode `json:"deploymentMode"`
	ListenIP            string         `json:"listenIp"`
	AdvertiseIP         string         `json:"advertiseIp"`
	AdvertiseIPInferred bool           `json:"advertiseIpInferred"`
	Port                int            `json:"port"`
	Domain              string         `json:"domain"`
	ServerID            string         `json:"serverId"`
	HasPassword         bool           `json:"hasPassword"`
}

type SIPConfigService struct {
	db           *gorm.DB
	installation *InstallationService
}

func NewSIPConfigService(db *gorm.DB) *SIPConfigService {
	return &SIPConfigService{db: db, installation: NewInstallationService(db)}
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
		if err := repo.Save(ctx, &saved); err != nil {
			return err
		}
		return s.installation.completeWithDB(ctx, tx)
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
		HasPassword:         row.Password != "",
	}
}
