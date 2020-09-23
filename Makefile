SSHPROXY_VERSION ?= 1.3.1
SSHPROXY_GIT_URL ?= github.com/cea-hpc/sshproxy

prefix		?= /usr
bindir		?= $(prefix)/bin
sbindir		?= $(prefix)/sbin
datarootdir	?= $(prefix)/share
mandir		?= $(datarootdir)/man
bashcompdir	?= /etc/bash_completion.d

GO		?= go

ASCIIDOC_OPTS	= -asshproxy_version=$(SSHPROXY_VERSION)
GO_OPTS		= $(GO_OPTS_EXTRA) -ldflags "-X main.SshproxyVersion=$(SSHPROXY_VERSION)"

SSHPROXY_SRC		= $(wildcard cmd/sshproxy/*.go)
SSHPROXY_DUMPD_SRC	= $(wildcard cmd/sshproxy-dumpd/*.go)
SSHPROXY_REPLAY_SRC	= $(wildcard cmd/sshproxy-replay/*.go)
SSHPROXYCTL_SRC		= $(wildcard cmd/sshproxyctl/*.go)
RECORD_SRC		= $(wildcard pkg/record/*.go)
UTILS_SRC		= $(wildcard pkg/utils/*.go)

PKGS	= $(shell $(GO) list ./... | grep -v /vendor/)
TEST	= test/centos-image/sshproxy_test.go
EXE	= $(addprefix bin/, sshproxy sshproxy-dumpd sshproxy-replay sshproxyctl)
MANDOC	= doc/sshproxy.yaml.5 doc/sshproxy.8 doc/sshproxy-dumpd.8 doc/sshproxy-replay.8 doc/sshproxyctl.8

all: exe doc

exe: $(EXE)

doc: $(MANDOC)

%.5: %.txt
	a2x $(ASCIIDOC_OPTS) -f manpage $<

%.8: %.txt
	a2x $(ASCIIDOC_OPTS) -f manpage $<

bin/sshproxy: $(SSHPROXY_SRC) $(RECORD_SRC) $(UTILS_SRC)
	$(GO) build $(GO_OPTS) -o $@ $(SSHPROXY_GIT_URL)/cmd/sshproxy

bin/sshproxy-dumpd: $(SSHPROXY_DUMPD_SRC) $(RECORD_SRC) $(UTILS_SRC)
	$(GO) build $(GO_OPTS) -o $@ $(SSHPROXY_GIT_URL)/cmd/sshproxy-dumpd

bin/sshproxy-replay: $(SSHPROXY_REPLAY_SRC) $(RECORD_SRC)
	$(GO) build $(GO_OPTS) -o $@ $(SSHPROXY_GIT_URL)/cmd/sshproxy-replay

bin/sshproxyctl: $(SSHPROXYCTL_SRC) $(UTILS_SRC)
	$(GO) build $(GO_OPTS) -o $@ $(SSHPROXY_GIT_URL)/cmd/sshproxyctl

install: install-binaries install-doc-man

install-doc-man: $(MANDOC)
	install -d $(DESTDIR)$(mandir)/man5
	install -p -m 0644 doc/*.5 $(DESTDIR)$(mandir)/man5
	install -d $(DESTDIR)$(mandir)/man8
	install -p -m 0644 doc/*.8 $(DESTDIR)$(mandir)/man8

install-binaries: $(EXE)
	install -d $(DESTDIR)$(sbindir)
	install -p -m 0755 bin/sshproxy $(DESTDIR)$(sbindir)
	install -p -m 0755 bin/sshproxy-dumpd $(DESTDIR)$(sbindir)
	install -d $(DESTDIR)$(bindir)
	install -p -m 0755 bin/sshproxy-replay $(DESTDIR)$(bindir)
	install -p -m 0755 bin/sshproxyctl $(DESTDIR)$(bindir)
	install -d $(DESTDIR)$(bashcompdir)
	install -p -m 0644 misc/sshproxyctl-completion.bash $(DESTDIR)$(bashcompdir)

fmt:
	$(GO) fmt $(PKGS)
	$(GO) fmt $(TEST)

get-deps:
	$(GO) get -u golang.org/x/lint/golint honnef.co/go/tools/cmd/staticcheck

check:
	golint $(PKGS)
	golint $(TEST)
	$(GO) vet ./...
	$(GO) vet $(TEST)
	staticcheck ./...
	staticcheck $(TEST)

test:
	$(GO) test -failfast -race -count=1 -timeout=10s ./...
	cd test && bash ./run.sh

clean:
	rm -f $(EXE) $(MANDOC) doc/*.xml

.PHONY: all exe doc install install-doc-man install-binaries fmt get-deps check clean test
