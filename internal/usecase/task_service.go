package usecase

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"example.com/yourapp/internal/domain"
	"example.com/yourapp/internal/repository"
)

var (
	ErrInvalidText     = errors.New("task text is empty")
	ErrInvalidTimezone = errors.New("invalid timezone")
)

type TaskService struct {
	repo repository.TaskRepository
	now  func() time.Time
}

func NewTaskService(repo repository.TaskRepository) *TaskService {
	return &TaskService{
		repo: repo,
		now:  time.Now,
	}
}

func (s *TaskService) Create(userID int64, text string, dueAt, remindAt *time.Time, tz string) (domain.Task, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return domain.Task{}, ErrInvalidText
	}
	loc, err := locationFromTZ(tz)
	if err != nil {
		return domain.Task{}, err
	}
	task := domain.Task{
		UserID:   userID,
		Text:     trimmed,
		Status:   domain.TaskStatusActive,
		DueAt:    toUTC(dueAt),
		RemindAt: toUTC(remindAt),
	}
	created, err := s.repo.Create(task)
	if err != nil {
		return domain.Task{}, err
	}
	return toLocation(created, loc), nil
}

func (s *TaskService) ListActive(userID int64, tz string) ([]domain.Task, error) {
	loc, err := locationFromTZ(tz)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListActive(userID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i] = toLocation(items[i], loc)
	}
	return items, nil
}

func (s *TaskService) GetByID(id int64, tz string) (domain.Task, error) {
	loc, err := locationFromTZ(tz)
	if err != nil {
		return domain.Task{}, err
	}
	item, err := s.repo.GetByID(id)
	if err != nil {
		return domain.Task{}, err
	}
	return toLocation(item, loc), nil
}

func (s *TaskService) MarkDone(id int64, tz string) (domain.Task, error) {
	loc, err := locationFromTZ(tz)
	if err != nil {
		return domain.Task{}, err
	}
	item, err := s.repo.MarkDone(id)
	if err != nil {
		return domain.Task{}, err
	}
	return toLocation(item, loc), nil
}

func (s *TaskService) Delete(id int64) error {
	return s.repo.Delete(id)
}

func (s *TaskService) SetDue(id int64, dueAt *time.Time, tz string) (domain.Task, error) {
	loc, err := locationFromTZ(tz)
	if err != nil {
		return domain.Task{}, err
	}
	item, err := s.repo.SetDue(id, toUTC(dueAt))
	if err != nil {
		return domain.Task{}, err
	}
	return toLocation(item, loc), nil
}

func (s *TaskService) SetRemind(id int64, remindAt *time.Time, tz string) (domain.Task, error) {
	loc, err := locationFromTZ(tz)
	if err != nil {
		return domain.Task{}, err
	}
	item, err := s.repo.SetRemind(id, toUTC(remindAt))
	if err != nil {
		return domain.Task{}, err
	}
	return toLocation(item, loc), nil
}

func (s *TaskService) ListDueForNotify(now time.Time) ([]domain.Task, error) {
	return s.repo.ListDueForNotify(now.UTC())
}

func toUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	tt := t.UTC()
	return &tt
}

func toLocation(t domain.Task, loc *time.Location) domain.Task {
	t.CreatedAt = t.CreatedAt.In(loc)
	t.UpdatedAt = t.UpdatedAt.In(loc)
	t.DueAt = timeInLocation(t.DueAt, loc)
	t.RemindAt = timeInLocation(t.RemindAt, loc)
	t.NotifiedAt = timeInLocation(t.NotifiedAt, loc)
	return t
}

func timeInLocation(t *time.Time, loc *time.Location) *time.Time {
	if t == nil {
		return nil
	}
	tt := t.In(loc)
	return &tt
}

func locationFromTZ(tz string) (*time.Location, error) {
	if tz == "" || tz == "UTC" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err == nil {
		return loc, nil
	}
	if loc, ok := parseOffsetLocation(tz); ok {
		return loc, nil
	}
	return nil, ErrInvalidTimezone
}

func parseOffsetLocation(tz string) (*time.Location, bool) {
	if len(tz) != 6 || (tz[0] != '+' && tz[0] != '-') || tz[3] != ':' {
		return nil, false
	}
	hours, err := strconv.Atoi(tz[1:3])
	if err != nil || hours > 23 {
		return nil, false
	}
	minutes, err := strconv.Atoi(tz[4:6])
	if err != nil || minutes > 59 {
		return nil, false
	}
	offset := hours*3600 + minutes*60
	if tz[0] == '-' {
		offset = -offset
	}
	return time.FixedZone(tz, offset), true
}
