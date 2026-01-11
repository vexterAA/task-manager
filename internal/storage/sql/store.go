package sqlstore

import (
	"database/sql"
	"errors"
	"time"

	"example.com/yourapp/internal/domain"
	"example.com/yourapp/internal/storage"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db *sql.DB
}

func New(driver, dsn string) *Store {
	var db *sql.DB
	if driver != "" && dsn != "" {
		db, _ = sql.Open(driver, dsn)
	}
	return &Store{db: db}
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (domain.Task, error) {
	var t domain.Task
	var dueAt, remindAt, notifiedAt sql.NullTime
	if err := scanner.Scan(
		&t.ID,
		&t.UserID,
		&t.Text,
		&t.Status,
		&dueAt,
		&remindAt,
		&notifiedAt,
		&t.CreatedAt,
		&t.UpdatedAt,
	); err != nil {
		return domain.Task{}, err
	}
	if dueAt.Valid {
		t.DueAt = &dueAt.Time
	}
	if remindAt.Valid {
		t.RemindAt = &remindAt.Time
	}
	if notifiedAt.Valid {
		t.NotifiedAt = &notifiedAt.Time
	}
	return t, nil
}

func (s *Store) ListUsers() ([]domain.User, error) {
	if s.db == nil {
		return nil, errors.New("db")
	}
	rows, err := s.db.Query(`
		select id, telegram_user_id, chat_id, timezone, created_at
		from users
		order by id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.TelegramUserID, &u.ChatID, &u.Timezone, &u.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, u)
	}
	return res, rows.Err()
}

func (s *Store) CreateUser(u domain.User) (domain.User, error) {
	if s.db == nil {
		return domain.User{}, errors.New("db")
	}
	if u.Timezone == "" {
		u.Timezone = "UTC"
	}
	row := s.db.QueryRow(`
		insert into users(telegram_user_id, chat_id, timezone)
		values ($1, $2, $3)
		returning id, created_at`,
		u.TelegramUserID,
		u.ChatID,
		u.Timezone,
	)
	if err := row.Scan(&u.ID, &u.CreatedAt); err != nil {
		return domain.User{}, err
	}
	return u, nil
}

func (s *Store) ListTasks(userID int64, status string) ([]domain.Task, error) {
	if s.db == nil {
		return nil, errors.New("db")
	}
	var rows *sql.Rows
	var err error
	if status == "" {
		rows, err = s.db.Query(`
			select id, user_id, text, status, due_at, remind_at, notified_at, created_at, updated_at
			from tasks
			where user_id = $1
			order by id`,
			userID,
		)
	} else {
		rows, err = s.db.Query(`
			select id, user_id, text, status, due_at, remind_at, notified_at, created_at, updated_at
			from tasks
			where user_id = $1 and status = $2
			order by id`,
			userID,
			status,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, t)
	}
	return res, rows.Err()
}

func (s *Store) ListActive(userID int64) ([]domain.Task, error) {
	return s.ListTasks(userID, domain.TaskStatusActive)
}

func (s *Store) GetTask(id int64) (domain.Task, error) {
	if s.db == nil {
		return domain.Task{}, errors.New("db")
	}
	row := s.db.QueryRow(`
		select id, user_id, text, status, due_at, remind_at, notified_at, created_at, updated_at
		from tasks
		where id = $1`,
		id,
	)
	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, storage.ErrNotFound
		}
		return domain.Task{}, err
	}
	return t, nil
}

func (s *Store) GetByID(id int64) (domain.Task, error) {
	return s.GetTask(id)
}

func (s *Store) CreateTask(t domain.Task) (domain.Task, error) {
	if s.db == nil {
		return domain.Task{}, errors.New("db")
	}
	if t.Status == "" {
		t.Status = domain.TaskStatusActive
	}
	row := s.db.QueryRow(`
		insert into tasks(user_id, text, status, due_at, remind_at, notified_at)
		values ($1, $2, $3, $4, $5, $6)
		returning id, created_at, updated_at`,
		t.UserID,
		t.Text,
		t.Status,
		t.DueAt,
		t.RemindAt,
		t.NotifiedAt,
	)
	if err := row.Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return domain.Task{}, storage.ErrNotFound
		}
		return domain.Task{}, err
	}
	return t, nil
}

func (s *Store) Create(task domain.Task) (domain.Task, error) {
	return s.CreateTask(task)
}

func (s *Store) MarkDone(id int64) (domain.Task, error) {
	if s.db == nil {
		return domain.Task{}, errors.New("db")
	}
	row := s.db.QueryRow(`
		update tasks
		set status = $1,
			updated_at = now()
		where id = $2
		returning id, user_id, text, status, due_at, remind_at, notified_at, created_at, updated_at`,
		domain.TaskStatusDone,
		id,
	)
	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, storage.ErrNotFound
		}
		return domain.Task{}, err
	}
	return t, nil
}

func (s *Store) SetDue(id int64, dueAt *time.Time) (domain.Task, error) {
	if s.db == nil {
		return domain.Task{}, errors.New("db")
	}
	row := s.db.QueryRow(`
		update tasks
		set due_at = $1,
			updated_at = now()
		where id = $2
		returning id, user_id, text, status, due_at, remind_at, notified_at, created_at, updated_at`,
		dueAt,
		id,
	)
	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, storage.ErrNotFound
		}
		return domain.Task{}, err
	}
	return t, nil
}

func (s *Store) SetRemind(id int64, remindAt *time.Time) (domain.Task, error) {
	if s.db == nil {
		return domain.Task{}, errors.New("db")
	}
	row := s.db.QueryRow(`
		update tasks
		set remind_at = $1,
			notified_at = null,
			updated_at = now()
		where id = $2
		returning id, user_id, text, status, due_at, remind_at, notified_at, created_at, updated_at`,
		remindAt,
		id,
	)
	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, storage.ErrNotFound
		}
		return domain.Task{}, err
	}
	return t, nil
}

func (s *Store) UpdateTask(t domain.Task) (domain.Task, error) {
	if s.db == nil {
		return domain.Task{}, errors.New("db")
	}
	row := s.db.QueryRow(`
		update tasks
		set text = $1,
			status = $2,
			due_at = $3,
			remind_at = $4,
			notified_at = $5,
			updated_at = now()
		where id = $6
		returning updated_at`,
		t.Text,
		t.Status,
		t.DueAt,
		t.RemindAt,
		t.NotifiedAt,
		t.ID,
	)
	if err := row.Scan(&t.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Task{}, storage.ErrNotFound
		}
		return domain.Task{}, err
	}
	return t, nil
}

func (s *Store) ListDueForNotify(now time.Time) ([]domain.Task, error) {
	if s.db == nil {
		return nil, errors.New("db")
	}
	rows, err := s.db.Query(`
		update tasks
		set notified_at = $1,
			updated_at = $1
		where status = $2
			and remind_at is not null
			and remind_at <= $1
			and notified_at is null
		returning id, user_id, text, status, due_at, remind_at, notified_at, created_at, updated_at`,
		now,
		domain.TaskStatusActive,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, t)
	}
	return res, rows.Err()
}

func (s *Store) DeleteTask(id int64) error {
	if s.db == nil {
		return errors.New("db")
	}
	res, err := s.db.Exec(`delete from tasks where id = $1`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) Delete(id int64) error {
	return s.DeleteTask(id)
}
