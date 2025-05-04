# Changelog

## [1.4.0](https://github.com/dimonomid/nerdlog/compare/v1.3.0...v1.4.0) (2025-05-04)


### Features

* Implement ssh authentication via public keys in addition to ssh-agent ([435eeab](https://github.com/dimonomid/nerdlog/commit/435eeab28bd48dbb71c3cd86647e11a32e8dce78))
* Don't fail if there is a `Match` directive in ssh config ([3dad67c](https://github.com/dimonomid/nerdlog/commit/3dad67ceb5e6314ca51a3daf051ba52a62da444f))
* **UI:** Resize the connection dialog when needed ([e85811f](https://github.com/dimonomid/nerdlog/commit/e85811f4e03c756a717b874a8f6a43f8dae3cb13))


### Bug Fixes

* FreeBSD is tested and supported both as a client (where the Nerdlog app runs) and as a remote host (where logs are collected from):
    * Make the agent script work on FreeBSD ([bcb3ac4](https://github.com/dimonomid/nerdlog/commit/bcb3ac4206722aa98f80b9e567771c5ba371a878))
    * Fix FreeBSD build with `CGO_ENABLED=1` ([db2665f](https://github.com/dimonomid/nerdlog/commit/db2665fc4ab68a772833cd34a2d016ed0dcaa74d))
* Don't use localhost as a default on Windows ([03e7d96](https://github.com/dimonomid/nerdlog/commit/03e7d96dccef4e685b6c362c38f7d7888b25fc46))


## [1.3.0](https://github.com/dimonomid/nerdlog/compare/v1.2.4...v1.3.0) (2025-05-03)


### Features

* Implement proper support for localhost ([82425bf](https://github.com/dimonomid/nerdlog/commit/82425bfe4b53494e217030d96efe335f6affe5be))
* Implement refresh using Ctrl+R or F5 ([6c95ca5](https://github.com/dimonomid/nerdlog/commit/6c95ca5716212f5a22f946a31a60726f9769608f))
* Implement hard refresh using Alt+Ctrl+R or Shift+F5 ([6799aca](https://github.com/dimonomid/nerdlog/commit/6799aca0844e384e882566938d9cf5f5dc5c98eb))


### Bug Fixes

* Don't assume that bash is in /bin ([ddcde2b](https://github.com/dimonomid/nerdlog/commit/ddcde2bdacc2d9d4140f3490ef463d10a8f0ca0e))
* Handle multiline records from journalctl ([af591a0](https://github.com/dimonomid/nerdlog/commit/af591a089ee66b0f5d8cba411faaea4dd15c56e3))
* **UI:** Apply the timezone changes everywhere in UI ([8b523f6](https://github.com/dimonomid/nerdlog/commit/8b523f6be63c26d736bc4baa956bba5717c8e0fb))
* **UI:** Fix timezone label in query edit form ([e3d7f26](https://github.com/dimonomid/nerdlog/commit/e3d7f26a2e8b40e206bf1cb021f1a6de1a174f4d))

## [1.2.4](https://github.com/dimonomid/nerdlog/compare/v1.2.3...v1.2.4) (2025-04-28)


### Bug Fixes

* Avoid panic if nerdlog was built with CGO_ENABLED=0 ([389a193](https://github.com/dimonomid/nerdlog/commit/389a193836b8e07de71cf7f9bf39fb8441e2871b))

## [1.2.3](https://github.com/dimonomid/nerdlog/compare/v1.2.2...v1.2.3) (2025-04-28)


### Bug Fixes

* **CI:** Install libx11-dev to the runner ([adde3f5](https://github.com/dimonomid/nerdlog/commit/adde3f5a12d79ced4ad5c5b8dcdbdff568086147))

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
