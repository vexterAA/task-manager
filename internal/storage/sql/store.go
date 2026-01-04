package sqlstore

import (
	"database/sql"
	"errors"

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

func (s *Store) ListUsers() ([]string, error) {
	if s.db == nil {
		return nil, errors.New("db")
	}
	rows, err := s.db.Query("select name from users order by id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		res = append(res, name)
	}
	return res, rows.Err()
}

func (s *Store) CreateUser(name string) error {
	if s.db == nil {
		return errors.New("db")
	}
	_, err := s.db.Exec("insert into users(name) values($1)", name)
	return err
}
