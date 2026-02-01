package repository

import "example.com/yourapp/internal/domain"

type AttachmentRepository interface {
	CreateAttachment(attachment domain.Attachment) (domain.Attachment, error)
	ListAttachmentsByTaskID(taskID int64) ([]domain.Attachment, error)
	DeleteAttachmentsByTaskID(taskID int64) error
}
