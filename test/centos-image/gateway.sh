#!/bin/bash

set -eux

# Create rpmbuild directories
mkdir -p /root/rpmbuild/SOURCES
mkdir -p /root/rpmbuild/SPECS

# Find sshproxy version and branch
SSHPROXY_VERSION="$(grep '^SSHPROXY_VERSION' /sshproxy/Makefile | cut -d' ' -f3)"
SSHPROXY_FULLNAME="sshproxy-${SSHPROXY_VERSION}"
SSHPROXY_COMMIT="$(cd /sshproxy && git rev-parse HEAD)"

# Create tarball
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
log: /tmp/sshproxy.log

etcd:
    endpoints:
        - "https://etcd:2379"
    tls:
        cafile: "/etc/etcd/ca.pem"
        keyfile: "/etc/etcd/sshproxy-key.pem"
        certfile: "/etc/etcd/sshproxy.pem"
    username: "sshproxy"
    password: "sshproxy"
    keyttl: 1

routes:
    "gateway:2022": ["server1", "server2"]
    "gateway:2023": ["server1"]
    "gateway:2024": ["server2"]
    default: ["server3"]
EOF

exec "$@"
