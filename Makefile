.PHONY: build gui install test clean

BIN := tsession
PREFIX ?= $(HOME)/.local/bin
WAILS ?= wails

build:
	go build -o $(BIN) .

gui:
	@command -v $(WAILS) >/dev/null 2>&1 || { echo "error: wails is not installed. Install it with: go install github.com/wailsapp/wails/v2/cmd/wails@latest" >&2; exit 1; }
	cd gui && $(WAILS) build

install: build
	mkdir -p $(PREFIX)
	install -m 0755 $(BIN) $(PREFIX)/$(BIN)

test:
	go test ./...

clean:
	rm -f $(BIN)
