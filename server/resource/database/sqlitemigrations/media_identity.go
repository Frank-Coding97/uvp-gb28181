package sqlitemigrations

import _ "embed"

const MediaIdentityVersion = "2026-09-07-zlm-media-identity-sqlite.sql"
const MediaIdentitySHA256 = "b3efe3bed7e8653c53c28f4e39c8587a101a5417d9372d5ebcd36a98f8bcfb2b"

//go:embed 2026-09-07-zlm-media-identity-sqlite.sql
var MediaIdentitySQL string
