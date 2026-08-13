# Dragonpilot cereal schema provenance

This package uses an exact snapshot from the user's Dragonpilot fork:

- Web source: <https://git.lirou.fun:666/gen/Dragonpilot/commits/branch/pre-build>
- Clone URL: <https://git.lirou.fun:666/gen/Dragonpilot.git>
- Branch: `pre-build`
- Commit: `21d40d72c65021c81e84a62e23d700972c7c8a7f`

## Copied files

The schema files below are copied byte-for-byte and are not annotated or
otherwise modified in this repository:

- `schema/log.capnp` — `3cfb10a1a1b44b810977cefbb55ad945b409909d9b9e6a2c0aade9a9c4adb8c3`
- `schema/car.capnp` — `bc4b32367adea7428614b761eb1525f174a2c79612ecc51ab16395ec2e0af3bf`
- `schema/custom.capnp` — `da8149adeeae6bafba017735d27910184ef4483579d693e732030a5762246775`
- `schema/deprecated.capnp` — `2ff0763df1483fd3a7faad9d658fbcac0d5b90fe8e404f6bef9698e1013a1cba`
- `schema/include/c++.capnp` — `fb306076cd38c27af1aed20fac6395e9a46fbe5b5df6a248b5f1b6845a079c44`

`LICENSE.dragonpilot.md` is an exact copy of Dragonpilot `LICENSE.md`
(SHA-256 `d2c0b49249de153c87a29eff48c99149466b13c9db30b7cafa0c57a7c5524f98`).
It contains Dragonpilot's non-commercial terms and also reproduces Comma.ai
MIT license text. `LICENSE.openpilot` is an exact copy of the source
repository's `LICENSE` (SHA-256
`716ce815a0467219c59ec2433e6bce7f32efc45240725c6d3141a52b111d2558`),
which contains the Comma.ai MIT license text. These files record the source
terms; this note does not offer a legal interpretation.

## Generation

`generate.sh` copies the five pristine schema files to a temporary directory,
adds the required Go package/import annotations only to those temporary
copies, and writes generated Go files into this package. It uses:

- Cap'n Proto compiler `1.5.0`
- `capnpc-go` from `capnproto.org/go/capnp/v3 v3.1.0-alpha.2`
- Go runtime module `capnproto.org/go/capnp/v3 v3.1.0-alpha.2`

Install the development tools and regenerate from the repository root:

```sh
brew install capnp
go install capnproto.org/go/capnp/v3/capnpc-go@v3.1.0-alpha.2
PATH="$(go env GOPATH)/bin:$PATH" go generate ./internal/replay/cereal
```

The compiler and generator are development-only. Runtime and packaged SPK
artifacts use the checked-in generated Go sources and do not require either
tool.
