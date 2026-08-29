# Terraform Provider for Matrix

Manages rooms and users on a [Matrix](https://matrix.org) homeserver as code.

The provider speaks two APIs, and works against any homeserver serving both —
[Synapse](https://github.com/element-hq/synapse) and
[tuwunel](https://github.com/matrix-construct/tuwunel) are the tested targets:

- the **Client-Server API** for rooms and room state
- the **Synapse admin API** for user management and room deletion

Nothing in the provider is specific to one homeserver implementation.

> **Status:** early development. `matrix_room`, `matrix_user`, `matrix_media` and
> `matrix_room_member` are the resources so far, and the schema may still change.

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
the admin API: all of `matrix_user`, and destroying a `matrix_room`. Room
creation and state edits need only an ordinary account.

## Users and passwords

`matrix_user` takes the password as a
[write-only argument](https://developer.hashicorp.com/terraform/language/resources/ephemeral/write-only),
so it is sent to the homeserver and never written to state or to a plan file:

```terraform
variable "alice_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "matrix_user" "alice" {
  localpart    = "alice"
  display_name = "Alice"

  password_wo         = var.alice_password
  password_wo_version = "1"
}
```

Because nothing stores the password, nothing can detect that it changed —
`password_wo_version` is what tells the provider to send it again.

## Avatars

Matrix profile pictures are content-repository references, not ordinary URLs, so
a picture has to be uploaded before it can be set. `matrix_media` does the upload
and exports the `mxc://` URI:

```terraform
resource "matrix_media" "alice_avatar" {
  source = "${path.module}/avatars/alice.png"
}

resource "matrix_user" "alice" {
  localpart  = "alice"
  avatar_url = matrix_media.alice_avatar.mxc_uri
}
```

The file is hashed during planning, so editing `avatars/alice.png` in place
uploads the new picture and repoints the avatar even though nothing in the
configuration changed.

Two things about users have no workaround, and are worth knowing before you
manage any: Matrix cannot **delete** an account, so a destroy deactivates it and
the user ID stays claimed for good; and it cannot **rename** one, so changing
`localpart` deactivates the old account and creates a separate new one.

## Membership

`matrix_room_member` puts an account in a room:

```terraform
resource "matrix_room_member" "alice_ops" {
  room_id = matrix_room.ops.id
  user_id = matrix_user.alice.id
}
```

That defaults to `membership = "join"`, which is the one operation the
Client-Server API cannot do. The Matrix auth rules require a `join` membership
event to be sent by the joining user themselves, so no amount of administrative
power lets one account accept an invitation on another's behalf — the provider
uses the admin API's join endpoint instead, and that only works for **local**
users. Inviting a remote user is fine; forcing one to join is rejected during
planning.

`membership = "invite"` is a floor rather than an exact value. Terraform can
withdraw an invitation but has nothing that un-accepts one, so an invitee who
joins is not treated as drift and does not get planned back out of the room on
the next apply. `current_membership` reports what the homeserver actually holds.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0, or [OpenTofu](https://opentofu.org) — >= 1.11 for `password_wo`
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

Acceptance tests create real rooms and accounts on a real homeserver, and the
accounts they create cannot be deleted afterwards, so point them at a throwaway
instance rather than anything you care about:

```shell
make testacc
```

## License

[MPL-2.0](LICENSE)
