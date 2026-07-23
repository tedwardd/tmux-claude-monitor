BINARY := claude-monitor
INSTALL_DIR := $(HOME)/.local/bin

.PHONY: build test install

build:
	go build -o $(BINARY) .

test:
	go test ./...

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed to $(INSTALL_DIR)/$(BINARY)"
