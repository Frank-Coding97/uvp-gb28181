package app

import "context"

// SessionValidator is the small authentication boundary shared by middleware and services.
// Concrete persistence lives in app/service to avoid coupling the global package to GORM models.
type SessionValidatorInterface interface {
	ValidateSession(context.Context, string, uint) error
	TouchSession(context.Context, string) error
}
