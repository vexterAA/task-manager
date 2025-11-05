package memory

type Store struct {
	users []string
}

func New() *Store {
	return &Store{users: make([]string, 0, 16)}
}

func (s *Store) ListUsers() ([]string, error) {
	out := make([]string, len(s.users))
	copy(out, s.users)
	return out, nil
}

func (s *Store) CreateUser(name string) error {
	s.users = append(s.users, name)
	return nil
}
