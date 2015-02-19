%global debug_package   %{nil}

Name:           sshproxy
Version:        0.1.0
Release:        1%{?dist}
Summary:        SSH proxy
License:        CeCILL-B
Source:         %{name}-%{version}.tar.xz
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
%setup -q

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
%doc README.md Licence_CeCILL-B_V1-en.txt Licence_CeCILL-B_V1-fr.txt
%config(noreplace) %{_sysconfdir}/sshproxy.cfg
%{_sbindir}/sshproxy

%changelog
* Thu Feb 12 2015 Arnaud Guignard <arnaud.guignard@cea.fr> - 0.1.0-1
- sshproxy 0.1.0
