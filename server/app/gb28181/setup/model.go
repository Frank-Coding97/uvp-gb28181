package setup

import "time"

const SingletonID uint8 = 1

type DeploymentMode string

const (
	DeploymentLAN    DeploymentMode = "lan"
	DeploymentPublic DeploymentMode = "public"
)

func (m DeploymentMode) Valid() bool {
	return m == DeploymentLAN || m == DeploymentPublic
}

// SIPConfig is the persisted runtime configuration. Password never crosses JSON boundaries.
type SIPConfig struct {
	ID                  uint8          `gorm:"column:id;primaryKey;autoIncrement:false" json:"-"`
	DeploymentMode      DeploymentMode `gorm:"column:deployment_mode;size:8;not null" json:"deploymentMode"`
	ListenIP            string         `gorm:"column:listen_ip;size:45;not null" json:"listenIp"`
	AdvertiseIP         string         `gorm:"column:advertise_ip;size:45;not null" json:"advertiseIp"`
	AdvertiseIPInferred bool           `gorm:"column:advertise_ip_inferred;not null;default:false" json:"advertiseIpInferred"`
	Port                int            `gorm:"column:port;not null" json:"port"`
	Domain              string         `gorm:"column:domain;size:10;not null" json:"domain"`
	ServerID            string         `gorm:"column:server_id;size:20;not null" json:"serverId"`
	Password            string         `gorm:"column:password;size:255;not null" json:"-"`
	CreatedAt           time.Time      `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt           time.Time      `gorm:"column:updated_at" json:"updatedAt"`
}

func (SIPConfig) TableName() string { return "gb_sip_config" }
