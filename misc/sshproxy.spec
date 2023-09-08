# disable debug package creation because of a bug when producing debuginfo
# packages: http://fedoraproject.org/wiki/PackagingDrafts/Go#Debuginfo
%global debug_package   %{nil}

Name:           sshproxy
Version:        1.5.2
Release:        1%{?dist}
Summary:        SSH proxy
License:        CeCILL-B
Source:         https://github.com/cea-hpc/%{name}/archive/v%{version}/%{name}-%{version}.tar.gz
ExclusiveArch:  %{?go_arches:%{go_arches}}%{!?go_arches:%{ix86} x86_64 %{arm} aarch64}
BuildRequires:  %{?go_compiler:compiler(go-compiler)}%{!?go_compiler:golang}
BuildRequires:  asciidoc

Summary:        SSH proxy

%description
%{summary}

This package provides an SSH proxy which can be used on a gateway to
automatically connect a remote user to a defined internal host.

%prep
%setup -q

%build
# set up temporary build gopath, and put our directory there
mkdir -p ./_build/src/github.com/cea-hpc
ln -s $(pwd) ./_build/src/github.com/cea-hpc/sshproxy

export GOPATH=$(pwd)/_build:%{gopath}
make

%install
make install DESTDIR=%{buildroot} prefix=%{_prefix} mandir=%{_mandir}
install -d -m0755 %{buildroot}%{_sysconfdir}/sshproxy
install -p -m 0644 config/sshproxy.yaml %{buildroot}%{_sysconfdir}/sshproxy

%files
%doc Licence_CeCILL-B_V1-en.txt Licence_CeCILL-B_V1-fr.txt
%config(noreplace) %{_sysconfdir}/sshproxy/sshproxy.yaml
%{_sysconfdir}/bash_completion.d
%{_sbindir}/sshproxy
%{_sbindir}/sshproxy-dumpd
%{_bindir}/sshproxy-replay
%{_bindir}/sshproxyctl
%{_mandir}/man5/sshproxy.yaml.5*
%{_mandir}/man8/sshproxy.8*
%{_mandir}/man8/sshproxyctl.8*
%{_mandir}/man8/sshproxy-dumpd.8*
%{_mandir}/man8/sshproxy-replay.8*

%changelog
* Fri Sep 08 2023 Cyril Servant <cyril.servant@cea.fr> - 1.5.2-1
- sshproxy 1.5.2

* Tue Mar 22 2022 Cyril Servant <cyril.servant@cea.fr> - 1.5.1-1
- sshproxy 1.5.1

* Tue Oct 26 2021 Cyril Servant <cyril.servant@cea.fr> - 1.5.0-1
- sshproxy 1.5.0

* Mon Aug 16 2021 Cyril Servant <cyril.servant@cea.fr> - 1.4.0-1
- sshproxy 1.4.0

* Wed Jul 28 2021 Cyril Servant <cyril.servant@cea.fr> - 1.3.8-1
- sshproxy 1.3.8

* Tue Jun 29 2021 Cyril Servant <cyril.servant@cea.fr> - 1.3.7-1
- sshproxy 1.3.7

* Fri Apr 09 2021 Cyril Servant <cyril.servant@cea.fr> - 1.3.6-1
- sshproxy 1.3.6

* Thu Mar 04 2021 Cyril Servant <cyril.servant@cea.fr> - 1.3.5-1
- sshproxy 1.3.5

* Tue Feb 02 2021 Cyril Servant <cyril.servant@cea.fr> - 1.3.4-1
- sshproxy 1.3.4

* Fri Oct 02 2020 Cyril Servant <cyril.servant@cea.fr> - 1.3.3-1
- sshproxy 1.3.3

* Mon Sep 28 2020 Cyril Servant <cyril.servant@cea.fr> - 1.3.2-1
- sshproxy 1.3.2

* Wed Sep 23 2020 Cyril Servant <cyril.servant@cea.fr> - 1.3.1-1
- sshproxy 1.3.1

* Wed Aug 05 2020 Cyril Servant <cyril.servant@cea.fr> - 1.3.0-1
- sshproxy 1.3.0

* Thu Apr 30 2020 Cyril Servant <cyril.servant@cea.fr> - 1.2.0-1
- sshproxy 1.2.0

* Fri Mar 06 2020 Cyril Servant <cyril.servant@cea.fr> - 1.1.0-1
- sshproxy 1.1.0

* Thu Jun 06 2019 Arnaud Guignard <arnaud.guignard@cea.fr> - 1.0.0-1
- sshproxy 1.0.0

* Thu Jan 11 2018 Arnaud Guignard <arnaud.guignard@cea.fr> - 0.4.5-1
- sshproxy 0.4.5

* Mon Sep 12 2016 Arnaud Guignard <arnaud.guignard@cea.fr> - 0.4.4-1
- sshproxy 0.4.4

* Wed Jul 13 2016 Arnaud Guignard <arnaud.guignard@cea.fr> - 0.4.3-1
- sshproxy 0.4.3

* Mon Apr 25 2016 Arnaud Guignard <arnaud.guignard@cea.fr> - 0.4.2-1
- sshproxy 0.4.2

* Tue Dec 08 2015 Arnaud Guignard <arnaud.guignard@cea.fr> - 0.4.1-1
- sshproxy 0.4.1

* Mon Nov 23 2015 Arnaud Guignard <arnaud.guignard@cea.fr> - 0.4.0-1
- sshproxy 0.4.0

* Wed Jun 24 2015 Arnaud Guignard <arnaud.guignard@cea.fr> - 0.3.1-1
- sshproxy 0.3.1

* Wed Mar 25 2015 Arnaud Guignard <arnaud.guignard@cea.fr> - 0.3.0-1
- sshproxy 0.3.0

* Mon Mar 02 2015 Arnaud Guignard <arnaud.guignard@cea.fr> - 0.2.0-1
- sshproxy 0.2.0

* Thu Feb 12 2015 Arnaud Guignard <arnaud.guignard@cea.fr> - 0.1.0-1
- sshproxy 0.1.0
