# Changelog

## [1.2.2](https://github.com/dimonomid/nerdlog/compare/v1.2.1...v1.2.2) (2025-04-28)


### Bug Fixes

* **CI:** Don't set CGO_ENABLED=0 ([201f621](https://github.com/dimonomid/nerdlog/commit/201f621f8d7c2472a098051dc4b93a233874f395))

## [1.2.1](https://github.com/dimonomid/nerdlog/compare/v1.2.0...v1.2.1) (2025-04-28)


### Bug Fixes

* **CI:** Fix the path to main package ([2ae940b](https://github.com/dimonomid/nerdlog/commit/2ae940b851bcb56c058c2286bc43b589a684c80d))

## [1.2.0](https://github.com/dimonomid/nerdlog/compare/v1.1.0...v1.2.0) (2025-04-28)


### Features

* Add support for `journalctl` ([6d7d69](https://github.com/dimonomid/nerdlog/commit/6d7d695ced450e1648994e690dd26a503b4fe034), [1687ee](https://github.com/dimonomid/nerdlog/commit/1687ee728387d838c9ec56d40b3b2a3d9acf7901)). Log files are still preferred, because [they are more reliable](https://github.com/dimonomid/nerdlog/issues/7#issuecomment-2820521885) and [work much faster](https://github.com/dimonomid/nerdlog/issues/7#issuecomment-2823303380), but `journalctl` is more universally available these days, and also often has longer logs history, so it is fully supported now.
* Add support for reading logs with `sudo` ([23a6a4](https://github.com/dimonomid/nerdlog/commit/23a6a4e6b48da8658fcfd0eefb0b2193ba389a13))
* Add `--ssh-config` flag to specify the ssh config location ([3ceb70](https://github.com/dimonomid/nerdlog/commit/3ceb70b803bff5b47e3982b8dd202516d2bbd538))
* **CI:** set up release-please with auto binaries building for releases ([2a9d23](https://github.com/dimonomid/nerdlog/commit/2a9d2353d3050e75d727825c6db86d09229b5d97))

### Bug Fixes

* Fix focus issue when non-last modal is removed ([2e4ff3](https://github.com/dimonomid/nerdlog/commit/2e4ff3d35f7e473283b7afe7671d6e0e180d2dac))

## 1.1.0 (2025-04-26)

### Features

* Support keyboard shortcuts `Alt+Left` and `Alt+Right` for navigating
  browser-like history, just as it works in a browser;
* Rename the binary installed with `go install
  github.com/dimonomid/nerdlog/cmd/nerdlog@latest` from `nerdlog-tui` to just
  `nerdlog`.

## 1.0.0 (2025-04-22)

### Features

* First release which can be considered a minimal viable product.

  There’s still plenty of room for new features, and some minor bug fixes, but
  overall the core functionality is in place and stable enough to be released
  into the wild.
