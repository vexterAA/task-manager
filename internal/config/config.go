package config

import (
	"flag"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Env             string
	HTTPAddr        string
	Storage         string
	DBDriver        string
	DBDSN           string
	ShutdownTimeout time.Duration
}

func getenv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func getdur(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func Load() Config {
	var addr string
	var storage string
	var env string
	flag.StringVar(&addr, "http", getenv("HTTP_ADDR", ":8080"), "addr")
	flag.StringVar(&storage, "storage", getenv("STORAGE", "memory"), "storage")
	flag.StringVar(&env, "env", getenv("APP_ENV", "dev"), "env")
	flag.Parse()
	return Config{
		Env:             env,
		HTTPAddr:        addr,
		Storage:         storage,
		DBDriver:        getenv("DB_DRIVER", "pgx"),
		DBDSN:           getenv("DB_DSN", ""),
		ShutdownTimeout: getdur("SHUTDOWN_TIMEOUT", 5*time.Second),
	}
}

func MustAtoi(s string, def int) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return i
}
