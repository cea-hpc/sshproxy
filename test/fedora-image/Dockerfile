FROM fedora:39

# Install development environment to compile RPM
RUN set -ex \
	&& yum -y update \
	&& yum -y install asciidoc etcd git golang hostname iproute make openssh-server rpm-build procps

# Create fedora, user1 and user2 users ; fedora and user1 groups
RUN set -ex \
	&& useradd fedora \
	&& install -d -m0755 -o fedora -g fedora /home/fedora/.ssh \
	&& useradd user1 \
	&& install -d -m0755 -o user1 -g user1 /home/user1/.ssh \
	&& useradd -g user1 user2 \
	&& install -d -m0755 -o user2 -g user1 /home/user2/.ssh

# Copy fedora public key to root authorized_keys
RUN set -ex && install -d -m0700 /root/.ssh
COPY ./ssh/id_ed25519.pub /root/.ssh/authorized_keys
RUN chmod 0600 /root/.ssh/authorized_keys

# Copy sshd keys
COPY ./ssh/ssh_config /etc/ssh/
RUN chmod 0644 /etc/ssh/ssh_config
COPY ./ssh/ssh_host_ed25519_key* /etc/ssh/
RUN chmod 0600 /etc/ssh/ssh_host_ed25519_key

# Copy fedora ssh keys
COPY --chown=fedora:fedora ./ssh/id_ed25519.pub /home/fedora/.ssh/authorized_keys
COPY --chown=fedora:fedora ./ssh/id_ed25519* ./ssh/known_hosts /home/fedora/.ssh/
RUN chmod 0600 /home/fedora/.ssh/id_ed25519 /home/fedora/.ssh/authorized_keys

# Copy user1 ssh keys
COPY --chown=user1:user1 ./ssh/id_ed25519.pub /home/user1/.ssh/authorized_keys
COPY --chown=user1:user1 ./ssh/id_ed25519* ./ssh/known_hosts /home/user1/.ssh/
RUN chmod 0600 /home/user1/.ssh/id_ed25519 /home/user1/.ssh/authorized_keys

# Copy user2 ssh keys
COPY --chown=user2:user1 ./ssh/id_ed25519.pub /home/user2/.ssh/authorized_keys
COPY --chown=user2:user1 ./ssh/id_ed25519* ./ssh/known_hosts /home/user2/.ssh/
RUN chmod 0600 /home/user2/.ssh/id_ed25519 /home/user2/.ssh/authorized_keys


# Copy etcd certificates and keys
COPY ./etcd/*.pem /etc/etcd/
RUN chmod 0644 /etc/etcd/ca.pem
RUN chmod 0644 /etc/etcd/sshproxy*

# Copy sshd configurations
COPY ./ssh/sshd_config.* /etc/ssh/

# Copy entrypoint for gateway
COPY ./gateway.sh /root

# Copy entrypoint for etcd
COPY ./etcd.sh /root

# Copy test file for tester
COPY --chown=fedora:fedora ./sshproxy_test.go /home/fedora/
