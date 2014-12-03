# sshproxy

## Compilation

* Install the Go compiler suite: see http://golang.org/doc/install for details.

* Unpack the source in `$HOME/go/src` (i.e. this file should be
  `$HOME/go/src/sshproxy/README.md`).

* Fetch the dependencies (git and Internet access are required):

    $ export GOPATH=$HOME/go
    $ cd $HOME/go/src/sshproxy
    $ go get -d

  The dependencies are in `$HOME/go` and you may want to add the `GOPATH`
  directory to your `.bashrc`.

* Compile the binary:

    $ go build

## Installation

* Copy `sshproxy` binary to `/sbin` and `sshproxy.cfg` to `/etc`.

* Configure `/etc/sshproxy.cfg` to suit your needs.

* Modify the SSH daemon configuration `/etc/ssh/sshd_config` by adding

    ForceCommand /sbin/sshproxy

## Configuration

Sshproxy reads its configuration from `/etc/sshproxy.cfg`. You can specify
another configuration as its first argument.

The configuration is in the TOML format, an enhanced version of INI (see
https://github.com/toml-lang/toml for details).

The following parameters can be defined:

* `debug`: a boolean (`true` or `false`) to enable debug messages in the logs
  (`false` by default).

* `log`: a string which can be:

  - empty (`""`) to display the logs on the standard output. It is the default.

  - `"syslog"` to save logs messages through the `syslog(3)`.

  - a path to a filename. The directory must exist. The pattern `{user}` in the
    path will be replaced with the user login (eg.
    `"/var/log/sshproxy/{user}.log"`).

* `bg_command`: a string specifying a command which will be launched in the
  background for the session duration. Its standard and error outputs are only
  logged in debug mode. It is empty by default.

A table `routes` defines the destination according to the listening IP address
of the SSH daemon:

```
[routes]
192.168.0.1 = ["host1", "host2"]
192.168.0.2 = ["host3", "host4"]
default = ["host5"]
```

Each key is a listening IP address of the SSH daemon and the values are a list
of destination hosts. The definitive host will be randomly chosen. The special
key `default` can be used to define a default route.

In the previous example, a client connected to `192.168.0.1` will be proxied to
either `host1` or `host2`. If a client does not connect to `192.168.0.1` or
`192.168.0.2` it will be proxied to `host5`.

A table `ssh` specifies the SSH options:

* `exe`: path or command to use for the SSH client (`ssh` by default).

* `args`: a list of arguments for the SSH client. Its default value is: `["-q",
  "-Y"]`.

Each of the previous parameters can be overridden for a group thanks to a
`groups` sub-table.

For example if we want to save debug messages for the `foo`
group we define:

```
[groups.foo]
debug = true
```

To modify the routes or SSH options we use another sub-table:

```
[groups.foo.routes]
default = ["hostx"]

[groups.foo.ssh]
args = ["-vvv", "-Y"]
```

The routes are fully overridden and not merged with previous defined ones.

If a user belongs to several groups and these groups are defined in the
configuration file, each setting can be overridden by the next group.

For example, if a user is in the `admin` and `users` groups the logs will be in
`/var/log/sshproxy/admin/{user}.log` with the following configuration:

```
[groups.users]
log = /var/log/sshproxy/users/{user}.log

[groups.admin]
log = /var/log/sshproxy/admin/{user}.log
```

We can also override the parameters for a specific user with a `users`
sub-table.

For example if we want to save debug messages for the `foo` user we
define:

```
[users.foo]
debug = true
```

As for the groups, a sub-table is used to modify the routes or SSH options:

```
[users.foo.ssh]
args = ["-vvv", "-Y"]
```

The parameters defined for a user are the last applied and therefore always
override the settings defined by one or more `groups` tables.
