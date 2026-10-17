# Fedora COPR, "make_srpm" source method (docs/linux-packages.md): COPR runs
#   make -f .copr/Makefile srpm outdir=<dir> spec=<spec>
# in a Fedora chroot with network, then builds the SRPM without network.
# Builds the offline source tarball from this checkout and fetches the
# release tarball with the prebuilt window for the version being built.
VERSION ?= $(patsubst v%,%,$(shell git describe --tags --abbrev=0))
outdir ?= $(CURDIR)
TOP := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)

srpm:
	dnf -y install git golang nodejs npm rpm-build curl
	git config --global --add safe.directory $(TOP)
	cd $(TOP) && (cd web && npm ci --no-audit --no-fund && node build.mjs) && PKG_VERSION=$(VERSION) packaging/linux/source-tarball.sh
	curl -fL -o $(TOP)/dist/packages/gecko-$(VERSION)-linux-x86_64.tar.gz \
		https://github.com/eternaleclipse/gecko/releases/download/v$(VERSION)/gecko-$(VERSION)-linux-x86_64.tar.gz
	rpmbuild -bs --define "_sourcedir $(TOP)/dist/packages" --define "_srcrpmdir $(outdir)" \
		--define "gecko_version $(VERSION)" $(TOP)/packaging/rpm/gecko.spec
