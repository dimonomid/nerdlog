# Changelog

## [Unreleased]

### Added
- Eureka: Successfully ran tests after fixing errors in cmd/nerdlog/ssh_match_filter.go and core/shell_transport_ssh_test.go.

- eureka: Successfully ran tests after fixing errors in cmd/nerdlog/ssh_match_filter.go and core/shell_transport_ssh_test.go. [2025-05-22]

### Features
- Added preprocessing of SSH config files to support the `Include` directive, allowing recursive merging of included files before parsing.
- Added preliminary support for the `Match` directive in SSH config, filtering hosts based on conditions (full evaluation planned for future releases).
- eureka: Verified and confirmed full implementation of ephemeral SSH key support as per docs/ephemeral_ssh_key_integration_plan.md.

### Improvements
- Improved compatibility with complex SSH configurations by enhancing SSH config parsing in the Nerdlog client.
- Fixed errors in cmd/nerdlog/ssh_match_filter.go to resolve undefined types and functions, ensuring tests can run successfully.
- Addressed additional build errors in cmd/nerdlog/ssh_match_filter.go, including unused imports and type mismatches.
- Further refined cmd/nerdlog/ssh_match_filter.go to fix type mismatches and remove unused code.
