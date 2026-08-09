# Releasing

Cutting a release is one command. Everything else here is one-time setup, or the
reasoning behind a decision that looks arbitrary until it bites.

```bash
git tag v0.1.1 && git push origin v0.1.1
```

That triggers `.github/workflows/release.yml`, which builds with GoReleaser,
signs the checksums, and publishes a GitHub release. Both registries ingest that
release automatically once the one-time setup below is done.

## Two registries, and why you need both

`terraform` and `tofu` resolve the *same* source address through *different*
registries:

| CLI | Resolves `blossomstack/horsie` via |
|---|---|
| `terraform` | `registry.terraform.io` |
| `tofu` | `registry.opentofu.org` |

Publishing to one does **not** cover the other, and there is no HCL escape
hatch: writing `source = "registry.terraform.io/blossomstack/horsie"` does not
work under OpenTofu, which rewrites the hostname to its own registry and reports
the provider missing. Both registries have to be set up separately.

The good news is that they consume the **same GitHub release and the same GPG
key**, so a release only ever has to be cut once.

## The signing key

Both registries verify a detached GPG signature over the `SHA256SUMS` file.

**Use RSA (4096) or DSA. Nothing else.** The Terraform Registry API rejects ECC
keys, which is what modern GnuPG generates *by default* — so `gpg
--full-generate-key` with the defaults produces a key that cannot publish here.
This is a registry constraint, not a security recommendation: Ed25519 would be
the better modern choice, and it is unusable.

**Never remove a public key from a registry.** Every already-published version
was signed with the key that was current at the time, and consumers verify
against it. Removing it breaks `init` for anyone pinned to an older version. The
Terraform Registry deliberately provides no way to remove one.

**Losing the private key is recoverable.** Generate a new one, upload the new
public key, sign future releases with it, and leave the old key registered. The
only cost is that no release can be cut until the new key is in place. Key
*compromise* is a different matter and has no self-service fix — contact the
registries.

Worth doing at key-creation time, because neither is retrofittable:

- Generate a revocation certificate and store it separately from the key.
- Sign with a subkey, keeping the primary offline, so a CI compromise costs a
  subkey rotation rather than the identity.

### CI secrets

The release workflow expects two repository secrets:

| Secret | Value |
|---|---|
| `GPG_PRIVATE_KEY` | ASCII-armored private key (`gpg --armor --export-secret-keys <ID>`) |
| `PASSPHRASE` | The key's passphrase, **with no trailing newline** |

A trailing newline in `PASSPHRASE` fails key import in CI with an unhelpful
error. Set it in a way that strips one:

```bash
printf '%s' "$(cat passphrase.txt)" | gh secret set PASSPHRASE
```

## One-time setup: Terraform Registry

1. **Upload the public key** at <https://registry.terraform.io/settings/gpg-keys>.
   Add it under the **organization** namespace (`blossomstack`), not your
   personal one — you must be an org admin. Signing in may require authorizing
   the org, or it will not appear in the selector.
2. **Publish the provider** at <https://registry.terraform.io/publish/provider>:
   select the org, select this repository, follow the prompts.

The key must be uploaded *before* publishing — the registry validates the
signature during ingest. Publishing installs a webhook, so later tags are picked
up with no further action.

Requirements this repo already satisfies: public repository, lowercase name
matching `terraform-provider-{NAME}`, `terraform-registry-manifest.json` at the
root, and generated `docs/`.

## One-time setup: OpenTofu Registry

Two submissions, both through GitHub issue forms at
[opentofu/registry](https://github.com/opentofu/registry/issues/new/choose).

**Your organization membership must be public** or the automated validation
rejects the submission:

```bash
gh api -X PUT orgs/blossomstack/public_members/<your-username>
```

Then, **key first** — a provider submission validates against a registered key,
so submitting in the other order fails:

1. **Submit new Provider Signing Key** — namespace `blossomstack`, leave the
   provider name *empty* so the key registers at namespace level and covers
   every future provider, tick the public-membership box, paste the armored
   public key.
2. **Submit new Provider** — repository `blossomstack/terraform-provider-horsie`.

Each submission is validated, then merged as a PR by a maintainer, so expect
hours rather than minutes.

These forms **must** be filled in through the browser. The automation reads the
issue form's structured fields, so an equivalent issue created with `gh` or the
API will not be processed.

## Verifying a release

Check the assets before trusting anything downstream. The registries match them
**by exact name**, and a mismatch fails at ingest rather than at build:

```bash
gh release view v0.1.0 --json assets --jq '.assets[].name'
```

Expect `terraform-provider-horsie_<version>_<os>_<arch>.zip` for each platform,
plus `_SHA256SUMS`, `_SHA256SUMS.sig` and `_manifest.json`. Note the version in
asset names has **no leading `v`** — GoReleaser strips it, which is what the
registries expect. `project_name` is pinned in `.goreleaser.yml` so a module
rename cannot silently change these.

Then verify each registry actually serves it:

```bash
curl -s https://registry.terraform.io/v1/providers/blossomstack/horsie/versions
curl -s https://registry.opentofu.org/v1/providers/blossomstack/horsie/versions
```

The real test is a clean install, which exercises signature verification too:

```bash
mkdir /tmp/verify && cd /tmp/verify
cat > main.tf <<'EOF'
terraform {
  required_providers {
    horsie = {
      source  = "blossomstack/horsie"
      version = "0.1.0"
    }
  }
}
EOF
terraform init    # and again with: tofu init
```

## Before tagging

- `make` passes — that includes `docs-check`, which fails if `docs/` is stale.
  The registries serve whatever is committed there, so a schema change without a
  regenerate ships documentation describing the old provider.
- `make generate` has been run if horsie's `.fl` schemas moved. Stale generated
  types are the failure mode that unit tests cannot catch: they run against a
  fake built from those same types, so the fake agrees with every mistake.
- A published version is **immutable**. Fixing a mistake means shipping the next
  patch version, not replacing the released one.
