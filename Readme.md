# tls-client (fork)

This is a fork of [bogdanfinn/tls-client](https://github.com/bogdanfinn/tls-client). Its only purpose is to **build cross-platform native binaries** (Linux, macOS, Windows, Android) and publish them as GitHub release assets so that [kotlin-tls-client](https://github.com/PianoNic/kotlin-tls-client) can consume them at build time.

If you are looking for the actual TLS-client library — documentation, examples, contributing, issue tracking — go to **[bogdanfinn/tls-client](https://github.com/bogdanfinn/tls-client)**. This fork makes no source-code changes; it only adds the build pipeline.

## What this fork does

1. Syncs `master` from upstream daily via the GitHub merge-upstream API.
2. When a new upstream release tag appears (or any expected ABI zip is missing on the corresponding release), runs `cffi_dist` builds with `CGO_ENABLED=1` for every supported platform/ABI.
3. Publishes the zipped shared libraries as assets on a release tagged with the upstream version (`vX.Y.Z`).

The pipeline lives in [`.github/workflows/sync-and-build.yml`](.github/workflows/sync-and-build.yml).

## Released artifacts

For every upstream tag, the workflow publishes one zip per target:

| Zip | Artifact inside | Target |
|---|---|---|
| `linux-x86_64.zip` | `libtls_client_go.so` | Linux x86_64 |
| `linux-aarch64.zip` | `libtls_client_go.so` | Linux ARM64 |
| `macos-x86_64.zip` | `libtls_client_go.dylib` | macOS Intel |
| `macos-arm64.zip` | `libtls_client_go.dylib` | macOS Apple Silicon |
| `windows-x86_64.zip` | `tls_client_go.dll` | Windows x86_64 |
| `arm64-v8a.zip` | `libtls_client_go.so` | Android arm64 |
| `armeabi-v7a.zip` | `libtls_client_go.so` | Android armv7 |
| `x86.zip` | `libtls_client_go.so` | Android x86 |
| `x86_64.zip` | `libtls_client_go.so` | Android x86_64 |

All binaries are produced by `go build -buildmode=c-shared` from `cffi_dist`.

## Where this is consumed

[`kotlin-tls-client`](https://github.com/PianoNic/kotlin-tls-client) downloads these zips at Gradle build time, unpacks them into `dev/kotlintls/natives/<platform>/`, and bundles them inside its published JAR. The pinned version lives in [`natives-version.txt`](https://github.com/PianoNic/kotlin-tls-client/blob/main/natives-version.txt) on the consumer side.

## Reporting issues

- **TLS-client behavior or bugs** → upstream: <https://github.com/bogdanfinn/tls-client/issues>
- **The build pipeline in this fork** → here.

## License

This fork inherits the license of the upstream project.
