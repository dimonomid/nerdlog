# Options

Options can be set or checked using `:set` command, for example:

- `:set numlines?` prints the current value of the `numlines` option
- `:set numlines=1000` sets `numlines` to the new value

So far there is no support for a persistent config file where we can specify initial values of these options, but you can provide them using the `--set` flag, like this:

```
$ nerdlog --set 'numlines=1000' --set 'transport=ssh-bin'
```

Currently supported options are:

### `numlines`

The number of log messages loaded from every logstream on every request. Default: 250.

### `timezone`

The timezone to format the timestamps on the UI. By default, `Local` is used, but you can specify `UTC` or `America/New_York` etc.

### `transport`

Specifies what to use to connect to remote hosts (has no effect on `localhost`: this one always goes via local shell).

Valid values are:

#### `ssh-lib`

Use internal Go ssh implementation (the [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh) library). This is what Nerdlog was using from the day 1, but it's pretty limited in terms of configuration; e.g. if you have more or less advanced ssh configuration, chances are that Nerdlog won't be able to fully parse it. Only some minimal parsing of `~/.ssh/config` is done.

#### `ssh-bin`

Use external `ssh` binary. This is still a bit experimental, but a lot more comprehensive. The only observable limitation here is that if the ssh agent is not running, and ssh key is encrypted, then with `ssh-lib` Nerdlog would ask you for the key passphrase interactively, while with `ssh-bin` the connection will just fail.

With `ssh-bin`, Nerdlog also uses the ssh config a bit differently: it only uses the list of hosts parsed from the ssh config to implement globs, so e.g. if your ssh config has two hosts `my-01` and `my-02`, then typing `my-*` in logstreams input would make Nerdlog connect to both of them. But, Nerdlog won't try to figure out the actual hostname, or usename, or port from the ssh config: it would simply run the command like `ssh -o 'BatchMode=yes' my-01 /bin/sh`, leaving all the config parsing up to that `ssh` binary.

However, the Nerdlog's own logstreams config is still interpreted as before; so if in that config you have e.g. this:

```yaml
log_streams:
  my-01:
    hostname: myactualserver.com
    port: 1234
    user: myuser
```

Then the ssh command will actually be: `ssh -p 1234 -o 'BatchMode=yes' myuser@myactualserver.com /bin/sh`

For now, `ssh-lib` is still the default, but the plan is to change that at some point and make `ssh-bin` the default if `ssh` binary is available.

#### `custom:<arbitrary command>`

The most flexible and advanced option: run the specified arbitrary command to connect to a remote host. This is still experimental. That command must start a POSIX shell session, like `/bin/sh` or any other compatible shell.

The main use case for it is to support Teleport or other similar tools which require more advanced authentication than the plain `ssh`.

That command is parsed in a shell-like way, where variable expansions are supported, like `${MYVAR}`, `${MYVAR:-default}`, or `${MYVAR:+alternative}`. But it's not a real shell, so e.g. logic constructs like `if`, `while` etc aren't supported. The `$(another_command)` also isn't supported. If you want to run it in a real shell, just invoke the shell explicitly, like `custom:/bin/sh -c "<arbitrary shell script>"`.

The following Nerdlog-specific variables are available for the command:

- `NLHOST`: hostname. Always present (comes from either the logstreams input, or the matched item in the logstreams config).
- `NLPORT`: port. Only present if it was specified in the logstreams input, or in the logstreams config.
- `NLUSER`: username. Only present if it was specified in the logstreams input, or in the logstreams config.

In addition to these Nerdlog-specific ones, all environment variables are also available.

Here's an example of a valid custom command which is doing exactly the same as `ssh-bin` would:

```
custom:ssh -o 'BatchMode=yes' ${NLPORT:+-p ${NLPORT}} ${NLUSER:+${NLUSER}@}${NLHOST} /bin/sh
```

And just like with `ssh-bin`, with the custom command, Nerdlog won't try to figure out the actual hostname, username or port from the ssh config. Only the Nerdlog's own logstreams config matters here, while ssh config is only used for globbing and nothing else, relying on the external command to parse ssh config if needed.
