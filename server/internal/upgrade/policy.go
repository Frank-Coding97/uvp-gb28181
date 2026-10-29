package upgrade

// AutoUpgradePolicy is the on-disk shape of a previously persisted automatic
// upgrade policy. The automatic upgrade feature itself has been removed; this
// type only survives so that historical task indexes can still be decoded.
//
// TaskStore.load decodes tasks.json with DisallowUnknownFields, so removing the
// type (or any of its fields, they are referenced by record.PolicySnapshot and
// record.PolicyVersion) would make every existing installation fail to read its
// own upgrade history. Do not add behaviour here: no automatic scheduling, no
// maintenance windows, no policy validation. Manual upgrades carry their own
// approval flow in WorkflowExecutor.
type AutoUpgradePolicy struct {
	Enabled            bool   `json:"enabled"`
	PolicyVersion      uint64 `json:"policy_version"`
	Timezone           string `json:"timezone"`
	WindowStart        string `json:"window_start"`
	WindowEnd          string `json:"window_end"`
	Channel            string `json:"channel"`
	Deployment         string `json:"deployment,omitempty"`
	OS                 string `json:"os,omitempty"`
	Arch               string `json:"arch,omitempty"`
	PatchOnly          bool   `json:"patch_only"`
	AllowRestart       bool   `json:"allow_restart"`
	AutoRollback       bool   `json:"auto_rollback"`
	NotificationTarget string `json:"notification_target,omitempty"`
	RetainVersions     int    `json:"retain_versions"`
	CreatedBy          string `json:"created_by,omitempty"`
}
