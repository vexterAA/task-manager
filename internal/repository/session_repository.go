package repository

import "example.com/yourapp/internal/domain"

type SessionRepository interface {
	GetSession(userID int64) (domain.UserSession, error)
	UpsertSession(session domain.UserSession) error
	DeleteSession(userID int64) error
}
