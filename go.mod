module github.com/cea-hpc/sshproxy

go 1.13

require (
	github.com/coreos/etcd v3.3.10+incompatible
	github.com/docker/docker v1.4.2-0.20170504205632-89658bed64c2
	github.com/go-yaml/yaml v0.0.0-20170812160011-eb3733d160e7
	github.com/gogo/protobuf v1.1.1
	github.com/golang/protobuf v1.2.0
	github.com/kr/pty v1.0.0
	github.com/mattn/go-runewidth v0.0.3
	github.com/olekukonko/tablewriter v0.0.0-20180912035003-be2c049b30cc
	github.com/op/go-logging v0.0.0-20160211212156-b2cb9fa56473
)

replace gopkg.in/yaml.v2 eb3733d160e74a9c7e442f435eb3bea458e1d19f => github.com/go-yaml/yaml v0.0.0-20170812160011-eb3733d160e7
