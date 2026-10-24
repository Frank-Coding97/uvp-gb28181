package upgrade

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
)

const UpdaterProtocolVersion = 1

type Action string

const (
	ActionStatus      Action = "status"
	ActionPrepare     Action = "prepare"
	ActionApply       Action = "apply"
	ActionCancel      Action = "cancel"
	ActionRollback    Action = "rollback"
	ActionCheck       Action = "check"
	ActionImport      Action = "import"
	ActionCreateTask  Action = "create_task"
	ActionGetTask     Action = "get_task"
	ActionGetImport   Action = "get_import"
	ActionTaskList    Action = "task_list"
	ActionEvents      Action = "events"
	ActionPrepareTask Action = "prepare_task"
	ActionPolicyGet   Action = "policy_get"
	ActionPolicySet   Action = "policy_set"
)

const maxProtocolEventLimit = 100

// PrepareTaskRequest is retained for internal compatibility. Its path fields
// are deliberately excluded from the wire protocol: a socket client may only
// name an opaque inbox token, never a helper filesystem path.
type PrepareTaskRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	Actor          string `json:"actor"`
	TargetDigest   string `json:"target_digest"`
	InboxToken     string `json:"inbox_token,omitempty"`
	StagingPath    string `json:"-"`
	Source         string `json:"-"`
}

// CreateTaskRequest is the bounded wire form of ControlCreateTaskRequest.
// It intentionally carries only the caller identity, idempotency key, and
// digest selected from a previously checked or imported release.
type CreateTaskRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	Actor          string `json:"actor"`
	TargetDigest   string `json:"target_digest"`
}

// ProtocolRequest is the only request shape accepted by the local updater
// socket. It deliberately contains identifiers and registered deployment
// references, never shell commands, arbitrary paths, units, or compose YAML.
type ProtocolRequest struct {
	Protocol     int                 `json:"protocol"`
	Action       Action              `json:"action"`
	TaskID       string              `json:"task_id,omitempty"`
	ImportID     string              `json:"import_id,omitempty"`
	TargetDigest string              `json:"target_digest,omitempty"`
	Confirmation bool                `json:"confirmation,omitempty"`
	Prepare      *PrepareTaskRequest `json:"prepare,omitempty"`
	Create       *CreateTaskRequest  `json:"create,omitempty"`
	Policy       *AutoUpgradePolicy  `json:"policy,omitempty"`
	Actor        string              `json:"actor,omitempty"`
	EventLimit   int                 `json:"event_limit,omitempty"`
	InboxToken   string              `json:"inbox_token,omitempty"`
}

type ProtocolResponse struct {
	Protocol int                 `json:"protocol"`
	OK       bool                `json:"ok"`
	Error    string              `json:"error,omitempty"`
	Task     *TaskRecord         `json:"task,omitempty"`
	Tasks    []TaskRecord        `json:"tasks,omitempty"`
	Events   []UpgradeEvent      `json:"events,omitempty"`
	Policy   *AutoUpgradePolicy  `json:"policy,omitempty"`
	Status   *ControlStatus      `json:"status,omitempty"`
	Check    *ControlCheckResult `json:"check,omitempty"`
	Staged   *StagedArtifact     `json:"staged,omitempty"`
	Import   *ImportRecord       `json:"import,omitempty"`
}

