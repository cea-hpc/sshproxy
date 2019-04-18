FROM centos:7

# Install development environment to compile RPM
RUN set -ex \
	&& yum -y install https://dl.fedoraproject.org/pub/epel/epel-release-latest-7.noarch.rpm \
	&& yum -y update \
	&& yum -y install asciidoc etcd git golang iproute make nmap-ncat openssh-server rpm-build

# Create centos user and group
RUN set -ex \
	&& useradd centos \
	&& install -d -m0755 -o centos -g centos /home/centos/.ssh

# Copy centos public key to root authorized_keys
RUN set -ex && mkdir -p /root/.ssh
COPY ./ssh/id_ed25519.pub /root/.ssh/authorized_keys

# Copy centos ssh keys
COPY --chown=centos:centos ./ssh/id_ed25519.pub /home/centos/.ssh/authorized_keys
COPY --chown=centos:centos ./ssh/id_ed25519* ./ssh/known_hosts /home/centos/.ssh/
COPY ./ssh/ssh_config /etc/ssh/
COPY ./ssh/ssh_host_ed25519_key* /etc/ssh/
RUN chmod 0600 /home/centos/.ssh/id_ed25519 /etc/ssh/ssh_host_ed25519_key

# Copy etcd certificates and keys
COPY ./etcd/*.pem /etc/etcd/
RUN chmod 0644 /etc/etcd/sshproxy*

# Copy wait-for script
COPY ./wait-for /usr/bin/

# Copy sshd configurations
COPY ./ssh/sshd_config.* /etc/ssh/

# Copy entrypoint for gateway
COPY ./gateway.sh /root

# Copy entrypoint for etcd
COPY ./etcd.sh /root

# Copy test file for tester
COPY --chown=centos:centos ./sshproxy_test.go /home/centos/
