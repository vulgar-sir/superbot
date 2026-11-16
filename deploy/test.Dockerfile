# syntax=docker/dockerfile:1
# Runs the Go test suite, including the integration tests that need a real
# Postgres. Point SUPERBOT_TEST_DATABASE_URL at a throwaway database.
FROM golang:1.27.3-bookworm
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY queries ./queries
RUN go build ./... && go vet ./...
CMD ["sh", "-c", "go test ./... && go test -tags integration -run Integration -count=1 -v ./internal/agent/... ./internal/sandbox/... ./internal/store/..."]
