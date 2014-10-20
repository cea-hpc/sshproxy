# sshproxy

## Compilation

* Install the Go compiler suite: see http://golang.org/doc/install for details.

* Fetch the dependencies (from the sshproxy source directory, git and Internet
  access are required):

    $ mkdir $HOME/go
    $ export GOPATH=$HOME/go
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

A table `ssh` specifies the SSH options:

* `exe`: path or command to use for the SSH client (`ssh` by default).

* `args`: a list of arguments for the SSH client. Its default value is: `["-q",
  "-Y"]`.

* `destination`: the host destination for the SSH client. This is a required
  argument.

Each of the previous parameters can be overridden for a user thanks to a
`users` sub-table. For example if we want to save debug messages for the `foo`
user we define:

```
[users.foo]
debug = true
```

To modify the SSH options we use another sub-table:

```
[users.foo.ssh]
args = ["-vvv", "-Y"]
```
