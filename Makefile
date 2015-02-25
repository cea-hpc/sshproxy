SSHPROXY_VERSION = 0.1.0

prefix		?= /usr
bindir		?= $(prefix)/bin
sbindir		?= $(prefix)/sbin
datarootdir	?= $(prefix)/share
mandir		?= $(datarootdir)/man

ASCIIDOC_OPTS	= -asshproxy_version=$(SSHPROXY_VERSION)
GO_OPTS		= -ldflags "-X main.SSHPROXY_VERSION $(SSHPROXY_VERSION)"

SSHPROXY_SRC		= $(wildcard sshproxy/*.go)
SSHPROXY_REPLAY_SRC	= $(wildcard sshproxy-replay/*.go)
RECORD_SRC		= $(wildcard record/*.go)
GROUPGO_SRC		= $(wildcard group.go/*.go)

EXE	= sshproxy/sshproxy sshproxy-replay/sshproxy-replay
MANDOC	= doc/sshproxy.cfg.5 doc/sshproxy.8 doc/sshproxy-replay.8

all: $(EXE) $(MANDOC)

%.5: %.txt
	a2x $(ASCIIDOC_OPTS) -f manpage $<

%.8: %.txt
	a2x $(ASCIIDOC_OPTS) -f manpage $<

sshproxy/sshproxy: $(SSHPROXY_SRC) $(RECORD_SRC) $(GROUPGO_SRC)
	cd sshproxy && go build $(GO_OPTS)

sshproxy-replay/sshproxy-replay: $(SSHPROXY_REPLAY_SRC) $(RECORD_SRC)
	cd sshproxy-replay && go build $(GO_OPTS)

install: install-binaries install-doc-man

install-doc-man: $(MANDOC)
	install -d $(DESTDIR)$(mandir)/man5
	install -p -m 0644 doc/*.5 $(DESTDIR)$(mandir)/man5
	install -d $(DESTDIR)$(mandir)/man8
	install -p -m 0644 doc/*.8 $(DESTDIR)$(mandir)/man8

install-binaries: $(EXE)
	install -d $(DESTDIR)$(sbindir)
	install -p -m 0755 sshproxy/sshproxy $(DESTDIR)$(sbindir)
	install -d $(DESTDIR)$(bindir)
	install -p -m 0755 sshproxy-replay/sshproxy-replay $(DESTDIR)$(bindir)

clean:
	rm -f doc/*.5 doc/*.8 doc/*.xml
	rm -f sshproxy/sshproxy sshproxy-replay/sshproxy-replay
