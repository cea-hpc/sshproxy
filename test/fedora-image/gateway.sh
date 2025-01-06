#!/bin/bash

set -eux

git config --global --add safe.directory /sshproxy

# Create rpmbuild directories
mkdir -p /root/rpmbuild/SOURCES
mkdir -p /root/rpmbuild/SPECS

# Find sshproxy version and branch
SSHPROXY_VERSION="$(grep '^SSHPROXY_VERSION' /sshproxy/Makefile | cut -d' ' -f3)"
SSHPROXY_FULLNAME="sshproxy-${SSHPROXY_VERSION}"
SSHPROXY_COMMIT="$(cd /sshproxy && git rev-parse HEAD)"

# Create tarball
rm -rf /tmp/sshproxy
git config --global --add safe.directory /sshproxy/.git
git clone /sshproxy /tmp/sshproxy
cd /tmp/sshproxy
git archive --format=tar --prefix="${SSHPROXY_FULLNAME}/" "${SSHPROXY_COMMIT}" | gzip -c > "/root/rpmbuild/SOURCES/${SSHPROXY_FULLNAME}.tar.gz"

# Compile and install RPM
cp /sshproxy/misc/sshproxy.spec /root/rpmbuild/SPECS/
cd /root/rpmbuild
rpmbuild -ba SPECS/sshproxy.spec
yum -y install RPMS/x86_64/sshproxy*.rpm

# sshproxy configuration
cat <<EOF >/etc/sshproxy/sshproxy.yaml
---
debug: true
log: /tmp/sshproxy-{user}.log
max_connections_per_user: 0
environment:
    XMODIFIERS: globalEnv_{user}
ssh:
    args: ["-q", "-Y", "-o SendEnv=XMODIFIERS"]

translate_commands:
    "/usr/libexec/openssh/sftp-server":
        ssh_args:
            - "-oForwardX11=no"
            - "-oForwardAgent=no"
            - "-oPermitLocalCommand=no"
            - "-oClearAllForwardings=yes"
            - "-oProtocol=2"
            - "-s"
        command: "sftp"
        disable_dump: true

etcd:
    endpoints: ["https://etcd:2379"]
    tls:
        cafile: "/etc/etcd/ca.pem"
        keyfile: "/etc/etcd/sshproxy-key.pem"
        certfile: "/etc/etcd/sshproxy.pem"
    username: "sshproxy"
    password: "sshproxy"
    keyttl: 1
    mandatory: false

service: default
dest: ["server3"]

overrides:
    - match:
          - sources: ["gateway1:2022", "gateway2:2022"]
      service: service1
      dest: ["server[1-2]"]
      route_select: ordered
      mode: sticky
      etcd_keyttl: 0
    - match:
          - sources: ["gateway1:2023"]
      service: service2
      dest: ["server1"]
    - match:
          - sources: ["gateway1:2024"]
      service: service3
      dest: ["server2"]
      environment:
          XMODIFIERS: serviceEnv_{user}
    - match:
          - sources: ["gateway2:2023"]
      service: sftp
      dest: ["server1"]
      force_command: "/usr/libexec/openssh/sftp-server"
      command_must_match: true
    - match:
          - sources: ["gateway1:2023"]
            groups: [user1, unknowngroup]
          - sources: ["gateway1:2023"]
            users: [unknownuser]
      service: service2
      dest: ["server2"]
    - match:
          - groups: [unknowngroup]
          - users: [unknownuser, user2]
      environment:
          XMODIFIERS: globalUserEnv_{user}
    - match:
          - sources: ["gateway1:2024"]
            groups: [unknowngroup]
          - sources: ["gateway1:2024"]
            users: [unknownuser, user2]
      service: service3
      dest: ["server1"]
      environment:
          XMODIFIERS: serviceUserEnv_{user}
EOF

exec "$@"
