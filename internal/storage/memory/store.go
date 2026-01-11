package memory

import (
	"sort"
	"sync"
	"time"

	"example.com/yourapp/internal/domain"
	"example.com/yourapp/internal/storage"
)

type Store struct {
	mu         sync.Mutex
	nextUserID int64
	nextTaskID int64
	users      map[int64]domain.User
	tasks      map[int64]domain.Task
}

func New() *Store {
	return &Store{
		nextUserID: 1,
		nextTaskID: 1,
		users:      make(map[int64]domain.User),
		tasks:      make(map[int64]domain.Task),
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

func (s *Store) GetTask(id int64) (domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return domain.Task{}, storage.ErrNotFound
	}
	return t, nil
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

func (s *Store) DeleteTask(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return storage.ErrNotFound
	}
	delete(s.tasks, id)
	return nil
}
