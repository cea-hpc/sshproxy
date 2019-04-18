#!/bin/bash
ETCD_DIR="/etc/etcd"
cd "${ETCD_DIR}"
exec /usr/bin/etcd \
    --initial-cluster "default=https://etcd:2380" \
    --initial-advertise-peer-urls "https://etcd:2380" \
    --listen-peer-urls "https://0.0.0.0:2380" \
    --listen-client-urls "https://0.0.0.0:2379" \
    --advertise-client-urls "https://etcd:2379" \
    --cert-file "${ETCD_DIR}/etcd.pem" \
    --key-file "${ETCD_DIR}/etcd-key.pem" \
    --client-cert-auth \
    --trusted-ca-file "${ETCD_DIR}/ca.pem" \
    --peer-cert-file "${ETCD_DIR}/etcd.pem" \
    --peer-key-file "${ETCD_DIR}/etcd-key.pem" \
    --peer-client-cert-auth \
    --peer-trusted-ca-file "${ETCD_DIR}/ca.pem"
