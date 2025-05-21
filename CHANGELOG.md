# Changelog

## [Unreleased]

### Features
- Added preprocessing of SSH config files to support the `Include` directive, allowing recursive merging of included files before parsing.
- Added preliminary support for the `Match` directive in SSH config, filtering hosts based on conditions (full evaluation planned for future releases).

### Improvements
- Improved compatibility with complex SSH configurations by enhancing SSH config parsing in the Nerdlog client.
