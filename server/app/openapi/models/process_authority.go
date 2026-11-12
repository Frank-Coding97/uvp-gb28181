package models

import "time"

// ProcessGeneration is written once by the root's lifetime authority. It is
// retained across rollback; old IDs are never inferred or backfilled from time.
type ProcessGeneration struct {
	GenerationID string    `gorm:"column:generation_id;size:32;primaryKey;uniqueIndex:uk_openapi_generation_domain,priority:2" json:"-"`
	DomainID     string    `gorm:"column:domain_id;size:32;not null;uniqueIndex:uk_openapi_generation_domain,priority:1" json:"-"`
	StartedAt    time.Time `gorm:"column:started_at;not null" json:"-"`
}

func (ProcessGeneration) TableName() string { return "sys_openapi_process_generation" }

// ProcessAuthority is not a time-based lease. New generations require both
// the actual OS lifetime lock and an atomic current-generation transition.
type ProcessAuthority struct {
	ID                  int64             `gorm:"column:id;primaryKey;autoIncrement:false;check:ck_openapi_authority_singleton,id = 1 AND row_version > 0" json:"-"`
	DomainID            string            `gorm:"column:domain_id;size:32;not null" json:"-"`
	CurrentGenerationID string            `gorm:"column:current_generation_id;size:32;not null" json:"-"`
	RowVersion          int64             `gorm:"column:row_version;not null" json:"-"`
	Generation          ProcessGeneration `gorm:"foreignKey:DomainID,CurrentGenerationID;references:DomainID,GenerationID;constraint:OnDelete:RESTRICT,OnUpdate:RESTRICT" json:"-"`
}

func (ProcessAuthority) TableName() string { return "sys_openapi_process_authority" }
