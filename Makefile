.PHONY: build run test lint clean

build:
	go build -o bin/proxy ./cmd/proxy

run: build
	./bin/proxy

test:
	go test -v -race ./...

lint:
	go vet ./...

clean:
	rm -rf bin/

tidy:
	go mod tidy
