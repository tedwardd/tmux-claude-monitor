BINARY     := claude-monitor
INSTALL_DIR := $(HOME)/.local/bin
PKGNAME    := tmux-claude-monitor
VERSION    := test
WORKDIR    := /tmp/aur-build
ARCHIVE    := $(WORKDIR)/$(PKGNAME)-$(VERSION).tar.gz
PKG        := $(WORKDIR)/$(PKGNAME)-$(VERSION)-1-x86_64.pkg.tar.zst
TMUX_CONF  := $(HOME)/.config/tmux/tmux.conf

# GNU coreutils and the BSD tools on macOS disagree on both of these.
SHA256      := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo 'shasum -a 256')
SED_INPLACE := $(shell sed --version >/dev/null 2>&1 && echo 'sed -i' || echo "sed -i ''")

.PHONY: build test check-pollers snapshot install pkg pkg-install pkg-uninstall clean

build:
	go build -o $(BINARY) .

test:
	go test ./...

# Skim for a second daemon, a daemon started from this tree, a stale PID file, or
# another usage monitor sharing the token. Running `./claude-monitor daemon` from
# here hijacks the installed daemon's PID file and deletes it on exit, which
# leaves refresh reporting no daemon while one is running. Exits non-zero on a
# conflict. For "why is the bar showing ??" use `claude-monitor doctor` instead.
check-pollers:
	@./scripts/check-pollers.sh

# Build every release target locally and render the Homebrew cask into dist/
snapshot:
	HOMEBREW_TAP_TOKEN=unused go run github.com/goreleaser/goreleaser/v2@latest \
		release --snapshot --clean --skip=publish

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
	@SHA=$$($(SHA256) $(ARCHIVE) | awk '{print $$1}'); \
	echo "==> sha256: $$SHA"; \
	echo "==> Patching PKGBUILD"; \
	cp packaging/aur/$(PKGNAME)/PKGBUILD $(WORKDIR)/; \
	cp packaging/aur/$(PKGNAME)/claude-monitor.install $(WORKDIR)/; \
	$(SED_INPLACE) "s/^pkgver=.*/pkgver=$(VERSION)/" $(WORKDIR)/PKGBUILD; \
	$(SED_INPLACE) "s|source=.*|source=(\"$(PKGNAME)-$(VERSION).tar.gz\")|" $(WORKDIR)/PKGBUILD; \
	$(SED_INPLACE) "s/^sha256sums=.*/sha256sums=('$$SHA')/" $(WORKDIR)/PKGBUILD
	@echo "==> Running makepkg"
	@cd $(WORKDIR) && makepkg --noconfirm
	@echo ""
	@echo "Package ready: $(PKG)"

# Install the locally built package and run post-install setup
pkg-install:
	sudo pacman -U --noconfirm $(PKG)
	systemctl --user enable --now claude-monitor
	claude-monitor init

# Reverse everything pkg-install did
pkg-uninstall:
	-systemctl --user stop claude-monitor
	-systemctl --user disable claude-monitor
	-rm -f $(HOME)/.config/systemd/user/claude-monitor.service
	-systemctl --user daemon-reload
	-$(SED_INPLACE) '/# claude-monitor begin/,/# claude-monitor end/d' $(TMUX_CONF)
	-tmux source $(TMUX_CONF) 2>/dev/null
	-rm -rf $(HOME)/.config/claude-monitor
	-rm -rf $(HOME)/.cache/claude-monitor
	sudo pacman -R --noconfirm $(PKGNAME)

# Remove build artifacts (local binary and AUR build directory)
clean:
	rm -f $(BINARY)
	rm -rf $(WORKDIR)
