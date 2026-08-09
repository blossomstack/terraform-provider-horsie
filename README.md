# terraform-provider-horsie

Manage a [horsie](https://github.com/blossomstack/horsie) server's configuration as code.

horsie's configuration is the sort of thing that benefits from review: which models exist, what an agent preset is allowed to do, which routines run unattended. This provider puts that in a repository instead of in a settings page.

## Usage

```hcl
terraform {
  required_providers {
    horsie = {
      source = "blossomstack/horsie"
    }
  }
}

provider "horsie" {
  endpoint = "https://horsie.example.com" # or HORSIE_ENDPOINT
  token    = var.horsie_token             # or HORSIE_TOKEN
}

resource "horsie_model_provider" "anthropic" {
  name    = "anthropic"
  kind    = "anthropic"
  api_key = var.anthropic_api_key

  keep_thinking_signature = true
}

resource "horsie_model" "sonnet" {
  alias          = "sonnet"
  model_provider = horsie_model_provider.anthropic.name
  model_id       = "claude-sonnet-4-6"
}
```

## Authentication

The provider authenticates with a horsie **agent token** — a long-lived, revocable credential meant for unattended callers:

```bash
curl -X POST https://horsie.example.com/api/device/tokens \
  -H 'content-type: application/json' \
  -d '{"label": "terraform"}'
```

The token is shown once and stored hashed, so it cannot be read back. Label it for the machine that will hold it: a wall of unlabelled secrets is unrevokable in practice.

Everything the provider manages is scoped to the account that owns the token. It cannot see or change another account's configuration.

## Resources

| Resource | Manages |
|---|---|
| `horsie_model_provider` | An LLM provider — endpoint, kind and credential |
| `horsie_model` | A model alias sessions and presets select by name |
| `horsie_agent` | An agent preset: model, skills, MCP servers, memory spaces |
| `horsie_memory_space` | A namespace for an agent's long-term memories |
| `horsie_routine` | An agent preset plus a fixed prompt and a schedule |
| `horsie_environment` | A reusable runtime + repos bundle (experimental) |
| `horsie_plugin` | A bundle of skills, commands, agents and hooks, installed from a git repo |
| `horsie_mcp_server` | A remote MCP server: endpoint and how horsie authenticates to it |
| `horsie_workflow` | A graph of agent-preset steps, wired by conditions over their output |

Not covered, deliberately: plugin marketplaces (a bundle installs from a git URL without one), and the parts of horsie that are per-account state rather than configuration.

### Things worth knowing

**`model_provider`, not `provider`.** horsie already uses "vendor" for the *execution runtime* that runs a session, so a bare "provider" beside it would be ambiguous — and `provider` is a reserved word in HCL anyway.

**`api_key` is write-only.** The server never returns a key, only whether one is stored (`has_credential`). Omitting `api_key` leaves a stored key untouched; setting it to `""` clears it. A `chatgpt`-kind provider authenticates with an OAuth sign-in performed out of band and holds no key at all, so Terraform cannot manage its credential.

Deleting a provider that a model still routes to is refused by the server. Reference the provider's `name` from your `horsie_model` resources and Terraform will order the destroy correctly.

**A preset says nothing about where the work happens.** No repos, no runtime. Which machine runs it and what it runs against are properties of the *invocation* — a pinned runtime is invisible once it disconnects but fatal at invoke, and a hardcoded checkout can only ever be run one way. `horsie_routine` therefore carries a required `environment` block, and `horsie_environment` exists for the named case. `horsie_workflow` is the same idea: a definition is only the graph, and where a run happens comes from the invocation.

**In a workflow, order is behaviour.** A step's `transition` blocks are tried in the order written and the first match wins, so put the catch-all last. A step with no transitions ends the run. Conditions read the producing step's structured output, so any step with a conditional transition needs an `output_schema` — write it with `jsonencode`; the provider keeps your formatting in state and only reports drift when the document actually differs.

**A plugin is a git repo, pinned.** horsie clones a bundle once, at install, and serves its own copy from then on — so `source_ref` should be a commit sha. Point it at a branch and Terraform sees no change however far the remote moves; the `version` attribute records the sha that actually landed. Both source attributes replace the resource, because horsie has no re-pin operation. The bundle's `name` is assigned by the server, from the repo's `plugin.json` or its basename, so it is an output rather than an input.

**An MCP server's `oauth` kind is half-manageable.** `none`, `bearer` and `github_app` are fully declarative. `oauth` is not: Terraform writes the client configuration, but the sign-in itself is a browser redirect, so the server applies with `connected = false` until someone authorises it from horsie's settings page. That is the same split as a `chatgpt` model provider, and the resource says so in `connected` rather than pretending the apply finished the job.

## Development

```bash
make            # fmt-check, vet, test, build, docs-check
make generate   # regenerate the wire types from horsie's schemas
make docs       # regenerate docs/ from the schemas + examples/
```

`docs/` is what the Terraform Registry serves, so CI fails if it is stale: a schema change with no regenerate ships documentation describing the old provider.

Releases are cut by tagging `vX.Y.Z`. GoReleaser builds the per-platform zips, a `SHA256SUMS` file and a detached GPG signature — the three shapes the Registry ingests by name.

The types in `internal/horsieapi` are generated by [fluorite](https://github.com/blossomstack/fluorite) from horsie's `.fl` schemas — the same schemas the server and web UI are built from, so a field rename cannot silently diverge here. They are committed, and `make generate` needs a horsie checkout:

```bash
make generate HORSIE_FLUORITE=/path/to/horsie/crates/models/fluorite
```

Generated Go is expected to be byte-identical to `gofmt` output; `make generate` fails if it is not.

## Registries

`terraform` and `tofu` resolve the same source address through **different** registries, and publishing to one does not cover the other:

| CLI | Resolves via |
|---|---|
| `terraform` | `registry.terraform.io` |
| `tofu` | `registry.opentofu.org` |

Writing `source = "registry.terraform.io/blossomstack/horsie"` does not help under OpenTofu — it rewrites the hostname to its own registry. See [RELEASING.md](RELEASING.md) for the setup and release process, including the signing-key constraints (RSA/DSA only; never remove a published key).

## Licence

Apache-2.0 OR MIT, matching horsie.
