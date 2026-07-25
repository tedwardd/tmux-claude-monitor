BINARY     := claude-monitor
INSTALL_DIR := $(HOME)/.local/bin
PKGNAME    := tmux-claude-monitor
VERSION    := test
WORKDIR    := /tmp/aur-build
ARCHIVE    := $(WORKDIR)/$(PKGNAME)-$(VERSION).tar.gz
PKG        := $(WORKDIR)/$(PKGNAME)-$(VERSION)-1-x86_64.pkg.tar.zst

.PHONY: build test install pkg pkg-install pkg-uninstall clean

build:
	go build -o $(BINARY) .

test:
	go test ./...

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed to $(INSTALL_DIR)/$(BINARY)"

# Build a local .pkg.tar.zst from the current working tree
pkg:
	@echo "==> Creating source archive"
	@mkdir -p $(WORKDIR)
	@git archive --format=tar.gz --prefix=$(PKGNAME)-$(VERSION)/ HEAD \
		> $(ARCHIVE)
	@SHA=$$(sha256sum $(ARCHIVE) | awk '{print $$1}'); \
	echo "==> sha256: $$SHA"; \
	echo "==> Patching PKGBUILD"; \
	cp packaging/aur/$(PKGNAME)/PKGBUILD $(WORKDIR)/; \
	cp packaging/aur/$(PKGNAME)/claude-monitor.install $(WORKDIR)/; \
	sed -i "s/^pkgver=.*/pkgver=$(VERSION)/" $(WORKDIR)/PKGBUILD; \
	sed -i "s|source=.*|source=(\"$(PKGNAME)-$(VERSION).tar.gz\")|" $(WORKDIR)/PKGBUILD; \
	sed -i "s/^sha256sums=.*/sha256sums=('$$SHA')/" $(WORKDIR)/PKGBUILD
	@echo "==> Running makepkg"
	@cd $(WORKDIR) && makepkg --noconfirm
	@echo ""
	@echo "Package ready: $(PKG)"
	@echo "Run 'make pkg-install' to install it"

# Install the locally built package
pkg-install: $(PKG)
	sudo pacman -U --noconfirm $(PKG)

# Uninstall the test package
pkg-uninstall:
	sudo pacman -R --noconfirm $(PKGNAME)

# Remove build artifacts (local binary and AUR build directory)
clean:
	rm -f $(BINARY)
	rm -rf $(WORKDIR)
