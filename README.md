# hiok — command line for HIOK Cloud

A single binary that drives the same API the console uses. Every command prints a
table for people and accepts `--json` for scripts.

## Install

Download the archive for your platform from the
[latest release](https://github.com/HIOK-Official/hiok-cli/releases/latest)
(`hiok_<version>_<os>_<arch>`, Linux/macOS/Windows, amd64/arm64), or with Go 1.23+:

```bash
go install github.com/HIOK-Official/hiok-cli/cmd/hiok@latest
```

In CI/CD, use the [GitHub Action](https://github.com/HIOK-Official/hiok-action) or the
Azure DevOps extension (HIOK Cloud on the Visual Studio Marketplace); both install it for you.

## Sign in from a pipeline

```bash
export HIOK_CLIENT_ID=...       # a service principal: console → Identity → Service principals
export HIOK_CLIENT_SECRET=...
hiok vm list                    # signs in on first use, renews the one-hour token
```

## Every API operation

```bash
hiok api list --search keyvault
hiok api call StorageAccount.GetStorageAccounts
hiok api call ContainerJobs.Run --param id=<job-id>
hiok api call KeyVault.Create --body @vault.json
```

## Build

```bash
make build            # → build/hiok
make install          # → ~/.local/bin/hiok
```

## Sign in

```bash
hiok login run --endpoint https://hiokcloud.com --email you@example.com --region canada
```

The token is written to `~/.hiok/config.json` with mode 0600 — it is a bearer token,
so it is as good as the password. `HIOK_ENDPOINT`, `HIOK_TOKEN` and `HIOK_REGION`
override the stored file, and `HIOK_CONFIG` points at a different file entirely, which
is how a CI job keeps its credentials separate from a developer's.

`HIOK_PASSWORD` is read when `--password` is omitted, so the password need not appear
in shell history.

## What it covers

| Topic | Resources |
|---|---|
| `vm`, `vnet` | Virtual machines, virtual networks |
| `app` | Container apps and their environments |
| `keyvault`, `cert` | Vaults, secrets, keys, certificates |
| `mongo`, `yugabyte`, `postgres`, `analytics` | Managed databases |
| `vpn` | Gateways, point-to-site clients, profiles |
| `iot`, `dps` | Hubs, devices, provisioning enrollments |
| `publicip` | Address pools, allocations, reconcile |

Run `hiok` for the list, `hiok <topic>` for its commands.

## Examples

```bash
# A secret, and reading it back for a script to consume
hiok keyvault create --name payments-prod --retention-days 30
hiok keyvault set <vault-id> db-password --value s3cr3t
export DB_PASSWORD=$(hiok keyvault get <vault-id> db-password)

# A certificate signed by your own authority
hiok cert create <vault-id> web-tls --action csr --subject shop.example.com --out web.csr
# ...have web.csr signed...
hiok cert merge <vault-id> web-tls --file web-signed.crt
hiok cert export <vault-id> web-tls --format pfx --password '…' --out web.pfx

# A replica set that reads locally without going stale
hiok mongo create --name sessions --regions canada,germany --consistency bounded --max-staleness 120

# A VPN profile for someone
hiok vpn client-create <gateway-id> alex-laptop
hiok vpn client-config <client-id> --out alex.ovpn
```

Flags may appear before, between or after positional arguments — `hiok keyvault set
<vault> <name> --value x` works, which is how people actually type.

## Things worth knowing

- `keyvault get` prints the bare value so it can be substituted directly. An item that
  is disabled, expired or not yet valid has no readable value and the command fails
  rather than printing nothing.
- `keyvault set` on an existing name creates a **new version**; the previous value stays
  retrievable.
- `keyvault delete` is a soft delete. `keyvault purge` is the irreversible one and asks
  first — unless output is piped, where there is nobody to answer.
- `cert create --action csr` keeps the private key in the vault and hands you a signing
  request. The item is unusable until you merge the signed certificate back.
- A profile or `.pfx` written by this tool is mode 0600: it carries private key material.

## In CI/CD

Pipeline commands, designed to be the whole step:

```bash
hiok registry push my-registry --build . --repository web     # prints the image; tag = commit SHA
hiok app deploy web --image "$IMAGE"                           # new revision, waits until active
hiok docker deploy api --image "$IMAGE"                        # replace a container, keep ports/env
hiok job run db-migrate --wait 30m                             # shows output; exit code follows the job
hiok vm ssh app-01 --script scripts/post-deploy.sh             # platform key; IPv4 relay by default
hiok storage upload my-account site ./dist --prefix release-42/
eval "$(hiok keyvault export prod-secrets --format env)"       # or --format github / azure-devops
hiok kubernetes kubeconfig my-cluster --out kubeconfig
```

Outputs (image, revision, run_id, …) are written to `GITHUB_OUTPUT`, Azure DevOps output variables,
or the dotenv file named by `HIOK_OUTPUT_ENV`. Sign in with `HIOK_CLIENT_ID`/`HIOK_CLIENT_SECRET`
(a service principal) or `HIOK_TOKEN` (an API key).

Ready-made steps: [GitHub Actions](https://github.com/HIOK-Official/hiok-action), the
[Azure DevOps extension](https://marketplace.visualstudio.com/items?itemName=hiok.hiok-cloud), and
[GitLab / Bitbucket / Jenkins / CircleCI templates](ci/) on the `ghcr.io/hiok-official/hiok` image.
