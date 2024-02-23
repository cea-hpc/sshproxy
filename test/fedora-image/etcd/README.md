Certificates were created using [cfssl](https://github.com/cloudflare/cfssl):

```
$ cfssl genkey -initca ca-csr.json | cfssljson -bare ca
$ cfssl gencert -ca ca.pem -ca-key ca-key.pem etcd-csr.json | cfssljson -bare etcd
$ cfssl gencert -ca ca.pem -ca-key ca-key.pem sshproxy-csr.json | cfssljson -bare sshproxy
```
