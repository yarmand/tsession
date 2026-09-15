.PHONY: build gui install test clean

BIN := tsession
PREFIX ?= $(HOME)/.local/bin
WAILS ?= wails
WAILS_BUILD_FLAGS ?= $(if $(filter linux,$(shell go env GOOS)),-tags webkit2_41)

build:
	$(info "Building $(BIN)...")
	go build -o $(BIN) .

gui:
	$(info "Building GUI with $(WAILS)...")
	@command -v $(WAILS) >/dev/null 2>&1 || { echo "error: wails is not installed. Install it with: go install github.com/wailsapp/wails/v2/cmd/wails@latest" >&2; exit 1; }
	cd gui && $(WAILS) build $(WAILS_BUILD_FLAGS)

install: build gui
	mkdir -p $(PREFIX)
	install -m 0755 $(BIN) $(PREFIX)/$(BIN)
	@case "$$(go env GOOS)" in \
		darwin) \
			test -d gui/build/bin/TSession.app || { echo "error: GUI artifact gui/build/bin/TSession.app was not produced" >&2; exit 1; }; \
			rm -rf "$(PREFIX)/TSession.app"; \
			cp -R gui/build/bin/TSession.app "$(PREFIX)/TSession.app"; \
			;; \
		windows) \
			test -f gui/build/bin/TSession.exe || { echo "error: GUI artifact gui/build/bin/TSession.exe was not produced" >&2; exit 1; }; \
			install -m 0755 gui/build/bin/TSession.exe "$(PREFIX)/TSession.exe"; \
			;; \
		*) \
			test -f gui/build/bin/TSession || { echo "error: GUI artifact gui/build/bin/TSession was not produced" >&2; exit 1; }; \
			install -m 0755 gui/build/bin/TSession "$(PREFIX)/TSession"; \
			;; \
	esac

test:
	go test ./...

clean:
	rm -f $(BIN)
