APP?=yourapp

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: build
build:
	go build -o bin/$(APP) ./cmd/api

.PHONY: run
run:
	HTTP_ADDR=:8080 STORAGE=memory go run ./cmd/api

.PHONY: run-sql
run-sql:
	STORAGE=sql DB_DRIVER=pgx DB_DSN=$${DB_DSN} go run ./cmd/api

.PHONY: test
test:
	go test ./...
