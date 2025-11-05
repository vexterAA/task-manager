FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -o /out/app ./cmd/api

FROM alpine:3.20
ENV HTTP_ADDR=:8080
WORKDIR /app
COPY --from=build /out/app /app/app
EXPOSE 8080
CMD ["/app/app"]
