# k2c

The Kernel2Cloud command line.

Today it has one job: **`k2c forward`** connects your computer to a deployment's
**TCP port** (a database, a cache, a queue) on Kernel2Cloud, for any protocol,
with one of your account's API keys.

```sh
export K2C_API_KEY=k2c_key_...        # API keys page in the console
k2c forward db-chat-acme.apps.kernel2cloud.ai:443 5432
psql -h 127.0.0.1 -p 5432 -U app      # in another terminal
```

`k2c forward <address> <local port>` listens on `127.0.0.1:<local port>` and
carries each connection there to the port. The address is the one the console
shows for the port (Deployments, the deployment, **Endpoint**, **Other ports**).
The port's access must be **API key**. Ports with **App login** take any TLS
client directly, without this command.

## Install

Download the archive for your system from the
[latest release](https://github.com/Kernel2Cloud/k2c-cli/releases/latest):

| System | Archive |
|---|---|
| Linux, x86-64 | [k2c_linux_amd64.tar.gz](https://github.com/Kernel2Cloud/k2c-cli/releases/latest/download/k2c_linux_amd64.tar.gz) |
| Linux, ARM | [k2c_linux_arm64.tar.gz](https://github.com/Kernel2Cloud/k2c-cli/releases/latest/download/k2c_linux_arm64.tar.gz) |
| macOS, Apple silicon | [k2c_darwin_arm64.tar.gz](https://github.com/Kernel2Cloud/k2c-cli/releases/latest/download/k2c_darwin_arm64.tar.gz) |
| macOS, Intel | [k2c_darwin_amd64.tar.gz](https://github.com/Kernel2Cloud/k2c-cli/releases/latest/download/k2c_darwin_amd64.tar.gz) |
| Windows, x86-64 | [k2c_windows_amd64.zip](https://github.com/Kernel2Cloud/k2c-cli/releases/latest/download/k2c_windows_amd64.zip) |
| Windows, ARM | [k2c_windows_arm64.zip](https://github.com/Kernel2Cloud/k2c-cli/releases/latest/download/k2c_windows_arm64.zip) |

On Linux or macOS:

```sh
curl -fsSLO https://github.com/Kernel2Cloud/k2c-cli/releases/latest/download/k2c_linux_amd64.tar.gz
tar -xzf k2c_linux_amd64.tar.gz
sudo install k2c_linux_amd64/k2c /usr/local/bin/k2c
k2c version
```

The binaries aren't signed for macOS or Windows yet. On macOS, if it is blocked
as from an unidentified developer, run `xattr -d com.apple.quarantine k2c` once.
On Windows, SmartScreen may ask you to confirm the first time.

Or build it with Go 1.27 or later: `go install github.com/Kernel2Cloud/k2c-cli/cmd/k2c@latest`.

### Check what you downloaded

Each release has `SHA256SUMS`:

```sh
curl -fsSLO https://github.com/Kernel2Cloud/k2c-cli/releases/latest/download/SHA256SUMS
sha256sum --ignore-missing -c SHA256SUMS
```

Each archive also has a signed build provenance attestation: it was built by
this repository's release workflow from the tagged source. With the GitHub CLI:

```sh
gh attestation verify k2c_linux_amd64.tar.gz --repo Kernel2Cloud/k2c-cli
```

## How it works

Each connection is TLS to the port's address (the certificate is verified).
`k2c` asks for the protocol `k2c-forward/1` and sends `KEY <api key>`. Kernel2Cloud
answers `OK` once a ready copy of the deployment is connected, or
`ERR <reason>`, for example `invalid API key`. After `OK` the connection carries
your app's own protocol both ways. Before it starts listening, `k2c forward`
checks that the address is a TCP port that takes an API key, without sending
the key.

A connection stays on one copy for its life. If that copy's site goes offline,
the connection ends; reconnect and it goes to a copy still running.

## Flags

`k2c forward`:

| Flag | Default | |
|---|---|---|
| `-bind` | `127.0.0.1` | Local address to listen on. `0.0.0.0` shares it on your network: anyone who can reach it uses your key. |
| `-connect` | | Connect to this host:port instead of the address (testing). |
| `-insecure` | off | Don't verify the certificate (testing only). |

## Development

```sh
make test                      # vet and tests
make build                     # ./k2c for this machine
make dist VERSION=v0.1.0       # every platform into dist/, with SHA256SUMS
```

A release is made by pushing a tag: `git tag v0.1.0 && git push origin v0.1.0`.
The release workflow tests, builds every platform with `scripts/dist.sh`, and
publishes the archives, `SHA256SUMS` and the attestation.
