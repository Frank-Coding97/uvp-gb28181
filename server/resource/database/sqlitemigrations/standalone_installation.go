package sqlitemigrations

import _ "embed"

const StandaloneInstallationVersion = "2026-09-08-standalone-installation-sqlite.sql"
const StandaloneInstallationSHA256 = "435dea7fcc642461ccd571c0b28663fb7fa26ed478877c8c0887ce358606101b"

//go:embed 2026-09-08-standalone-installation-sqlite.sql
var StandaloneInstallationSQL string
