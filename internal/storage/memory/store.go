package memory

import (
	"sort"
	"sync"
	"time"

	"example.com/yourapp/internal/domain"
	"example.com/yourapp/internal/storage"
)

type Store struct {
	mu               sync.Mutex
	nextUserID       int64
	nextTaskID       int64
	nextAttachmentID int64
	users            map[int64]domain.User
	tasks            map[int64]domain.Task
	attachments      map[int64]domain.Attachment
	sessions         map[int64]domain.UserSession
}

func New() *Store {
	return &Store{
		nextUserID:       1,
		nextTaskID:       1,
		nextAttachmentID: 1,
		users:            make(map[int64]domain.User),
		tasks:            make(map[int64]domain.Task),
		attachments:      make(map[int64]domain.Attachment),
		sessions:         make(map[int64]domain.UserSession),
	}
}

func (s *Store) ListUsers() ([]domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) CreateUser(u domain.User) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.Timezone == "" {
		u.Timezone = "UTC"
	}
	u.ID = s.nextUserID
	s.nextUserID++
	u.CreatedAt = time.Now().UTC()
	s.users[u.ID] = u
	return u, nil
}

func (s *Store) UpdateUser(u domain.User) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[u.ID]; !ok {
		return domain.User{}, storage.ErrNotFound
	}
	s.users[u.ID] = u
	return u, nil
}

func (s *Store) GetByTelegramID(telegramUserID int64) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.TelegramUserID == telegramUserID {
			return u, nil
		}
	}
	return domain.User{}, storage.ErrNotFound
}

func (s *Store) GetUserByID(id int64) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return domain.User{}, storage.ErrNotFound
	}
	return u, nil
}

func (s *Store) GetSession(userID int64) (domain.UserSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[userID]
	if !ok {
		return domain.UserSession{}, storage.ErrNotFound
	}
	return session, nil
}

func (s *Store) UpsertSession(session domain.UserSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session.UpdatedAt = time.Now().UTC()
	s.sessions[session.UserID] = session
	return nil
}

func (s *Store) DeleteSession(userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, userID)
	return nil
}

func (s *Store) ListTasks(userID int64, status string) ([]domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if t.UserID != userID {
			continue
		}
		if status != "" && t.Status != status {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) ListActive(userID int64) ([]domain.Task, error) {
	return s.ListTasks(userID, domain.TaskStatusActive)
}

func (s *Store) GetTask(id int64) (domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return domain.Task{}, storage.ErrNotFound
	}
	return t, nil
}

func (s *Store) GetByID(id int64) (domain.Task, error) {
	return s.GetTask(id)
}

func (s *Store) CreateTask(t domain.Task) (domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[t.UserID]; !ok {
		return domain.Task{}, storage.ErrNotFound
	}
	if t.Status == "" {
		t.Status = domain.TaskStatusActive
	}
	now := time.Now().UTC()
	t.ID = s.nextTaskID
	s.nextTaskID++
	t.CreatedAt = now
	t.UpdatedAt = now
	s.tasks[t.ID] = t
	return t, nil
}

func (s *Store) Create(task domain.Task) (domain.Task, error) {
	return s.CreateTask(task)
}

func (s *Store) MarkDone(id int64) (domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return domain.Task{}, storage.ErrNotFound
	}
	t.Status = domain.TaskStatusDone
	t.UpdatedAt = time.Now().UTC()
	s.tasks[id] = t
	return t, nil
}

func (s *Store) SetDue(id int64, dueAt *time.Time) (domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return domain.Task{}, storage.ErrNotFound
	}
	t.DueAt = dueAt
	t.UpdatedAt = time.Now().UTC()
	s.tasks[id] = t
	return t, nil
}

func (s *Store) SetRemind(id int64, remindAt *time.Time) (domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return domain.Task{}, storage.ErrNotFound
	}
	t.RemindAt = remindAt
	t.NotifiedAt = nil
	t.UpdatedAt = time.Now().UTC()
	s.tasks[id] = t
	return t, nil
}

func (s *Store) UpdateTask(t domain.Task) (domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[t.ID]; !ok {
		return domain.Task{}, storage.ErrNotFound
	}
	t.UpdatedAt = time.Now().UTC()
	s.tasks[t.ID] = t
	return t, nil
}

func (s *Store) ListDueForNotify(now time.Time) ([]domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now = now.UTC()
	out := make([]domain.Task, 0)
	for id, t := range s.tasks {
		if t.Status != domain.TaskStatusActive {
			continue
		}
		if t.RemindAt == nil || t.RemindAt.After(now) {
			continue
		}
		if t.NotifiedAt != nil {
			continue
		}
		t.NotifiedAt = &now
		t.UpdatedAt = now
		s.tasks[id] = t
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) CreateAttachment(a domain.Attachment) (domain.Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[a.TaskID]; !ok {
		return domain.Attachment{}, storage.ErrNotFound
	}
	a.ID = s.nextAttachmentID
	s.nextAttachmentID++
	s.attachments[a.ID] = a
	return a, nil
}

func (s *Store) ListAttachmentsByTaskID(taskID int64) ([]domain.Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Attachment, 0)
	for _, a := range s.attachments {
		if a.TaskID == taskID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) DeleteAttachmentsByTaskID(taskID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, a := range s.attachments {
		if a.TaskID == taskID {
			delete(s.attachments, id)
		}
	}
	return nil
}

func (s *Store) DeleteTask(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return storage.ErrNotFound
	}
	delete(s.tasks, id)
	for attID, a := range s.attachments {
		if a.TaskID == id {
			delete(s.attachments, attID)
		}
	}
	return nil
}

func (s *Store) Delete(id int64) error {
	return s.DeleteTask(id)
}
