.PHONY: build test vet fmt tidy frontend run-server run-ops race measure

build:
	go build ./...

test:
	go test -timeout=300s -count=1 ./...

race:
	go test -race -timeout=420s -count=1 ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

tidy:
	go mod tidy

frontend:
	cd web && npm install && npm run build

run-server:
	go run ./cmd/server -config config.yaml

run-ops:
	go run ./cmd/ops status -data ./data

measure:
	go run .factory/measure_project.go -root . -enforce \
		-min-prod-lines 2400 -min-prod-files 23 \
		-min-packages 9 -min-test-lines 900 \
		-max-file-lines 600 -require-frontend
