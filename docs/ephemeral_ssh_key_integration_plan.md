# Ephemeral SSH Key Support

Nerdlog supports ephemeral SSH keys for enhanced security when connecting to remote hosts. Instead of using persistent SSH keys stored on disk, ephemeral keys are generated on-demand and automatically expire.

## What are Ephemeral SSH Keys?

Ephemeral SSH keys are temporary cryptographic keys that:
- Are generated on-demand for each session
- Never touch the disk (stored only in memory)
- Automatically expire after a short time
- Are backed by identity providers via OIDC authentication

This approach eliminates many security risks associated with traditional SSH key management.

## Configuration

### Command Line

Use the `--ephemeral-key-provider` flag to specify which provider to use:

```bash
# Use mock provider (for testing)
nerdlog --ephemeral-key-provider=mock --lstreams localhost

# Use opkssh provider (requires setup)
nerdlog --ephemeral-key-provider=opkssh --lstreams myhost
```

### Configuration File

Add to your `~/.config/nerdlog/config.yaml`:

```yaml
ephemeral_key_provider: opkssh
```

### Runtime Configuration

You can also change the provider at runtime using the `:set` command:

```
:set ephemeralkeyprovider=mock
:set ephemeralkeyprovider=opkssh
:set ephemeralkeyprovider=  # disable
```

## Available Providers

### Mock Provider (`mock`)

A testing provider that generates RSA key pairs in memory. Useful for:
- Testing the ephemeral key feature
- Development and debugging
- Verifying the authentication flow

**Usage**: No setup required, just specify `--ephemeral-key-provider=mock`

### opkssh Provider (`opkssh`)

Integrates with [opkssh](https://github.com/openpubkey/opkssh) for OIDC-based ephemeral keys.

**Prerequisites**:
1. Install opkssh: `go install github.com/openpubkey/opkssh@latest`
2. Configure your OIDC provider
3. Ensure SSH servers trust the ephemeral key CA

**Usage**: `--ephemeral-key-provider=opkssh`

## Authentication Flow

When ephemeral key support is enabled, Nerdlog will:

1. **Try ephemeral key first**: Generate and use an ephemeral key for authentication
2. **Fallback gracefully**: If ephemeral key fails, fall back to traditional methods:
   - SSH agent
   - Private key files
   - Password authentication

This ensures compatibility with existing setups while providing enhanced security when available.

## Example Usage

```bash
# Quick test with mock provider
nerdlog --ephemeral-key-provider=mock --lstreams localhost

# Production usage with opkssh
nerdlog --ephemeral-key-provider=opkssh --lstreams 'web-*,db-*'

# Disable ephemeral keys (use traditional auth only)
nerdlog --lstreams myhost  # or --ephemeral-key-provider=""
```

## Troubleshooting

### Mock Provider Issues
- **Error**: "ephemeral key not available"
  - **Solution**: Ensure you're using `--ephemeral-key-provider=mock`

### opkssh Provider Issues
- **Error**: "obtaining ephemeral key from opkssh"
  - **Solution**: Verify opkssh is installed and in PATH
  - **Solution**: Check OIDC provider configuration
  - **Solution**: Ensure SSH server trusts the ephemeral key CA

### General Issues
- **Fallback behavior**: If ephemeral keys fail, Nerdlog automatically falls back to traditional SSH authentication
- **Debugging**: Use `--log-level=debug` for detailed authentication logs

## Security Benefits

- **No persistent keys**: Keys never touch the disk
- **Automatic expiration**: Keys are short-lived
- **Identity-based**: Tied to your identity provider
- **Audit trail**: Better tracking of key usage
- **Reduced attack surface**: No long-lived credentials to compromise

## Limitations

- **Experimental feature**: Currently in experimental status
- **Setup required**: opkssh provider requires additional configuration
- **Server support**: SSH servers must be configured to trust ephemeral key CAs
- **Network dependency**: Requires connectivity to identity provider
