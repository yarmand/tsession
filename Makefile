.PHONY: build install test clean

BIN := tsession
PREFIX ?= $(HOME)/.local/bin

build:
	go build -o $(BIN) .

install: build
	mkdir -p $(PREFIX)
	install -m 0755 $(BIN) $(PREFIX)/$(BIN)

test:
	go test ./...

clean:
	rm -f $(BIN)
