PREFIX  ?= /usr/local
BINDIR  := $(PREFIX)/bin
DESTDIR ?=

VERSION := 0.1.0

.PHONY: all install uninstall deb rpm arch

all:
	@echo "Run 'make install' to install workmuxinator."

install:
	install -Dm755 bin/workmuxinator $(DESTDIR)$(BINDIR)/workmuxinator

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/workmuxinator

# ── Packaging targets ─────────────────────────────────────────────────────────

deb:
	@command -v dpkg-deb >/dev/null 2>&1 || { echo "dpkg-deb not found"; exit 1; }
	rm -rf /tmp/workmuxinator-deb
	mkdir -p /tmp/workmuxinator-deb/DEBIAN
	mkdir -p /tmp/workmuxinator-deb/usr/local/bin
	cp packaging/debian/control /tmp/workmuxinator-deb/DEBIAN/control
	sed -i "s/@VERSION@/$(VERSION)/" /tmp/workmuxinator-deb/DEBIAN/control
	install -Dm755 bin/workmuxinator /tmp/workmuxinator-deb/usr/local/bin/workmuxinator
	dpkg-deb --build /tmp/workmuxinator-deb workmuxinator_$(VERSION)_all.deb
	@echo "Built: workmuxinator_$(VERSION)_all.deb"

rpm:
	@command -v rpmbuild >/dev/null 2>&1 || { echo "rpmbuild not found"; exit 1; }
	mkdir -p ~/rpmbuild/{BUILD,BUILDROOT,RPMS,SOURCES,SPECS,SRPMS}
	sed "s/@VERSION@/$(VERSION)/" packaging/rpm/workmuxinator.spec > ~/rpmbuild/SPECS/workmuxinator.spec
	cp bin/workmuxinator ~/rpmbuild/SOURCES/
	rpmbuild -bb ~/rpmbuild/SPECS/workmuxinator.spec
	@echo "RPM built in ~/rpmbuild/RPMS/"

arch:
	@command -v makepkg >/dev/null 2>&1 || { echo "makepkg not found"; exit 1; }
	cp packaging/arch/PKGBUILD /tmp/workmuxinator-arch-PKGBUILD
	cd /tmp && mkdir -p workmuxinator-arch && cp workmuxinator-arch-PKGBUILD workmuxinator-arch/PKGBUILD
	cd /tmp/workmuxinator-arch && makepkg -sf
	@echo "Package built in /tmp/workmuxinator-arch/"
