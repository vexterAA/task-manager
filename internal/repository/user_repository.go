package repository

import "example.com/yourapp/internal/domain"

type UserRepository interface {
	GetByTelegramID(telegramUserID int64) (domain.User, error)
	CreateUser(user domain.User) (domain.User, error)
}
