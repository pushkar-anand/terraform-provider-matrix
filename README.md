# Terraform Provider for Matrix

Manages rooms and users on a [Matrix](https://matrix.org) homeserver as code.

The provider speaks two APIs, and works against any homeserver serving both —
[Synapse](https://github.com/element-hq/synapse) and
[tuwunel](https://github.com/matrix-construct/tuwunel) are the tested targets:

- the **Client-Server API** for rooms and room state
- the **Synapse admin API** for user management and room deletion

Nothing in the provider is specific to one homeserver implementation.

> **Status:** early development. `matrix_room` is the only resource so far, and
> the schema may still change before `v0.1.0`.

## Why the admin API

Matrix has no way to delete a room. The Client-Server API can only make a user
*leave* one, so a provider built on the spec API alone would drop a room from
Terraform state while leaving it live and joinable on the server. Room deletion
therefore goes through the Synapse admin API, and a homeserver that does not
serve it gets an explicit error rather than a silent no-op.

The same applies to users: user management is not part of the Matrix
specification at all, and the Synapse admin API is the closest thing to a
portable interface for it.

## Usage

```terraform
terraform {
  required_providers {
    matrix = {
      source  = "pushkar-anand/matrix"
      version = "~> 0.1"
    }
  }
}

provider "matrix" {
  homeserver_url = "https://matrix.example.org"
  access_token   = var.matrix_access_token
}

resource "matrix_room" "ops" {
  name       = "Ops"
  topic      = "Alerts and incident chatter"
  preset     = "private_chat"
  visibility = "private"
}
```

Both provider settings can come from the environment instead, which is the
better place for the token:

| Variable | Attribute |
|---|---|
| `MATRIX_HOMESERVER_URL` | `homeserver_url` |
| `MATRIX_ACCESS_TOKEN` | `access_token` |

The token must belong to a **server administrator** for anything that touches
the admin API — room deletion today, users later. Room creation and state edits
need only an ordinary account.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0, or [OpenTofu](https://opentofu.org)
- [Go](https://go.dev/doc/install) >= 1.27 (to build from source)

## Developing

Build and install into `$GOBIN`:

```shell
make install
```

Regenerate documentation after any schema change — `docs/` is generated from the
schema and the files under `examples/`, never edited by hand:

```shell
make generate
```

Run unit tests:

```shell
make test
```

Acceptance tests create real rooms on a real homeserver, so point them at a
throwaway instance rather than anything you care about:

```shell
make testacc
```

## License

[MPL-2.0](LICENSE)
