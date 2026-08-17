package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
)

var ErrCurrentSessionForceLogout = errors.New("cannot force logout current session")

type OnlineSessionFilter struct {
	PageNum      int
	PageSize     int
	Username     string
	DepartmentID uint
	ClientIP     string
	Status       string
}

type OnlineSessionView struct {
	SID              string    `gorm:"column:sid" json:"sid"`
	UserID           uint      `gorm:"column:user_id" json:"userId"`
	Username         string    `gorm:"column:username" json:"username"`
	NickName         string    `gorm:"column:nick_name" json:"nickName"`
	DepartmentID     uint      `gorm:"column:department_id" json:"departmentId"`
	DepartmentName   string    `gorm:"column:department_name" json:"departmentName"`
	ClientIP         string    `gorm:"column:client_ip" json:"clientIp"`
	LoginLocation    string    `gorm:"column:login_location" json:"loginLocation"`
	Browser          string    `gorm:"column:browser" json:"browser"`
	OS               string    `gorm:"column:os" json:"os"`
	LoginAt          time.Time `gorm:"column:login_at" json:"loginAt"`
	LastActiveAt     time.Time `gorm:"column:last_active_at" json:"lastActiveAt"`
	SessionExpiresAt time.Time `gorm:"column:session_expires_at" json:"sessionExpiresAt"`
	Status           string    `gorm:"column:status" json:"status"`
}

type ForceLogoutResult struct {
	Revoked  bool
	Username string
	ClientIP string
	SID      string
}

func (s *AuthSessionService) ListOnline(ctx context.Context, filter OnlineSessionFilter) ([]OnlineSessionView, int64, error) {
	now := s.now()
	cutoff := now.Add(-s.activeWindow)
	pageNum, pageSize := filter.PageNum, filter.PageSize
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	base := s.db.WithContext(ctx).Table("sys_user_sessions AS sessions").
		Joins("JOIN sys_users AS users ON users.id = sessions.user_id AND users.deleted_at IS NULL AND users.status = ?", 1).
		Joins("LEFT JOIN sys_department AS departments ON departments.id = users.dept_id AND departments.deleted_at IS NULL").
		Where("sessions.revoked_at IS NULL AND sessions.session_expires_at > ?", now)
	if username := strings.TrimSpace(filter.Username); username != "" {
		base = base.Where("users.username LIKE ?", "%"+username+"%")
	}
	if filter.DepartmentID != 0 {
		base = base.Where("users.dept_id = ?", filter.DepartmentID)
	}
	if clientIP := strings.TrimSpace(filter.ClientIP); clientIP != "" {
		base = base.Where("sessions.client_ip = ?", clientIP)
	}
	switch filter.Status {
	case "active":
		base = base.Where("sessions.last_active_at >= ?", cutoff)
	case "idle":
		base = base.Where("sessions.last_active_at < ?", cutoff)
	case "":
	default:
		return nil, 0, fmt.Errorf("invalid online session status %q", filter.Status)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("%w: count online sessions: %v", ErrSessionStore, err)
	}
	var list []OnlineSessionView
	selectFields := []string{
		"sessions.sid", "sessions.user_id", "users.username", "users.nick_name",
		"users.dept_id AS department_id", "COALESCE(departments.name, '') AS department_name",
		"sessions.client_ip", "sessions.login_location", "sessions.browser", "sessions.os",
		"sessions.login_at", "sessions.last_active_at", "sessions.session_expires_at",
		"CASE WHEN sessions.last_active_at >= ? THEN 'active' ELSE 'idle' END AS status",
	}
	err := base.Select(strings.Join(selectFields, ", "), cutoff).
		Order("sessions.login_at DESC").Offset((pageNum - 1) * pageSize).Limit(pageSize).Scan(&list).Error
	if err != nil {
		return nil, 0, fmt.Errorf("%w: list online sessions: %v", ErrSessionStore, err)
	}
	return list, total, nil
}

func (s *AuthSessionService) ForceLogout(ctx context.Context, targetSID, currentSID string, actorID uint) (*ForceLogoutResult, error) {
	if targetSID == "" {
		return nil, ErrSessionUnavailable
	}
	if targetSID == currentSID {
		return nil, ErrCurrentSessionForceLogout
	}
	var row struct {
		models.SysUserSession
		Username string
	}
	err := s.db.WithContext(ctx).Table("sys_user_sessions AS sessions").
		Select("sessions.*, users.username").
		Joins("LEFT JOIN sys_users AS users ON users.id = sessions.user_id").
		Where("sessions.sid = ?", targetSID).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionUnavailable
		}
		return nil, fmt.Errorf("%w: load force-logout target: %v", ErrSessionStore, err)
	}
	result := &ForceLogoutResult{Username: row.Username, ClientIP: row.ClientIP, SID: row.SID}
	if row.RevokedAt != nil || !row.SessionExpiresAt.After(s.now()) {
		return result, nil
	}
	revoked, err := s.Revoke(ctx, targetSID, "forced", &actorID)
	if err != nil {
		return nil, err
	}
	result.Revoked = revoked
	return result, nil
}
