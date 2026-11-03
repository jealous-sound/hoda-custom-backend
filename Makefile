BIN = bin/hoda-server

$(BIN): $(shell find cmd internal -name '*.go') go.mod
	go build -o $(BIN) ./cmd/hoda-server

run: $(BIN)
	./$(BIN)

play:
	./scripts/play-local.sh

test:
	go vet ./...
	go test ./...

format:
	gofmt -w cmd internal

format-check:
	test -z "$$(gofmt -l cmd internal)"

clean:
	rm -rf bin

.PHONY: run play test format format-check clean
