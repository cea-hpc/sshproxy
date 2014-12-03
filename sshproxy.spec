%global debug_package   %{nil}
%global commit          83aa3a1b8b61b7b6eed652f3ffe042780f0e5469
%global shortcommit     %(c=%{commit}; echo ${c:0:7})

Name:           sshproxy
Version:        0
Release:        0.2.git%{shortcommit}%{?dist}
Summary:        SSH proxy
License:        BSD
Source0:        sshproxy-%{shortcommit}.tar.xz
BuildArch:      %{ix86} x86_64 %{arm}

BuildRequires:  golang >= 1.3
BuildRequires:  golang(github.com/BurntSushi/toml)
BuildRequires:  golang(github.com/op/go-logging)
Summary:        SSH proxy

%description
%{summary}

This package provides an SSH proxy which can be used on a gateway to
automatically connect a remote user to a defined internal host.

%prep
%setup -q -n sshproxy-%{shortcommit}

%build
# set up temporary build gopath, and put our directory there
mkdir -p ./_build/src
ln -s $(pwd) ./_build/src/sshproxy

export GOPATH=$(pwd)/_build:%{gopath}
go build -o sshproxy .

%install
# install binary
install -d %{buildroot}%{_sbindir}
install -p -m 755 ./sshproxy %{buildroot}%{_sbindir}/sshproxy

# install configuration
install -d %{buildroot}%{_sysconfdir}
install -p -m 644 sshproxy.cfg %{buildroot}%{_sysconfdir}/

%files
%doc README.md
%config(noreplace) %{_sysconfdir}/sshproxy.cfg
%{_sbindir}/sshproxy

%changelog
* Wed Dec 03 2014 Arnaud Guignard <arnaud.guignard@cea.fr> - 0-0.2.git83aa3a1
- New version with groups and routes configuration

* Tue Oct 21 2014 Arnaud Guignard <arnaud.guignard@cea.fr> - 0-0.1.git1c1c5ee
- Initial fedora package
