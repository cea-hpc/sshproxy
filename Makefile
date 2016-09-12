SSHPROXY_VERSION = 0.4.4

prefix		?= /usr
bindir		?= $(prefix)/bin
sbindir		?= $(prefix)/sbin
datarootdir	?= $(prefix)/share
mandir		?= $(datarootdir)/man

GO		?= go

ASCIIDOC_OPTS	= -asshproxy_version=$(SSHPROXY_VERSION)
GO_OPTS		= -ldflags "-X main.SSHPROXY_VERSION=$(SSHPROXY_VERSION)"

SSHPROXY_SRC		= $(wildcard sshproxy/*.go)
SSHPROXY_DUMPD_SRC	= $(wildcard sshproxy-dumpd/*.go)
SSHPROXY_MANAGERD_SRC	= $(wildcard sshproxy-managerd/*.go)
SSHPROXY_REPLAY_SRC	= $(wildcard sshproxy-replay/*.go)
GROUPGO_SRC		= $(wildcard group.go/*.go)
MANAGER_SRC		= $(wildcard manager/*.go)
RECORD_SRC		= $(wildcard record/*.go)
ROUTE_SRC		= $(wildcard route/*.go)
UTILS_SRC		= $(wildcard utils/*.go)

EXE	= sshproxy/sshproxy sshproxy-dumpd/sshproxy-dumpd sshproxy-managerd/sshproxy-managerd sshproxy-replay/sshproxy-replay
MANDOC	= doc/sshproxy.yaml.5 doc/sshproxy-managerd.yaml.5 doc/sshproxy.8 doc/sshproxy-dumpd.8 doc/sshproxy-managerd.8 doc/sshproxy-replay.8

all: $(EXE) $(MANDOC)

%.5: %.txt
	a2x $(ASCIIDOC_OPTS) -f manpage $<

%.8: %.txt
	a2x $(ASCIIDOC_OPTS) -f manpage $<

sshproxy/sshproxy: $(SSHPROXY_SRC) $(GROUPGO_SRC) $(MANAGER_SRC) $(RECORD_SRC) $(ROUTE_SRC) $(UTILS_SRC)
	cd sshproxy && $(GO) build $(GO_OPTS)

sshproxy-dumpd/sshproxy-dumpd: $(SSHPROXY_DUMPD_SRC) $(RECORD_SRC) $(UTILS_SRC)
	cd sshproxy-dumpd && $(GO) build $(GO_OPTS)

sshproxy-managerd/sshproxy-managerd: $(SSHPROXY_MANAGERD_SRC) $(ROUTE_SRC) $(UTILS_SRC)
	cd sshproxy-managerd && $(GO) build $(GO_OPTS)

sshproxy-replay/sshproxy-replay: $(SSHPROXY_REPLAY_SRC) $(RECORD_SRC)
	cd sshproxy-replay && $(GO) build $(GO_OPTS)

install: install-binaries install-doc-man

install-doc-man: $(MANDOC)
	install -d $(DESTDIR)$(mandir)/man5
	install -p -m 0644 doc/*.5 $(DESTDIR)$(mandir)/man5
	install -d $(DESTDIR)$(mandir)/man8
	install -p -m 0644 doc/*.8 $(DESTDIR)$(mandir)/man8

install-binaries: $(EXE)
	install -d $(DESTDIR)$(sbindir)
	install -p -m 0755 sshproxy/sshproxy $(DESTDIR)$(sbindir)
	install -p -m 0755 sshproxy-dumpd/sshproxy-dumpd $(DESTDIR)$(sbindir)
	install -p -m 0755 sshproxy-managerd/sshproxy-managerd $(DESTDIR)$(sbindir)
	install -d $(DESTDIR)$(bindir)
	install -p -m 0755 sshproxy-replay/sshproxy-replay $(DESTDIR)$(bindir)

clean:
	rm -f $(EXE) $(MANDOC) doc/*.xml
