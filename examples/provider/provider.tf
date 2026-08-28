provider "matrix" {
  homeserver_url = "https://matrix.example.org"

  # Managing users, or deleting rooms, needs a server administrator's token.
  # Prefer the MATRIX_ACCESS_TOKEN environment variable over committing it.
  access_token = var.matrix_access_token
}
