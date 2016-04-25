# disable debug package creation because of a bug when producing debuginfo
# packages: http://fedoraproject.org/wiki/PackagingDrafts/Go#Debuginfo
%global debug_package   %{nil}


%if 0%{?rhel} >= 7
%global _with_systemd 1
%endif

Name:           sshproxy
Version:        0.4.2
Release:        1%{?dist}
Summary:        SSH proxy
License:        CeCILL-B
Source:         https://github.com/cea-hpc/%{name}/archive/v%{version}/%{name}-%{version}.tar.gz
BuildArch:      %{ix86} x86_64 %{arm}

BuildRequires:  golang >= 1.3
BuildRequires:  asciidoc

%if 0%{?_with_systemd}
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd
BuildRequires:  systemd
%else
Requires(post): upstart
Requires(preun): upstart
Requires(postun): upstart
%endif

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
make

%install
make install DESTDIR=%{buildroot} prefix=%{_prefix} mandir=%{_mandir}
install -d -m0755 %{buildroot}%{_sysconfdir}/sshproxy
install -p -m 0644 config/sshproxy.yaml %{buildroot}%{_sysconfdir}/sshproxy
install -p -m 0644 config/sshproxy-managerd.yaml %{buildroot}%{_sysconfdir}/sshproxy
%if 0%{?_with_systemd}
install -d -m0755  %{buildroot}%{_unitdir}
install -Dp -m0644 misc/sshproxy-managerd.service %{buildroot}%{_unitdir}/sshproxy-managerd.service
%else
install -d -m0755 %{buildroot}%{_sysconfdir}/init
install -p -m 0644 misc/sshproxy-managerd.upstart %{buildroot}%{_sysconfdir}/init/sshproxy-managerd.conf
%endif

%files
%doc Licence_CeCILL-B_V1-en.txt Licence_CeCILL-B_V1-fr.txt
%if 0%{?_with_systemd}
%{_unitdir}/sshproxy-managerd.service
%else
%config(noreplace) %{_sysconfdir}/init/sshproxy-managerd.conf
%endif
%config(noreplace) %{_sysconfdir}/sshproxy/sshproxy.yaml
%config(noreplace) %{_sysconfdir}/sshproxy/sshproxy-managerd.yaml
%{_sbindir}/sshproxy
%{_sbindir}/sshproxy-dumpd
%{_sbindir}/sshproxy-managerd
%{_bindir}/sshproxy-replay
%{_mandir}/man5/sshproxy.yaml.5*
%{_mandir}/man5/sshproxy-managerd.yaml.5*
%{_mandir}/man8/sshproxy.8*
%{_mandir}/man8/sshproxy-dumpd.8*
%{_mandir}/man8/sshproxy-managerd.8*
%{_mandir}/man8/sshproxy-replay.8*

%post
%if 0%{?_with_systemd}
%systemd_post sshproxy-managerd.service
%else
if [ "$1" -ge 1 ]; then
  /sbin/stop sshproxy-managerd >/dev/null 2>&1
  /sbin/start sshproxy-managerd
fi
%endif
exit 0

%preun
%if 0%{?_with_systemd}
%systemd_preun sshproxy-managerd.service
%else
if [ "$1" -eq 0 ]; then
  /sbin/stop sshproxy-managerd >/dev/null 2>&1
fi
%endif
exit 0

%postun
%if 0%{?_with_systemd}
%systemd_postun_with_restart sshproxy-managerd.service
%endif
exit 0

%changelog
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