func (r ProtocolRequest) Validate() error {
	if r.Protocol != UpdaterProtocolVersion {
		return fmt.Errorf("unsupported updater protocol %d", r.Protocol)
	}
	switch r.Action {
	case ActionStatus, ActionCheck, ActionTaskList, ActionPolicyGet:
		if !r.hasOnlyZeroOptionalFields() {
			return errors.New("read request contains unexpected fields")
		}
	case ActionPrepare:
		if !validTaskID(r.TaskID) || r.ImportID != "" || !validDigest(r.TargetDigest) || r.Confirmation || r.Prepare != nil || r.Create != nil || r.Policy != nil || r.Actor != "" || r.EventLimit != 0 || r.InboxToken != "" {
			return errors.New("prepare request is incomplete")
		}
	case ActionApply:
		// target_digest was added as an optional field in protocol v1 so older
		// clients can still confirm a persisted task by ID. When supplied it
		// must be a complete SHA-256 digest; the daemon compares it with the
		// durable task before crossing the stopping boundary.
		if !validTaskID(r.TaskID) || r.ImportID != "" || !r.Confirmation || (r.TargetDigest != "" && !validDigest(r.TargetDigest)) || r.Prepare != nil || r.Create != nil || r.Policy != nil || r.Actor != "" || r.EventLimit != 0 || r.InboxToken != "" {
			return errors.New("apply request requires confirmation, task ID and a valid optional target digest")
		}
	case ActionCancel, ActionRollback:
		if !validTaskID(r.TaskID) || r.ImportID != "" || r.TargetDigest != "" || r.Confirmation || r.Prepare != nil || r.Create != nil || r.Policy != nil || r.Actor != "" || r.EventLimit != 0 || r.InboxToken != "" {
			return errors.New("task action contains invalid fields")
		}
	case ActionEvents:
		if r.TaskID != "" || r.ImportID != "" || r.TargetDigest != "" || r.Confirmation || r.Prepare != nil || r.Create != nil || r.Policy != nil || r.Actor != "" || r.EventLimit < 0 || r.EventLimit > maxProtocolEventLimit || r.InboxToken != "" {
			return errors.New("events request contains invalid fields")
		}
	case ActionImport:
		if r.TaskID != "" || r.ImportID != "" || r.TargetDigest != "" || r.Confirmation || r.Prepare != nil || r.Create != nil || r.Policy != nil || r.Actor != "" || r.EventLimit != 0 || !validInboxToken(r.InboxToken) {
			return errors.New("import request is incomplete")
		}
	case ActionGetImport:
		if !validImportID(r.ImportID) || r.TaskID != "" || r.TargetDigest != "" || r.Confirmation || r.Prepare != nil || r.Create != nil || r.Policy != nil || r.Actor != "" || r.EventLimit != 0 || r.InboxToken != "" {
			return errors.New("get_import request is incomplete")
		}
	case ActionCreateTask:
		if r.TaskID != "" || r.ImportID != "" || r.TargetDigest != "" || r.Confirmation || r.Prepare != nil || r.Policy != nil || r.Actor != "" || r.EventLimit != 0 || r.InboxToken != "" || r.Create == nil || r.Create.validate() != nil {
			return errors.New("create_task request is incomplete")
		}
	case ActionGetTask:
		if !validTaskID(r.TaskID) || r.ImportID != "" || r.TargetDigest != "" || r.Confirmation || r.Prepare != nil || r.Create != nil || r.Policy != nil || r.Actor != "" || r.EventLimit != 0 || r.InboxToken != "" {
			return errors.New("get_task request is incomplete")
		}
	case ActionPrepareTask:
		if r.TaskID != "" || r.ImportID != "" || r.TargetDigest != "" || r.Confirmation || r.Create != nil || r.Policy != nil || r.Actor != "" || r.EventLimit != 0 || r.InboxToken != "" {
			return errors.New("prepare_task request is incomplete")
		}
		if err := r.Prepare.validateWire(); err != nil {
			return errors.New("prepare_task request is incomplete")
		}
	case ActionPolicySet:
		if r.TaskID != "" || r.ImportID != "" || r.TargetDigest != "" || r.Confirmation || r.Prepare != nil || r.Create != nil || r.EventLimit != 0 || r.Policy == nil || !validProtocolText(r.Actor, 128) || r.InboxToken != "" {
			return errors.New("policy_set request is incomplete")
		}
	default:
		return fmt.Errorf("unsupported updater action %q", r.Action)
	}
	return nil
}

func (r ProtocolRequest) hasOnlyZeroOptionalFields() bool {
	return r.TaskID == "" && r.ImportID == "" && r.TargetDigest == "" && !r.Confirmation && r.Prepare == nil && r.Create == nil && r.Policy == nil && r.Actor == "" && r.EventLimit == 0 && r.InboxToken == ""
}

func (r *PrepareTaskRequest) validate() error {
	if r == nil || !validProtocolText(r.IdempotencyKey, 256) || !validProtocolText(r.Actor, 128) || !validDigest(r.TargetDigest) || !filepath.IsAbs(r.StagingPath) || (r.Source != "online" && r.Source != "offline") {
		return errors.New("prepare task fields are invalid")
	}
	return nil
}

func (r *PrepareTaskRequest) validateWire() error {
	if r == nil || !validProtocolText(r.IdempotencyKey, 256) || !validProtocolText(r.Actor, 128) || !validDigest(r.TargetDigest) || !validInboxToken(r.InboxToken) {
		return errors.New("prepare task fields are invalid")
	}
	return nil
}

func (r *CreateTaskRequest) validate() error {
	if r == nil || !validProtocolText(r.IdempotencyKey, 256) || !validProtocolText(r.Actor, 128) || !validDigest(r.TargetDigest) {
		return errors.New("create task fields are invalid")
	}
	return nil
}

func validProtocolText(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}

func DecodeProtocolRequest(data []byte) (ProtocolRequest, error) {
	var request ProtocolRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return ProtocolRequest{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return ProtocolRequest{}, errors.New("multiple protocol requests")
		}
		return ProtocolRequest{}, err
	}
	if err := request.Validate(); err != nil {
		return ProtocolRequest{}, err
	}
	return request, nil
}

func validTaskID(value string) bool {
	return len(value) == 32 && filepath.Base(value) == value
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}
