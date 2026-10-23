BIN := vela
VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test cross clean run

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BIN) .

test:
	go test ./...

cross:
	mkdir -p dist
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BIN)-linux-amd64 .
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BIN)-linux-arm64 .
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BIN)-darwin-arm64 .
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BIN)-darwin-amd64 .
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BIN)-windows-amd64.exe .
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o dist/hapticsd-windows-amd64.exe ./cmd/hapticsd
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o dist/hapticsd-linux-amd64 ./cmd/hapticsd
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o dist/hapticsd-linux-arm64 ./cmd/hapticsd
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o dist/hapticsd-darwin-arm64 ./cmd/hapticsd
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o dist/hapticsd-darwin-amd64 ./cmd/hapticsd

run: build
	./bin/$(BIN) serve

clean:
	rm -rf bin dist
