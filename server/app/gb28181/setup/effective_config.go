package setup

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

type ConfigSource string

const (
	ConfigSourceDatabase ConfigSource = "database"
	ConfigSourceYAMLSeed ConfigSource = "yaml-seed"
	ConfigSourceInferred ConfigSource = "inferred"
	ConfigSourceMissing  ConfigSource = "missing"
)

type LegacyConfigReader interface {
	GetString(string) string
	GetInt(string) int
	GetStringSlice(string) []string
}

type EffectiveSIPConfig struct {
	DeploymentMode      DeploymentMode
	ListenIP            string
	AdvertiseIP         string
	AdvertiseIPInferred bool
	Port                int
	Domain              string
	ServerID            string
	Password            string
	Transport           []string
	Source              ConfigSource
}

type EffectiveConfigLoader struct {
	db        *gorm.DB
	legacy    LegacyConfigReader
	addresses InterfaceProvider
}

func NewEffectiveConfigLoader(db *gorm.DB, legacy LegacyConfigReader, addresses InterfaceProvider) *EffectiveConfigLoader {
	return &EffectiveConfigLoader{db: db, legacy: legacy, addresses: addresses}
}

func (l *EffectiveConfigLoader) Load(ctx context.Context) (EffectiveSIPConfig, error) {
	repo := NewSIPConfigRepository(l.db)
	row, err := repo.Get(ctx)
	if err != nil {
		return EffectiveSIPConfig{}, err
	}
	if row != nil {
		source := ConfigSourceDatabase
		if row.AdvertiseIPInferred {
			source = ConfigSourceInferred
		}
		return effectiveFromRow(row, l.transports(), source), nil
	}

	candidate := l.legacyCandidate()
	if !candidate.complete() {
		return EffectiveSIPConfig{Transport: l.transports(), Source: ConfigSourceMissing}, nil
	}
	row = &SIPConfig{
		ID:                  SingletonID,
		DeploymentMode:      candidate.DeploymentMode,
		ListenIP:            candidate.ListenIP,
		AdvertiseIP:         candidate.AdvertiseIP,
		AdvertiseIPInferred: candidate.AdvertiseIPInferred,
		Port:                candidate.Port,
		Domain:              candidate.Domain,
		ServerID:            candidate.ServerID,
		Password:            candidate.Password,
	}
	if err := repo.Save(ctx, row); err != nil {
		return EffectiveSIPConfig{}, err
	}
	source := ConfigSourceYAMLSeed
	if row.AdvertiseIPInferred {
		source = ConfigSourceInferred
	}
	return effectiveFromRow(row, l.transports(), source), nil
}

func (l *EffectiveConfigLoader) legacyCandidate() EffectiveSIPConfig {
	if l.legacy == nil {
		return EffectiveSIPConfig{Source: ConfigSourceMissing}
	}
	mode := DeploymentMode(l.legacy.GetString("gb28181.sip.deploymentmode"))
	if !mode.Valid() {
		mode = DeploymentLAN
	}
	listenIP := strings.TrimSpace(l.legacy.GetString("gb28181.sip.ip"))
	advertiseIP := strings.TrimSpace(l.legacy.GetString("gb28181.sip.advertiseip"))
	inferred := false
	if advertiseIP == "" && listenIP != "" && listenIP != wildcardIPv4 {
		advertiseIP = listenIP
	}
	if advertiseIP == "" && listenIP == wildcardIPv4 {
		addresses, err := EnumerateNetworkAddresses(l.addresses)
		if err == nil {
			for _, address := range addresses {
				if address.Recommended {
					advertiseIP = address.IP
					inferred = true
					break
				}
			}
		}
	}
	return EffectiveSIPConfig{
		DeploymentMode:      mode,
		ListenIP:            listenIP,
		AdvertiseIP:         advertiseIP,
		AdvertiseIPInferred: inferred,
		Port:                l.legacy.GetInt("gb28181.sip.port"),
		Domain:              strings.TrimSpace(l.legacy.GetString("gb28181.sip.domain")),
		ServerID:            strings.TrimSpace(l.legacy.GetString("gb28181.sip.serverid")),
		Password:            l.legacy.GetString("gb28181.sip.password"),
		Transport:           l.transports(),
	}
}

func (l *EffectiveConfigLoader) transports() []string {
	if l.legacy != nil {
		if transports := l.legacy.GetStringSlice("gb28181.sip.transport"); len(transports) > 0 {
			return transports
		}
	}
	return []string{"udp", "tcp"}
}

func (c EffectiveSIPConfig) complete() bool {
	return c.DeploymentMode.Valid() && c.ListenIP != "" && c.AdvertiseIP != "" &&
		c.Port > 0 && c.Domain != "" && c.ServerID != "" && c.Password != ""
}

func effectiveFromRow(row *SIPConfig, transports []string, source ConfigSource) EffectiveSIPConfig {
	return EffectiveSIPConfig{
		DeploymentMode:      row.DeploymentMode,
		ListenIP:            row.ListenIP,
		AdvertiseIP:         row.AdvertiseIP,
		AdvertiseIPInferred: row.AdvertiseIPInferred,
		Port:                row.Port,
		Domain:              row.Domain,
		ServerID:            row.ServerID,
		Password:            row.Password,
		Transport:           transports,
		Source:              source,
	}
}
