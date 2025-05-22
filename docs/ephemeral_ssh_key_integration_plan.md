# Ephemeral SSH Key Integration Plan for Nerdlog

## Overview

This document outlines a high-level design for integrating ephemeral SSH key support into Nerdlog, leveraging technologies like opkssh that provide ephemeral keys and modern authentication methods (e.g., passkeys, OpenID Connect).

## Goals

- Enable Nerdlog to authenticate to remote hosts using ephemeral SSH keys generated at runtime.
- Support alternative authentication flows that do not require persistent SSH keys on client devices.
- Maintain backward compatibility with existing SSH authentication methods (ssh-agent, private keys).
- Provide a seamless user experience for configuring and using ephemeral keys.

## Proposed Architecture

1. **External Ephemeral Key Provider Integration**

   - Integrate with an external ephemeral key provider (e.g., opkssh client or daemon).
   - Provide an interface in Nerdlog to request ephemeral keys and authentication tokens.
   - Support configuration options to enable/disable ephemeral key usage.

2. **SSH Authentication Flow Modification**

   - Extend the SSH authentication method selection to include ephemeral keys.
   - When ephemeral keys are enabled, request keys from the provider before establishing SSH connections.
   - Use the ephemeral keys dynamically in the SSH client configuration.

3. **Fallback Mechanisms**

   - If ephemeral key authentication fails or is disabled, fallback to existing methods (ssh-agent, private keys).
   - Provide clear error messages and guidance to users.

4. **User Experience**

   - Add CLI flags and config options to enable ephemeral key support.
   - Document setup and usage instructions.
   - Optionally integrate with Nerdlog UI to manage ephemeral key sessions.

## Next Steps

- Prototype the ephemeral key provider interface.
- Modify SSH client config generation to support ephemeral keys.
- Test integration with opkssh or similar tools.
- Update documentation and provide examples.

## References

- [opkssh GitHub Repository](https://github.com/openpubkey/opkssh)
- Nerdlog current SSH authentication implementation in `core/shell_transport_ssh.go`

---

This plan is open for review and feedback before implementation.

## Implementation Details

### Ephemeral Key Provider Interface

- Define an interface in Nerdlog to abstract ephemeral key retrieval.
- Implement a mock provider for testing.
- Future implementations can integrate with opkssh or other providers.

### SSH Client Configuration

- Modify SSH client config generation to optionally use ephemeral keys.
- Prioritize ephemeral keys over ssh-agent and private keys.
- Ensure fallback to existing methods if ephemeral keys are unavailable.

### User Configuration

- Add CLI flags and config file options to enable or disable ephemeral key usage.
- Provide options to specify ephemeral key provider parameters.

### Error Handling and Logging

- Provide clear error messages when ephemeral key retrieval or usage fails.
- Log authentication method used for transparency.

### Testing

- Unit tests for ephemeral key provider interface and SSH auth flow.
- Integration tests with mock and real ephemeral key providers.
- Test fallback scenarios and error conditions.

### Documentation

- Update README and docs with setup and usage instructions.
- Provide examples and troubleshooting tips.

## Future Work

- Full evaluation and support for SSH `Match` directive in config parsing.
- UI integration for managing ephemeral key sessions.
- Support for additional authentication methods (e.g., OIDC, FIDO2).
