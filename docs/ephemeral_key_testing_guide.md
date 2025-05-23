# Ephemeral SSH Key Testing Guide

This guide provides instructions for setting up a test environment to verify the ephemeral SSH key functionality in Nerdlog.

## Overview

The ephemeral key feature allows Nerdlog to use temporary SSH keys generated at runtime instead of persistent keys stored on disk. This improves security by reducing the risk of key compromise.

## Testing Approaches

### 1. Mock Provider Testing (Recommended for Development)

The simplest way to test the ephemeral key functionality is using the built-in mock provider.

#### Setup

No additional setup required - the mock provider is included in the codebase.

#### Usage

```bash
# Run nerdlog with the mock ephemeral key provider
nerdlog --ephemeral-key-provider=mock --lstreams myhost

# Or configure in ~/.config/nerdlog/config.yaml
ephemeral_key_provider: mock
```

#### What the Mock Provider Does

- Simulates the ephemeral key generation process
- Returns a test SSH key pair
- Logs key generation events for debugging
- Allows testing the full authentication flow without external dependencies

### 2. Local opkssh Testing

For more realistic testing, you can set up opkssh locally.

#### Prerequisites

- Go 1.19 or later
- A local OIDC provider (e.g., Keycloak, Dex, or mock OIDC server)
- SSH server configured to accept ephemeral keys

#### Setup Steps

1. **Install opkssh**:
   ```bash
   go install github.com/openpubkey/opkssh@latest
   ```

2. **Set up a local OIDC provider** (example using Dex):
   ```bash
   # Clone and run Dex
   git clone https://github.com/dexidp/dex.git
   cd dex
   make build
   ./bin/dex serve examples/config-dev.yaml
   ```

3. **Configure opkssh**:
   ```bash
   # Create opkssh config
   cat > ~/.opkssh/config.yaml <<EOF
   oidc:
     issuer: http://localhost:5556/dex
     client_id: example-app
     client_secret: ZXhhbXBsZS1hcHAtc2VjcmV0
   EOF
   ```

4. **Configure SSH server** to accept ephemeral keys:
   ```bash
   # Add to /etc/ssh/sshd_config
   TrustedUserCAKeys /etc/ssh/ephemeral_ca.pub
   ```

5. **Run Nerdlog with opkssh**:
   ```bash
   nerdlog --ephemeral-key-provider=opkssh --lstreams testhost
   ```

### 3. Docker-based Test Environment

For isolated testing, use Docker containers.

#### docker-compose.yml

```yaml
version: '3.8'

services:
  # Mock OIDC Provider
  oidc-provider:
    image: ghcr.io/navikt/mock-oauth2-server:latest
    ports:
      - "8080:8080"
    environment:
      - JSON_CONFIG_PATH=/config.json
    volumes:
      - ./oidc-config.json:/config.json

  # SSH Server with ephemeral key support
  ssh-server:
    build:
      context: .
      dockerfile: Dockerfile.sshd
    ports:
      - "2222:22"
    volumes:
      - ./ssh_config:/etc/ssh/sshd_config.d/
      - ./ephemeral_ca.pub:/etc/ssh/ephemeral_ca.pub

  # Test client
  test-client:
    build:
      context: .
      dockerfile: Dockerfile.client
    depends_on:
      - oidc-provider
      - ssh-server
    environment:
      - OIDC_ISSUER=http://oidc-provider:8080
    command: sleep infinity
```

#### Dockerfile.sshd

```dockerfile
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y \
    openssh-server \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir /var/run/sshd

# Configure SSH for ephemeral keys
RUN echo "TrustedUserCAKeys /etc/ssh/ephemeral_ca.pub" >> /etc/ssh/sshd_config

EXPOSE 22
CMD ["/usr/sbin/sshd", "-D"]
```

#### Running the Test Environment

```bash
# Start the environment
docker-compose up -d

# Run tests
docker-compose exec test-client bash
# Inside the container:
nerdlog --ephemeral-key-provider=opkssh --lstreams ssh-server
```

## Unit Testing

### Running Existing Tests

```bash
# Run all tests
go test ./...

# Run specific ephemeral key tests
go test -v ./core -run TestEphemeralKey

# Run with coverage
go test -v -cover ./core
```

### Writing New Tests

Example test for ephemeral key functionality:

```go
func TestEphemeralKeyAuthentication(t *testing.T) {
    // Create mock provider
    provider := &EphemeralKeyProviderMock{}
    
    // Create SSH transport with ephemeral key support
    transport := &ShellTransportSSH{
        ephemeralKeyProvider: provider,
    }
    
    // Test authentication
    err := transport.Connect()
    assert.NoError(t, err)
    
    // Verify key was generated
    assert.True(t, provider.KeyGenerated)
}
```

## GitHub Actions Integration

### .github/workflows/ephemeral-key-tests.yml

```yaml
name: Ephemeral Key Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    
    services:
      oidc:
        image: ghcr.io/navikt/mock-oauth2-server:latest
        ports:
          - 8080:8080
    
    steps:
    - uses: actions/checkout@v3
    
    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.21'
    
    - name: Install dependencies
      run: |
        sudo apt-get update
        sudo apt-get install -y openssh-server
    
    - name: Configure SSH server
      run: |
        sudo mkdir -p /etc/ssh/sshd_config.d/
        echo "TrustedUserCAKeys /tmp/ephemeral_ca.pub" | sudo tee /etc/ssh/sshd_config.d/ephemeral.conf
        sudo systemctl restart sshd
    
    - name: Run ephemeral key tests
      run: |
        go test -v ./core -run TestEphemeralKey
      env:
        OIDC_ISSUER: http://localhost:8080
```

## Debugging

### Enable Debug Logging

```bash
# Set debug environment variable
export NERDLOG_DEBUG=1

# Or use the debug flag
nerdlog --debug --ephemeral-key-provider=mock --lstreams myhost
```

### Common Issues

1. **Key generation fails**
   - Check OIDC provider connectivity
   - Verify opkssh configuration
   - Check SSH server CA configuration

2. **Authentication rejected**
   - Verify SSH server trusts the CA
   - Check key validity period
   - Ensure user principal matches

3. **Mock provider not working**
   - Verify the provider name is exactly "mock"
   - Check for typos in configuration

### Viewing Generated Keys

When using the mock provider with debug enabled:

```bash
NERDLOG_DEBUG=1 nerdlog --ephemeral-key-provider=mock --lstreams localhost
# Look for log lines like:
# DEBUG: Generated ephemeral key: ssh-rsa AAAAB3...
```

## Security Considerations

- The mock provider should **never** be used in production
- Test keys should be regularly rotated
- Ensure test OIDC providers are not accessible from production networks
- Use separate CA keys for testing and production

## Next Steps

After successful testing:

1. Document the production setup process
2. Create monitoring for key generation failures
3. Implement key rotation policies
4. Set up alerts for authentication failures
