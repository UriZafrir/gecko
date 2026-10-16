VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# Package version: the latest release tag without its "v".
PKG_VERSION ?= $(or $(patsubst v%,%,$(shell git describe --tags --abbrev=0 2>/dev/null)),0.0.0)
LDFLAGS := -s -w -X main.version=$(VERSION)
GO ?= go
export GOTOOLCHAIN ?= local

PREFIX ?= $(HOME)/.local
DESKTOP_DIR := $(PREFIX)/share/gecko/desktop
.PHONY: all web build test dist packages install install-desktop desktop clean dev android android-gecko android-test

all: web build

web:
	cd web && npm install --no-audit --no-fund && node build.mjs

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/gecko ./cmd/gecko

test:
	$(GO) vet ./...
	$(GO) test ./...

# Cross-compiled binaries; `gecko host install` picks the right one.
dist: web
	@mkdir -p dist
	for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 freebsd/amd64; do \
		os=$${t%/*}; arch=$${t#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/gecko-$$os-$$arch$$ext ./cmd/gecko || exit 1; \
	done

# Installers: .deb/.rpm, a macOS app (zip) and Windows setup.exe, in
# dist/packages. One platform: make packages PLATFORMS=linux
packages: web
	GO="$(GO)" VERSION="$(VERSION)" PKG_VERSION="$(PKG_VERSION)" LDFLAGS="$(LDFLAGS)" packaging/build.sh $(PLATFORMS)

install: all
	install -d $(PREFIX)/bin
	install bin/gecko $(PREFIX)/bin/gecko
	@# Restart a running service with the new binary, keeping its sessions.
	@-$(PREFIX)/bin/gecko upgrade --if-running
	@if [ "$$(uname -s)" = Linux ]; then $(MAKE) --no-print-directory install-desktop; fi

# App-menu launcher (freedesktop: GNOME, KDE, ...), runs `gecko open`.
install-desktop:
	install -d $(PREFIX)/share/applications $(PREFIX)/share/icons/hicolor/scalable/apps
	sed 's|@BIN@|$(PREFIX)/bin/gecko|' packaging/linux/gecko.desktop > $(PREFIX)/share/applications/gecko.desktop
	install -m 644 packaging/linux/gecko.svg $(PREFIX)/share/icons/hicolor/scalable/apps/gecko.svg
	-update-desktop-database $(PREFIX)/share/applications 2>/dev/null
	@# GNOME reads icons through this cache; a stale one hides new or changed icons.
	@if [ -f $(PREFIX)/share/icons/hicolor/icon-theme.cache ] || command -v gtk-update-icon-cache >/dev/null; then \
		gtk-update-icon-cache -f -t -q $(PREFIX)/share/icons/hicolor 2>/dev/null || true; fi

# Android app (android/): a debug APK at android/app/build/outputs/apk/debug.
# Needs JDK 17 and the Android SDK (ANDROID_HOME); see docs/android.md.
# The APK carries Gecko itself (the daemon and web UI) for the phone's own
# terminal, as lib/<abi>/libgecko.so: a static linux build runs on Android.
android: android-gecko
	cd android && ./gradlew --no-daemon assembleDebug
	@ls -l android/app/build/outputs/apk/debug/app-debug.apk

ANDROID_JNI := android/app/src/main/jniLibs
android-gecko: web
	@for t in arm64-v8a/arm64 x86_64/amd64; do \
		abi=$${t%/*}; arch=$${t#*/}; mkdir -p $(ANDROID_JNI)/$$abi; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(ANDROID_JNI)/$$abi/libgecko.so ./cmd/gecko || exit 1; \
	done

# Its unit tests; `./gradlew connectedDebugAndroidTest` runs the device tests.
android-test:
	cd android && ./gradlew --no-daemon testDebugUnitTest

# Native desktop app (Electron): see-through window with blur on macOS and
# Windows. `gecko open` uses it automatically once installed.
desktop:
	cd desktop && npm install --no-audit --no-fund && node node_modules/electron/install.js
	install -d $(DESKTOP_DIR) $(PREFIX)/bin
	cp packaging/linux/gecko-256.png desktop/icon.png
	rm -rf $(DESKTOP_DIR) && cp -a desktop $(DESKTOP_DIR)
	printf '#!/bin/sh\n[ "$$GECKO_DESKTOP_NO_SANDBOX" = 1 ] && set -- --no-sandbox "$$@"\nexec "%s/node_modules/electron/dist/electron" "%s" "$$@"\n' "$(DESKTOP_DIR)" "$(DESKTOP_DIR)" > $(PREFIX)/bin/gecko-desktop
	chmod +x $(PREFIX)/bin/gecko-desktop
	@echo "installed gecko-desktop; \`gecko open\` now uses it"
	@if [ "$$(cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns 2>/dev/null)" = 1 ] && [ ! -f /etc/apparmor.d/gecko-desktop ]; then \
		echo "this system restricts the sandbox Electron needs; run once: make desktop-sandbox (uses sudo)"; fi

# One-time, needs sudo: lets the desktop app's sandbox work on Ubuntu 23.10+.
desktop-sandbox:
	sed 's|@ELECTRON@|$(DESKTOP_DIR)/node_modules/electron/dist/electron|' packaging/linux/apparmor-gecko-desktop | sudo tee /etc/apparmor.d/gecko-desktop >/dev/null
	sudo apparmor_parser -r /etc/apparmor.d/gecko-desktop
	@echo "done: gecko-desktop can now use its sandbox"

dev:
	cd web && node build.mjs --watch

clean:
	rm -rf bin dist web/dist/*.js web/dist/*.css web/dist/assets
