### Improved SSH Config Support

Nerdlog now preprocesses SSH config files to support the `Include` directive, allowing you to organize your SSH configuration across multiple files. This preprocessing recursively merges included files before parsing.

Additionally, preliminary support for the `Match` directive has been added, filtering SSH config hosts based on conditions. Full evaluation of `Match` conditions is planned for future releases.

To use a custom SSH config file, use the `--ssh-config` flag as before. Nerdlog will handle includes automatically.

### Ephemeral SSH Key Support (Experimental)

Nerdlog now supports ephemeral SSH keys for authentication via an external provider such as [opkssh](https://github.com/openpubkey/opkssh). This allows using runtime-generated SSH keys, improving security by avoiding persistent keys on client devices.

To enable ephemeral key support, use the `--ephemeral-key-provider` flag or configure it in the config file. Nerdlog will attempt to use ephemeral keys first, falling back to ssh-agent or private keys if unavailable.

This feature is experimental and may require additional setup. See [docs/ephemeral_ssh_key_integration_plan.md](docs/ephemeral_ssh_key_integration_plan.md) for details and usage instructions.
