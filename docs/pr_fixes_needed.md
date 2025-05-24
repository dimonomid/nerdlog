# PR Fixes Needed Based on Reviewer Feedback

## Issues Identified by @dimonomid

### 1. Remove SSH Config Changes (As Agreed)
- ❌ `core/ssh_match_evaluator.go` - Should not be in this PR
- ❌ `cmd/nerdlog/ssh_config_preprocessor.go` - Should not be in this PR
- ❌ `cmd/nerdlog/ssh_match_filter.go` - Should not be in this PR

### 2. Fix Interface Design Issues
- ❌ `GetAuthMethod() (interface{}, error)` - Should return `ssh.AuthMethod` directly
- ❌ Unused `EphemeralKeyProviderEnabled` option - Remove entirely

### 3. Fix Error Handling
- ❌ Use `github.com/juju/errors` instead of standard errors
- ❌ Use `errors.Annotatef(err, "obtaining ephemeral key from opkssh")` pattern

### 4. Use Go API Instead of CLI
- ❌ `exec.Command(p.OpksshPath, "key", "export", "--private")` - Use opkssh Go API

### 5. Fix Documentation
- ❌ `docs/ephemeral_ssh_key_integration_plan.md` - Should be user documentation, not integration plan

## Action Plan

### Step 1: Clean Up SSH Config Code
Remove all SSH config related files that were supposed to be in separate PR:
- Delete `core/ssh_match_evaluator.go`
- Delete `cmd/nerdlog/ssh_config_preprocessor.go` 
- Delete `cmd/nerdlog/ssh_match_filter.go`
- Remove SSH config changes from `cmd/nerdlog/app.go`

### Step 2: Fix Interface Design
```go
// Current (wrong):
GetAuthMethod() (interface{}, error)

// Fixed:
GetAuthMethod() (ssh.AuthMethod, error)
```

### Step 3: Remove Unused Option
- Remove `EphemeralKeyProviderEnabled` from options
- Remove all related parsing logic
- Simplify to just `EphemeralKeyProvider string`

### Step 4: Fix Error Handling
```go
// Current (wrong):
return nil, errors.New("failed to obtain ephemeral key from opkssh: " + err.Error())

// Fixed:
return nil, errors.Annotatef(err, "obtaining ephemeral key from opkssh")
```

### Step 5: Use opkssh Go API
Research and implement direct Go API usage instead of CLI execution.

### Step 6: Fix Documentation
Convert integration plan to user-focused documentation explaining how to use the feature.

## Implementation Priority
1. Remove SSH config code (highest priority - as agreed)
2. Fix interface design issues
3. Fix error handling
4. Improve opkssh integration
5. Update documentation

This will result in a clean, focused PR that only adds ephemeral SSH key support without the SSH config preprocessing that belongs in the upstream library contribution.
