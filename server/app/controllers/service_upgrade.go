package controllers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/global/app"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

type ServiceUpgradeController struct {
	Common
	store           *upgrade.TaskStore
	storeMu         sync.Mutex
	releaseSource   string
	versionPath     string
	check           func(context.Context) (upgrade.Manifest, error)
	preflight       func(context.Context) (upgrade.PreflightSnapshot, error)
	runtimeIdentity func() (string, string)
	sequencePath    string
	sequenceMu      sync.Mutex
	policyStore     *upgrade.PolicyStore
	schedulerOnce   sync.Once
	rpc             serviceUpgradeRPC
	httpClient      *http.Client
	local           bool
}

type serviceUpgradeOptions struct {
	Store           *upgrade.TaskStore
	ReleaseSource   string
	VersionPath     string
	Check           func(context.Context) (upgrade.Manifest, error)
	Preflight       func(context.Context) (upgrade.PreflightSnapshot, error)
	RuntimeIdentity func() (string, string)
	RPC             serviceUpgradeRPC
	HTTPClient      *http.Client
}

func NewServiceUpgradeController() *ServiceUpgradeController {
	return newServiceUpgradeController(serviceUpgradeOptions{})
}

// CollectUpgradePreflight reads the current installation state using the same
// path and database rules as the automatic scheduler. Privileged updater
// helpers use it after the business process has stopped instead of trusting a
// snapshot supplied with the task.
func CollectUpgradePreflight(ctx context.Context) (upgrade.PreflightSnapshot, error) {
	return NewServiceUpgradeController().schedulerPreflight(ctx)
}

func newServiceUpgradeController(options serviceUpgradeOptions) *ServiceUpgradeController {
	versionPath := options.VersionPath
	if versionPath == "" {
		versionPath = strings.TrimSpace(os.Getenv("UVP_VERSION_PATH"))
		if versionPath == "" {
			versionPath = filepath.Join(app.BasePath, "version.json")
		}
	}
	runtimeIdentity := options.RuntimeIdentity
	if runtimeIdentity == nil {
		runtimeIdentity = defaultUpgradeRuntimeIdentity
	}
	stateRoot := upgradeStateRoot(versionPath)
	local := options.Store != nil || options.Check != nil || options.Preflight != nil
	rpc := options.RPC
	if rpc == nil {
		rpc = serviceUpgradeRPCCall(defaultServiceUpgradeRPC)
	}
	var policyStore *upgrade.PolicyStore
	if local {
		// Tests and explicitly embedded callers may still use the old local
		// implementation. The production constructor leaves helper-owned
		// policy state entirely outside the business process.
		policyStore = policyStoreForRoot(stateRoot)
	}
	return &ServiceUpgradeController{
		store: options.Store, releaseSource: options.ReleaseSource, versionPath: versionPath, check: options.Check, preflight: options.Preflight, runtimeIdentity: runtimeIdentity,
		sequencePath: filepath.Join(stateRoot, "sequences.json"),
		policyStore:  policyStore,
		rpc:          rpc,
		httpClient:   options.HTTPClient,
		local:        local,
	}
}

// defaultUpgradeRuntimeIdentity keeps the preflight subprocess independent
// from app.LoadVersionInfo. A regular service has loaded metadata and can bind
// both fields; the short-lived preflight process instead proves the embedded
// build ID against version.json below.
func defaultUpgradeRuntimeIdentity() (string, string) {
	if strings.TrimSpace(app.AppVersion.Version) != "" || strings.TrimSpace(app.AppVersion.BuildID) != "" {
		return app.RuntimeVersionIdentity()
	}
	return "", app.RuntimeBuildID()
}

func upgradeStateRoot(versionPath string) string {
	if value := strings.TrimSpace(os.Getenv("UVP_UPDATER_STATE")); value != "" {
		return value
	}
	if app.BasePath != "" {
		return filepath.Join(filepath.Dir(versionPath), "updater-state")
	}
	return filepath.Join(os.TempDir(), "uvp-gb28181-updater-state")
}

func policyStoreForRoot(root string) *upgrade.PolicyStore {
	path := filepath.Join(root, "policy.json")
	store, err := upgrade.OpenPolicyStore(path)
	if err != nil {
		return nil
	}
	return store
}

func (c *ServiceUpgradeController) ensureStore() (*upgrade.TaskStore, error) {
	c.storeMu.Lock()
	defer c.storeMu.Unlock()
	if c.store != nil {
		return c.store, nil
	}
	root := upgradeStateRoot(c.versionPath)
	store, err := upgrade.OpenTaskStore(root)
	if err != nil {
		return nil, err
	}
	c.store = store
	return store, nil
}

// StartAutoScheduler is retained for embedded test callers. Production
// controllers leave policy polling to the privileged helper's
// HelperScheduler, so the business process never opens helper-owned state.
func (c *ServiceUpgradeController) StartAutoScheduler(ctx context.Context) {
	if c == nil || !c.local {
		return
	}
	if ctx == nil {
		return
	}
	c.schedulerOnce.Do(func() {
		store, err := c.ensureStore()
		if err != nil {
			return
		}
		checker := c.check
		if checker == nil {
			checker = c.configuredCheck()
		}
		if checker == nil || c.policyStore == nil {
			return
		}
		events, eventErr := upgrade.OpenFileEventLog(filepath.Join(upgradeStateRoot(c.versionPath), "events.jsonl"))
		if eventErr != nil {
			return
		}
		scheduler := &upgrade.AutoScheduler{
			Store:          store,
			PolicyStore:    c.policyStore,
			Target:         upgrade.Target{Deployment: c.deployment(), OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: upgrade.UpdaterProtocolVersion},
			Check:          checker,
			CurrentVersion: c.currentVersion,
			Prepare:        c.prepareSchedulerArtifact,
			Preflight:      c.schedulerPreflight,
			Events:         events,
		}
		go func() { _ = scheduler.Run(ctx) }()
	})
}

func (c *ServiceUpgradeController) schedulerPreflight(ctx context.Context) (upgrade.PreflightSnapshot, error) {
	if ctx == nil {
		return upgrade.PreflightSnapshot{}, errors.New("preflight context is required")
	}
	if err := ctx.Err(); err != nil {
		return upgrade.PreflightSnapshot{}, err
	}
	currentVersion, currentBuildID, err := c.currentReleaseIdentity()
	if err != nil {
		return upgrade.PreflightSnapshot{}, err
	}
	snapshot := upgrade.PreflightSnapshot{CurrentVersion: currentVersion, CurrentBuildID: currentBuildID}
	configDigest, err := c.configurationDigest()
	if err != nil {
		return upgrade.PreflightSnapshot{}, err
	}
	snapshot.ConfigDigest = configDigest
	databaseDialect, migrationDigest, err := c.migrationState(ctx)
	if err != nil {
		return upgrade.PreflightSnapshot{}, err
	}
	snapshot.DatabaseDialect = databaseDialect
	snapshot.MigrationDigest = migrationDigest
	return snapshot, nil
}

func (c *ServiceUpgradeController) collectPreflight(ctx context.Context) (upgrade.PreflightSnapshot, error) {
	if c.preflight != nil {
		return c.preflight(ctx)
	}
	return c.schedulerPreflight(ctx)
}

func (c *ServiceUpgradeController) configurationDigest() (string, error) {
	root := c.configurationRoot()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve upgrade configuration: %w", err)
	}
	rootInfo, err := os.Stat(resolvedRoot)
	if err != nil {
		return "", fmt.Errorf("stat upgrade configuration: %w", err)
	}
	if !rootInfo.IsDir() {
		return "", fmt.Errorf("upgrade configuration path is not a directory: %s", root)
	}
	entries := make([]string, 0, 4)
	err = filepath.WalkDir(resolvedRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}
		// os.DirEntry.Type reports a symlink rather than its target. Stat the
		// entry so shared config files and a symlinked config directory are
		// included in the digest.
		info, statErr := os.Stat(path)
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("upgrade configuration entry is not a regular file: %s", path)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(resolvedRoot, path)
		if relErr != nil {
			return relErr
		}
		entries = append(entries, rel+"\x00"+string(data))
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("read upgrade configuration: %w", err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("upgrade configuration has no YAML files: %s", root)
	}
	sort.Strings(entries)
	return sha256Hex([]byte(strings.Join(entries, "\x00"))), nil
}

func (c *ServiceUpgradeController) configurationRoot() string {
	versionDir := filepath.Clean(filepath.Dir(c.versionPath))
	if app.BasePath != "" {
		basePath := filepath.Clean(app.BasePath)
		rel, err := filepath.Rel(basePath, versionDir)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			// The service runs from the fixed installation root while the
			// version file may live below current/releases/<id>. Config is a
			// shared directory at that fixed root and must survive release
			// switching.
			return filepath.Join(basePath, "config")
		}
	}
	return filepath.Join(versionDir, "config")
}

func (c *ServiceUpgradeController) migrationState(ctx context.Context) (string, string, error) {
	if ctx == nil {
		return "", "", errors.New("migration state context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if app.ConfigYml == nil {
		return "", "", errors.New("database configuration is unavailable")
	}
	dialect := upgrade.NormalizeDatabaseDialect(app.ConfigYml.GetString("gormv2.usedbtype"))
	if dialect == "" {
		return "", "", fmt.Errorf("unsupported database dialect %q", app.ConfigYml.GetString("gormv2.usedbtype"))
	}
	var db *gorm.DB
	switch dialect {
	case "mysql":
		db = app.GormDbMysql
	case "sqlite":
		db = app.GormDbSqlite
	case "sqlserver":
		db = app.GormDbSqlserver
	case "postgresql":
		db = app.GormDbPostgreSql
	}
	if db == nil {
		return "", "", fmt.Errorf("database connection for %s is unavailable", dialect)
	}
	var rows []struct {
		Version string `gorm:"column:version"`
	}
	if err := db.WithContext(ctx).Table("gb_schema_migrations").Order("version").Find(&rows).Error; err != nil {
		return "", "", fmt.Errorf("read migration state: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	parts := make([]string, 0, len(rows)+1)
	parts = append(parts, dialect)
	for _, row := range rows {
		parts = append(parts, row.Version)
	}
	return dialect, sha256Hex([]byte(strings.Join(parts, "\x00"))), nil
}

func (c *ServiceUpgradeController) migrationStateDigest(ctx context.Context) (string, error) {
	_, digest, err := c.migrationState(ctx)
	return digest, err
}

func (c *ServiceUpgradeController) prepareSchedulerArtifact(ctx context.Context, manifest upgrade.Manifest, artifact upgrade.Artifact) (upgrade.StagedArtifact, error) {
	if staged, err := c.findStagedArtifact(artifact.SHA256); err == nil && staged.Path != "" {
		if verified, verifyErr := c.verifyTrustedStagedArtifact(staged, artifact.SHA256); verifyErr == nil && verified.ReleaseID == manifest.ReleaseID && verified.Version == manifest.Version && verified.BuildID == manifest.BuildID && verified.Deployment == artifact.Deployment && verified.OS == artifact.OS && verified.Arch == artifact.Arch && upgrade.CompatibilityEqual(verified.Compatibility, manifest.Compatibility) && bytes.Equal(verified.ManifestEnvelope, manifest.Envelope) {
			return verified, nil
		}
	}
	if err := c.downloadArtifact(ctx, manifest, artifact); err != nil {
		return upgrade.StagedArtifact{}, err
	}
	staged, err := c.findStagedArtifact(artifact.SHA256)
	if err != nil {
		return upgrade.StagedArtifact{}, err
	}
	return c.verifyTrustedStagedArtifact(staged, artifact.SHA256)
}

func (c *ServiceUpgradeController) Status(ctx *gin.Context) {
	if !c.local {
		response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{Protocol: upgrade.UpdaterProtocolVersion, Action: upgrade.ActionStatus})
		if err != nil {
			c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
			return
		}
		if !response.OK {
			c.FailAndAbort(ctx, "读取升级状态失败", upgradeRPCResponseError(response), http.StatusServiceUnavailable)
			return
		}
		if response.Status == nil {
			c.FailAndAbort(ctx, "读取升级状态失败", errors.New("upgrade helper returned no status"), http.StatusServiceUnavailable)
			return
		}
		status := response.Status
		version := c.currentVersionPayload()
		if status.CurrentVersion != "" {
			version["version"] = status.CurrentVersion
		}
		if status.CurrentBuildID != "" {
			version["build_id"] = status.CurrentBuildID
		}
		_, updaterErr := os.Stat(c.updaterSocket())
		c.Success(ctx, gin.H{
			"current":                 version,
			"deployment":              status.Deployment,
			"releaseSourceConfigured": status.ReleaseSourceConfigured,
			"capabilities":            gin.H{"onlineCheck": status.OnlineCheck, "offlineImport": status.OfflineImport, "apply": updaterErr == nil, "rollback": updaterErr == nil, "updaterProtocol": upgrade.UpdaterProtocolVersion},
			"activeTask":              publicUpgradeTask(status.ActiveTask),
			"tasks":                   publicUpgradeTasks(status.Tasks),
			"events":                  status.Events,
		})
		return
	}
	store, err := c.ensureStore()
	if err != nil {
		c.FailAndAbort(ctx, "升级状态不可用", err, http.StatusServiceUnavailable)
		return
	}
	records, err := store.List()
	if err != nil {
		c.FailAndAbort(ctx, "读取升级任务失败", err, http.StatusServiceUnavailable)
		return
	}
	version := c.currentVersionPayload()
	_, updaterErr := os.Stat(c.updaterSocket())
	events := []upgrade.UpgradeEvent(nil)
	if eventLog, eventErr := upgrade.OpenFileEventLog(filepath.Join(upgradeStateRoot(c.versionPath), "events.jsonl")); eventErr == nil {
		events, _ = eventLog.Recent(50)
	}
	c.Success(ctx, gin.H{
		"current":                 version,
		"deployment":              c.deployment(),
		"releaseSourceConfigured": strings.HasPrefix(c.releaseSourceValue(), "https://"),
		"capabilities":            gin.H{"onlineCheck": c.check != nil || c.configuredCheck() != nil, "offlineImport": len(c.trustedKeys()) > 0, "apply": updaterErr == nil, "rollback": updaterErr == nil, "updaterProtocol": 1},
		"activeTask":              publicUpgradeTask(activeTask(records)),
		"tasks":                   publicUpgradeTasks(records),
		"events":                  events,
	})
}

func (c *ServiceUpgradeController) currentVersionPayload() map[string]any {
	version := map[string]any{"name": "uvp-gb28181", "version": "unknown"}
	if data, readErr := os.ReadFile(c.versionPath); readErr == nil {
		var loaded map[string]any
		if json.Unmarshal(data, &loaded) == nil {
			for key, value := range loaded {
				version[key] = value
			}
		}
	}
	return version
}

func (c *ServiceUpgradeController) Policy(ctx *gin.Context) {
	if !c.local {
		response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{Protocol: upgrade.UpdaterProtocolVersion, Action: upgrade.ActionPolicyGet})
		if err != nil {
			c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
			return
		}
		if !response.OK || response.Policy == nil {
			c.FailAndAbort(ctx, "读取无人值守策略失败", upgradeRPCResponseError(response), http.StatusServiceUnavailable)
			return
		}
		c.Success(ctx, response.Policy)
		return
	}
	if c.policyStore == nil {
		c.FailAndAbort(ctx, "无人值守策略不可用", errors.New("policy store is not configured"), http.StatusServiceUnavailable)
		return
	}
	policy, err := c.policyStore.Load()
	if err != nil {
		c.FailAndAbort(ctx, "读取无人值守策略失败", err, http.StatusServiceUnavailable)
		return
	}
	c.Success(ctx, policy)
}

// MigrationPlan is deliberately read-only: it reports the migration ledger
// and embedded target set without running SQL or repairing unknown records.
func (c *ServiceUpgradeController) MigrationPlan(ctx *gin.Context) {
	db := app.DB()
	if db == nil {
		c.FailAndAbort(ctx, "迁移计划不可用", errors.New("database is not configured"), http.StatusServiceUnavailable)
		return
	}
	dialect := strings.ToLower(db.Dialector.Name())
	if dialect == "postgres" {
		dialect = "postgresql"
	}
	if dialect != "mysql" && dialect != "postgresql" && dialect != "sqlserver" && dialect != "sqlite" {
		c.FailAndAbort(ctx, "迁移计划不可用", errors.New("database dialect is unknown"), http.StatusServiceUnavailable)
		return
	}
	available, err := upgrade.AvailableMigrationFiles(dialect)
	if err != nil {
		c.FailAndAbort(ctx, "读取迁移清单失败", err, http.StatusServiceUnavailable)
		return
	}
	var appliedRows []struct {
		Version string `gorm:"column:version"`
	}
	if err := db.Table("gb_schema_migrations").Order("version").Find(&appliedRows).Error; err != nil {
		c.FailAndAbort(ctx, "读取迁移记录失败", err, http.StatusServiceUnavailable)
		return
	}
	applied := make([]string, 0, len(appliedRows))
	for _, row := range appliedRows {
		applied = append(applied, row.Version)
	}
	destructive := app.ConfigYml.GetStringSlice("upgrade.destructive_migrations")
	plan := upgrade.BuildMigrationPlan(dialect, applied, available, destructive)
	c.Success(ctx, plan)
}

func (c *ServiceUpgradeController) UpdatePolicy(ctx *gin.Context) {
	var policy upgrade.AutoUpgradePolicy
	if err := ctx.ShouldBindJSON(&policy); err != nil {
		c.FailAndAbort(ctx, "无人值守策略参数无效", err, http.StatusBadRequest)
		return
	}
	actor := "unknown"
	if claims := c.GetClaims(ctx); claims != nil {
		actor = claims.Username
		if actor == "" {
			actor = "user:" + fmtUint(claims.UserID)
		}
	}
	if !c.local {
		response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{Protocol: upgrade.UpdaterProtocolVersion, Action: upgrade.ActionPolicySet, Policy: &policy, Actor: actor})
		if err != nil {
			c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
			return
		}
		if !response.OK || response.Policy == nil {
			c.FailAndAbort(ctx, "保存无人值守策略失败", upgradeRPCResponseError(response), http.StatusBadRequest)
			return
		}
		c.SuccessWithMessage(ctx, "无人值守策略已更新", response.Policy)
		return
	}
	if c.policyStore == nil {
		c.FailAndAbort(ctx, "无人值守策略不可用", errors.New("policy store is not configured"), http.StatusServiceUnavailable)
		return
	}
	current, err := c.policyStore.Load()
	if err != nil {
		c.FailAndAbort(ctx, "读取当前无人值守策略失败", err, http.StatusServiceUnavailable)
		return
	}
	if policy.Deployment == "" {
		policy.Deployment = c.deployment()
	}
	if policy.OS == "" {
		policy.OS = runtime.GOOS
	}
	if policy.Arch == "" {
		policy.Arch = runtime.GOARCH
	}
	if policy.RetainVersions == 0 {
		policy.RetainVersions = current.RetainVersions
		if policy.RetainVersions == 0 {
			policy.RetainVersions = 2
		}
	}
	policy.CreatedBy = actor
	policy, err = c.policyStore.SaveNext(policy)
	if err != nil {
		c.FailAndAbort(ctx, "保存无人值守策略失败", err, http.StatusBadRequest)
		return
	}
	c.cancelPendingAutomaticTasks(context.WithoutCancel(ctx.Request.Context()), policy.PolicyVersion)
	c.SuccessWithMessage(ctx, "无人值守策略已更新", policy)
}

func (c *ServiceUpgradeController) cancelPendingAutomaticTasks(ctx context.Context, policyVersion uint64) {
	store, err := c.ensureStore()
	if err != nil {
		return
	}
	records, err := store.List()
	if err != nil {
		return
	}
	events, err := upgrade.OpenFileEventLog(filepath.Join(upgradeStateRoot(c.versionPath), "events.jsonl"))
	if err != nil {
		events = nil
	}
	for _, record := range records {
		if record.Actor != "policy" || record.PolicyVersion == policyVersion || !isPendingUpgradePhase(record.Phase) {
			continue
		}
		if err := store.Cancel(record.TaskID); err != nil {
			continue
		}
		if events != nil {
			_ = events.Append(ctx, upgrade.UpgradeEvent{Event: "blocked", TaskID: record.TaskID, PolicyVersion: policyVersion, Reason: "policy changed before maintenance", At: time.Now().UTC()})
		}
	}
}

func isPendingUpgradePhase(phase upgrade.TaskPhase) bool {
	switch phase {
	case upgrade.PhaseFetching, upgrade.PhaseVerifying, upgrade.PhasePreparing, upgrade.PhaseAwaitingConfirmation:
		return true
	default:
		return false
	}
}

// Import receives an offline signed upgrade container. Production requests only
// write a bounded multipart stream to the registered helper inbox. The helper
// then reopens that token, verifies the archive, and writes its own staging
// metadata; no application or service restart occurs in this request.
func (c *ServiceUpgradeController) Import(ctx *gin.Context) {
	if !c.local {
		c.importViaUpgradeHelper(ctx)
		return
	}
	c.importLocal(ctx)
}

func (c *ServiceUpgradeController) importViaUpgradeHelper(ctx *gin.Context) {
	inbox := c.inboxRoot()
	if err := validateUpgradeInboxDirectory(inbox); err != nil {
		c.FailAndAbort(ctx, "升级助手收件目录不可用", err, http.StatusServiceUnavailable)
		return
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maxUpgradeUploadSize+1)
	reader, err := ctx.Request.MultipartReader()
	if err != nil {
		c.FailAndAbort(ctx, "离线升级包格式无效", err)
		return
	}
	var tokenPath string
	found := false
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			removeUpgradeInboxFile(tokenPath)
			c.FailAndAbort(ctx, "读取离线升级包失败", nextErr)
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		if found {
			_ = part.Close()
			removeUpgradeInboxFile(tokenPath)
			c.FailAndAbort(ctx, "离线升级包参数无效", errors.New("multiple upgrade files are not allowed"))
			return
		}
		found = true
		tokenPath, err = writeUpgradeInboxUpload(ctx.Request.Context(), inbox, part)
		closeErr := part.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			removeUpgradeInboxFile(tokenPath)
			c.FailAndAbort(ctx, "写入离线升级包失败", err, http.StatusBadRequest)
			return
		}
	}
	if !found {
		c.FailAndAbort(ctx, "离线升级包参数无效", errors.New("multipart field file is required"))
		return
	}
	token := filepath.Base(tokenPath)
	response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{
		Protocol:   upgrade.UpdaterProtocolVersion,
		Action:     upgrade.ActionImport,
		InboxToken: token,
	})
	if err != nil {
		if imported, missing := c.recoverImportAfterRPCLoss(ctx, token); imported != nil {
			c.respondImportAccepted(ctx, imported)
			return
		} else if missing || isUpgradeRPCConnectFailure(err) {
			removeUpgradeInboxFile(tokenPath)
		}
		c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
		return
	}
	if !response.OK {
		removeUpgradeInboxFile(tokenPath)
		c.FailAndAbort(ctx, "离线升级包校验失败", upgradeRPCResponseError(response), http.StatusBadGateway)
		return
	}
	if response.Import != nil {
		c.respondImportAccepted(ctx, response.Import)
		return
	}
	// A previously installed updater may still return the legacy synchronous
	// staged response while the business application is being rolled forward.
	if response.Staged != nil {
		c.SuccessWithMessage(ctx, "离线升级包已校验并暂存", *response.Staged)
		return
	}
	removeUpgradeInboxFile(tokenPath)
	c.FailAndAbort(ctx, "离线升级包导入失败", errors.New("upgrade helper returned no import record"), http.StatusBadGateway)
}

func (c *ServiceUpgradeController) respondImportAccepted(ctx *gin.Context, imported *upgrade.ImportRecord) {
	app.Response.ReturnJson(ctx, http.StatusAccepted, 0, "离线升级包已接收，正在后台校验", publicUpgradeImport(imported))
}

func (c *ServiceUpgradeController) recoverImportAfterRPCLoss(ctx *gin.Context, token string) (*upgrade.ImportRecord, bool) {
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx.Request.Context()), 12*time.Second)
	defer cancel()
	response, err := c.callUpgradeRPC(probeCtx, upgrade.ProtocolRequest{
		Protocol: upgrade.UpdaterProtocolVersion,
		Action:   upgrade.ActionGetImport,
		ImportID: token,
	})
	if err != nil {
		return nil, false
	}
	if response.OK && response.Import != nil {
		return response.Import, false
	}
	return nil, !response.OK && strings.Contains(strings.ToLower(response.Error), "not found")
}

func isUpgradeRPCConnectFailure(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "connect updater socket:")
}

func (c *ServiceUpgradeController) GetImport(ctx *gin.Context) {
	if c.local {
		c.FailAndAbort(ctx, "读取导入任务失败", errors.New("asynchronous import is available through the upgrade helper"), http.StatusServiceUnavailable)
		return
	}
	response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{
		Protocol: upgrade.UpdaterProtocolVersion,
		Action:   upgrade.ActionGetImport,
		ImportID: importIDFromContext(ctx),
	})
	if err != nil {
		c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
		return
	}
	if !response.OK || response.Import == nil {
		c.FailAndAbort(ctx, "导入任务不存在", upgradeRPCResponseError(response), http.StatusNotFound)
		return
	}
	c.Success(ctx, publicUpgradeImport(response.Import))
}

func importIDFromContext(ctx *gin.Context) string {
	if id := ctx.Param("id"); id != "" {
		return id
	}
	return ctx.Param("upload_id")
}

// importLocal is retained only for tests and explicitly embedded callers that
// inject a local TaskStore. Production constructors always use the helper.
func (c *ServiceUpgradeController) importLocal(ctx *gin.Context) {
	keys := c.trustedKeys()
	if len(keys) == 0 {
		c.FailAndAbort(ctx, "离线升级未配置可信公钥", errors.New("trusted release keys are not configured"), http.StatusServiceUnavailable)
		return
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 8<<30+1<<20)
	reader, err := ctx.Request.MultipartReader()
	if err != nil {
		c.FailAndAbort(ctx, "离线升级包格式无效", err)
		return
	}
	var staged upgrade.StagedArtifact
	found := false
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			c.FailAndAbort(ctx, "读取离线升级包失败", nextErr)
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		if found {
			_ = part.Close()
			c.FailAndAbort(ctx, "离线升级包参数无效", errors.New("multiple upgrade files are not allowed"))
			return
		}
		found = true
		staged, err = upgrade.ImportArchive(ctx.Request.Context(), part, c.stagingRoot(), keys, upgrade.Target{Deployment: c.deployment(), OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: 1})
		_ = part.Close()
		if err != nil {
			c.FailAndAbort(ctx, "离线升级包校验失败", err, http.StatusBadRequest)
			return
		}
	}
	if !found {
		c.FailAndAbort(ctx, "离线升级包参数无效", errors.New("multipart field file is required"))
		return
	}
	metadataPath := staged.Path + ".json"
	if err := upgrade.SaveStagedMetadata(metadataPath, staged); err != nil {
		c.FailAndAbort(ctx, "保存离线升级包记录失败", err, http.StatusServiceUnavailable)
		return
	}
	c.SuccessWithMessage(ctx, "离线升级包已校验并暂存", staged)
}

func (c *ServiceUpgradeController) Check(ctx *gin.Context) {
	if !c.local {
		response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{Protocol: upgrade.UpdaterProtocolVersion, Action: upgrade.ActionCheck})
		if err != nil {
			c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
			return
		}
		if !response.OK || response.Check == nil {
			c.FailAndAbort(ctx, "检查更新失败", upgradeRPCResponseError(response), http.StatusBadGateway)
			return
		}
		checked := response.Check
		c.Success(ctx, gin.H{"manifest": checked.Manifest, "artifact": checked.Artifact, "currentVersion": checked.CurrentVersion, "hasUpdate": checked.HasUpdate})
		return
	}
	checker := c.check
	if checker == nil {
		checker = c.configuredCheck()
	}
	if checker == nil {
		c.FailAndAbort(ctx, "升级检查尚未配置可信发布源", errors.New("release source checker is not configured"), http.StatusServiceUnavailable)
		return
	}
	manifest, err := checker(ctx.Request.Context())
	if err != nil {
		c.FailAndAbort(ctx, "检查更新失败", err, http.StatusBadGateway)
		return
	}
	current := c.currentVersion()
	hasUpdate := current == "" || upgrade.IsVersionAtLeast(manifest.Version, current) && manifest.Version != current
	if current != "" && manifest.Version == current {
		// Same version with a different build is not a normal upgrade target.
		hasUpdate = false
	}
	artifact, artifactErr := manifest.ArtifactFor(upgrade.Target{Deployment: c.deployment(), OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: 1})
	if artifactErr != nil {
		c.FailAndAbort(ctx, "当前部署方式不支持该版本", artifactErr, http.StatusConflict)
		return
	}
	c.Success(ctx, gin.H{"manifest": manifest, "artifact": artifact, "currentVersion": current, "hasUpdate": hasUpdate})
}

type serviceUpgradeTaskRequest struct {
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
	TargetDigest   string `json:"targetDigest" binding:"required"`
}

func (c *ServiceUpgradeController) CreateTask(ctx *gin.Context) {
	var request serviceUpgradeTaskRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		c.FailAndAbort(ctx, "升级任务参数无效", err)
		return
	}
	if len(request.TargetDigest) != 64 {
		c.FailAndAbort(ctx, "升级任务摘要无效", errors.New("target digest must be a 64-character SHA-256 digest"))
		return
	}
	if _, err := hex.DecodeString(request.TargetDigest); err != nil {
		c.FailAndAbort(ctx, "升级任务摘要无效", errors.New("target digest must be hexadecimal"))
		return
	}
	actor := "unknown"
	if claims := c.GetClaims(ctx); claims != nil {
		actor = claims.Username
		if actor == "" {
			actor = "user:" + fmtUint(claims.UserID)
		}
	}
	if !c.local {
		response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{
			Protocol: upgrade.UpdaterProtocolVersion,
			Action:   upgrade.ActionCreateTask,
			Create: &upgrade.CreateTaskRequest{
				IdempotencyKey: request.IdempotencyKey,
				Actor:          actor,
				TargetDigest:   strings.ToLower(request.TargetDigest),
			},
		})
		if err != nil {
			c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
			return
		}
		if !response.OK || response.Task == nil {
			c.FailAndAbort(ctx, "创建升级任务失败", upgradeRPCResponseError(response), http.StatusConflict)
			return
		}
		c.SuccessWithMessage(ctx, "升级任务已受理", publicUpgradeTask(response.Task))
		return
	}
	store, err := c.ensureStore()
	if err != nil {
		c.FailAndAbort(ctx, "升级任务不可用", err, http.StatusServiceUnavailable)
		return
	}
	// A syntactically valid digest is not an upgrade authorization. Before a
	// task is persisted, bind it to either a locally verified staged artifact or
	// the currently trusted signed manifest. The asynchronous download below is
	// still repeated and verified, so a manifest changing after this check can
	// only fail the task; it cannot activate an untrusted file.
	if err := c.validateTrustedTaskTarget(ctx.Request.Context(), request.TargetDigest); err != nil {
		status := http.StatusConflict
		if strings.Contains(err.Error(), "release source") || strings.Contains(err.Error(), "trusted public key") {
			status = http.StatusServiceUnavailable
		}
		c.FailAndAbort(ctx, "升级目标未通过可信校验", err, status)
		return
	}
	record, err := store.Submit(upgrade.TaskRequest{IdempotencyKey: request.IdempotencyKey, Actor: actor, TargetDigest: request.TargetDigest})
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, upgrade.ErrIdempotencyConflict) || errors.Is(err, upgrade.ErrTaskBusy) {
			c.FailAndAbort(ctx, "升级任务冲突", err, status)
			return
		}
		c.FailAndAbort(ctx, "创建升级任务失败", err, http.StatusInternalServerError)
		return
	}
	go c.prepareTask(context.WithoutCancel(ctx.Request.Context()), record)
	c.SuccessWithMessage(ctx, "升级任务已受理", record)
}

func (c *ServiceUpgradeController) validateTrustedTaskTarget(ctx context.Context, digest string) error {
	if staged, err := c.findStagedArtifact(digest); err == nil {
		_, err := c.verifyTrustedStagedArtifact(staged, digest)
		return err
	}
	checker := c.check
	if checker == nil {
		checker = c.configuredCheck()
	}
	if checker == nil {
		return errors.New("no trusted release source or verified staged artifact is available")
	}
	manifest, err := checker(ctx)
	if err != nil {
		return fmt.Errorf("trusted release source check failed: %w", err)
	}
	artifact, err := manifest.ArtifactFor(upgrade.Target{Deployment: c.deployment(), OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: upgrade.UpdaterProtocolVersion})
	if err != nil {
		return err
	}
	if !strings.EqualFold(artifact.SHA256, digest) {
		return errors.New("task digest does not match the trusted release artifact")
	}
	current, _, err := c.currentReleaseIdentity()
	if err != nil {
		return fmt.Errorf("read verified current release: %w", err)
	}
	if manifest.Version == current || !upgrade.IsVersionAtLeast(manifest.Version, current) {
		return fmt.Errorf("trusted release version %s is not newer than current version %s", manifest.Version, current)
	}
	if manifest.MinVersion != "" && !upgrade.IsVersionAtLeast(current, manifest.MinVersion) {
		return fmt.Errorf("current version %s is below manifest minimum version %s", current, manifest.MinVersion)
	}
	return nil
}

func (c *ServiceUpgradeController) verifyTrustedStagedArtifact(staged upgrade.StagedArtifact, digest string) (upgrade.StagedArtifact, error) {
	if !strings.EqualFold(staged.SHA256, digest) {
		return upgrade.StagedArtifact{}, errors.New("staged artifact digest does not match task digest")
	}
	target := upgrade.Target{Deployment: c.deployment(), OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: upgrade.UpdaterProtocolVersion}
	if staged.Deployment != target.Deployment || staged.OS != target.OS || staged.Arch != target.Arch {
		return upgrade.StagedArtifact{}, errors.New("staged artifact target does not match the installed deployment")
	}
	trusted := c.trustedKeys()
	if len(trusted) == 0 {
		return upgrade.StagedArtifact{}, errors.New("trusted release public keys are not configured")
	}
	manifest, err := upgrade.VerifySignedManifest(staged.ManifestEnvelope, trusted)
	if err != nil {
		return upgrade.StagedArtifact{}, fmt.Errorf("verify staged artifact manifest: %w", err)
	}
	artifact, err := manifest.ArtifactFor(target)
	if err != nil {
		return upgrade.StagedArtifact{}, err
	}
	if !strings.EqualFold(artifact.SHA256, digest) || !strings.EqualFold(staged.SHA256, artifact.SHA256) {
		return upgrade.StagedArtifact{}, errors.New("staged artifact digest does not match the signed release")
	}
	if staged.Size != artifact.Size {
		return upgrade.StagedArtifact{}, errors.New("staged artifact size does not match the signed release")
	}
	if staged.Deployment != artifact.Deployment || staged.OS != artifact.OS || staged.Arch != artifact.Arch {
		return upgrade.StagedArtifact{}, errors.New("staged artifact target does not match the signed release")
	}
	root := filepath.Clean(c.stagingRoot())
	path := filepath.Clean(staged.Path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return upgrade.StagedArtifact{}, errors.New("staged artifact is outside the updater staging root")
	}
	if !strings.HasPrefix(filepath.Base(path), strings.ToLower(digest)+"-") && !strings.HasPrefix(filepath.Base(path), digest+"-") {
		return upgrade.StagedArtifact{}, errors.New("staged artifact filename is not bound to the task digest")
	}
	current, _, err := c.currentReleaseIdentity()
	if err != nil {
		return upgrade.StagedArtifact{}, fmt.Errorf("read verified current release: %w", err)
	}
	if manifest.Version == current || !upgrade.IsVersionAtLeast(manifest.Version, current) {
		return upgrade.StagedArtifact{}, fmt.Errorf("signed release version %s is not newer than current version %s", manifest.Version, current)
	}
	if manifest.MinVersion != "" && !upgrade.IsVersionAtLeast(current, manifest.MinVersion) {
		return upgrade.StagedArtifact{}, fmt.Errorf("current version %s is below signed minimum version %s", current, manifest.MinVersion)
	}
	verified := staged
	verified.ReleaseNotes, verified.PublishedAt = manifest.ReleaseNotes, manifest.PublishedAt
	verified.Filename = artifact.Filename
	verified.Size = artifact.Size
	verified.SHA256 = artifact.SHA256
	verified.ReleaseID = manifest.ReleaseID
	verified.Version = manifest.Version
	verified.BuildID = manifest.BuildID
	verified.Deployment = artifact.Deployment
	verified.OS = artifact.OS
	verified.Arch = artifact.Arch
	verified.Images = append([]upgrade.ImageArtifact(nil), artifact.Images...)
	verified.Compatibility = upgrade.CloneCompatibility(manifest.Compatibility)
	verified.ManifestEnvelope = bytes.Clone(manifest.Envelope)
	if err := verifyStagedFile(verified); err != nil {
		return upgrade.StagedArtifact{}, err
	}
	return verified, nil
}

func (c *ServiceUpgradeController) prepareTask(ctx context.Context, record upgrade.TaskRecord) {
	staged, err := c.prepareTaskArtifact(ctx, record.TargetDigest)
	if err != nil {
		c.failPendingTask(record.TaskID, err)
		return
	}
	preflight, err := c.collectPreflight(ctx)
	if err != nil {
		c.failPendingTask(record.TaskID, fmt.Errorf("collect installation preflight: %w", err))
		return
	}
	if err := validateManualCompatibility(staged.Compatibility, preflight); err != nil {
		c.failPendingTask(record.TaskID, err)
		return
	}
	if err := c.ensureTaskMetadata(record.TaskID, func(task *upgrade.TaskRecord) {
		task.StagingPath, task.Source, task.Version, task.BuildID = staged.Path, "online", staged.Version, staged.BuildID
		task.ReleaseNotes, task.PublishedAt = staged.ReleaseNotes, staged.PublishedAt
		task.Images = append([]upgrade.ImageArtifact(nil), staged.Images...)
		task.Compatibility = upgrade.CloneCompatibility(staged.Compatibility)
		snapshot := preflight
		task.Preflight = &snapshot
	}); err != nil {
		c.failPendingTask(record.TaskID, fmt.Errorf("persist installation preflight: %w", err))
		return
	}
	for _, phase := range []upgrade.TaskPhase{upgrade.PhaseVerifying, upgrade.PhasePreparing, upgrade.PhaseAwaitingConfirmation} {
		if err := c.ensureTaskPhase(record.TaskID, phase); err != nil {
			c.failPendingTask(record.TaskID, fmt.Errorf("advance preparation to %s: %w", phase, err))
			return
		}
	}
}

// validateManualCompatibility only applies an explicit automatic declaration
// when it claims the release is safe to apply unattended. Packages without a
// declaration, or declarations that require manual handling, remain eligible
// for an administrator-confirmed upgrade.
func validateManualCompatibility(declaration *upgrade.CompatibilityDeclaration, preflight upgrade.PreflightSnapshot) error {
	if declaration == nil || !declaration.AutomaticAllowed {
		return nil
	}
	if err := declaration.AllowsAutomatic(preflight); err != nil {
		return fmt.Errorf("signed automatic compatibility does not match current installation: %w", err)
	}
	return nil
}

// failPendingTask changes only a preparation-stage task into a durable failed
// task. TaskStore.Cancel performs the phase check while holding its lock, so a
// stale HTTP request cannot mark an already-started upgrade as failed.
func (c *ServiceUpgradeController) failPendingTask(taskID string, cause error) {
	if taskID == "" || cause == nil {
		return
	}
	store, err := c.ensureStore()
	if err != nil || store.Cancel(taskID) != nil {
		return
	}
	_ = store.UpdateMetadata(taskID, func(task *upgrade.TaskRecord) error {
		task.Error = cause.Error()
		return nil
	})
}

func (c *ServiceUpgradeController) ensureTaskMetadata(taskID string, update func(*upgrade.TaskRecord)) error {
	store, err := c.ensureStore()
	if err != nil {
		return err
	}
	return store.UpdateMetadata(taskID, func(record *upgrade.TaskRecord) error { update(record); return nil })
}

func (c *ServiceUpgradeController) ensureTaskPhase(taskID string, phase upgrade.TaskPhase) error {
	store, err := c.ensureStore()
	if err != nil {
		return err
	}
	return store.MarkPhase(taskID, phase)
}

func (c *ServiceUpgradeController) prepareTaskArtifact(ctx context.Context, digest string) (upgrade.StagedArtifact, error) {
	if staged, err := c.findStagedArtifact(digest); err == nil && staged.Path != "" {
		return c.verifyTrustedStagedArtifact(staged, digest)
	}
	checker := c.check
	if checker == nil {
		checker = c.configuredCheck()
	}
	if checker == nil {
		return upgrade.StagedArtifact{}, errors.New("no online release source or matching offline artifact")
	}
	manifest, err := checker(ctx)
	if err != nil {
		return upgrade.StagedArtifact{}, err
	}
	artifact, err := manifest.ArtifactFor(upgrade.Target{Deployment: c.deployment(), OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: 1})
	if err != nil {
		return upgrade.StagedArtifact{}, err
	}
	if !strings.EqualFold(artifact.SHA256, digest) {
		return upgrade.StagedArtifact{}, errors.New("task digest does not match the trusted release artifact")
	}
	if err := c.downloadArtifact(ctx, manifest, artifact); err != nil {
		return upgrade.StagedArtifact{}, err
	}
	staged, err := c.findStagedArtifact(digest)
	if err != nil {
		return upgrade.StagedArtifact{}, err
	}
	return c.verifyTrustedStagedArtifact(staged, digest)
}

func (c *ServiceUpgradeController) downloadArtifact(ctx context.Context, manifest upgrade.Manifest, artifact upgrade.Artifact) error {
	if len(manifest.Envelope) == 0 {
		return errors.New("verified manifest envelope is required before staging an online artifact")
	}
	root := c.stagingRoot()
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, ".download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = tmp.Close(); _ = os.Remove(tmpPath) }()
	if _, err := upgrade.DownloadArtifactWithRedirects(ctx, c.httpClient, artifact, tmp, upgrade.DefaultReleaseRedirectHosts); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	destination := filepath.Join(root, artifact.SHA256+"-"+filepath.Base(artifact.Filename))
	if err := os.Rename(tmpPath, destination); err != nil {
		return err
	}
	staged := upgrade.StagedArtifact{
		ReleaseNotes: manifest.ReleaseNotes, PublishedAt: manifest.PublishedAt, Path: destination, Filename: artifact.Filename, Size: artifact.Size, SHA256: artifact.SHA256, ReleaseID: manifest.ReleaseID, Version: manifest.Version, BuildID: manifest.BuildID, Deployment: artifact.Deployment, OS: artifact.OS, Arch: artifact.Arch, Images: append([]upgrade.ImageArtifact(nil), artifact.Images...), Compatibility: upgrade.CloneCompatibility(manifest.Compatibility), ManifestEnvelope: append([]byte(nil), manifest.Envelope...)}
	if err := upgrade.SaveStagedMetadata(destination+".json", staged); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}

func (c *ServiceUpgradeController) findStagedArtifact(digest string) (upgrade.StagedArtifact, error) {
	if len(digest) != 64 {
		return upgrade.StagedArtifact{}, errors.New("invalid artifact digest")
	}
	paths, err := filepath.Glob(filepath.Join(c.stagingRoot(), digest+"-*.json"))
	if err != nil || len(paths) == 0 {
		return upgrade.StagedArtifact{}, fmt.Errorf("staged artifact %q not found", digest)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		return upgrade.StagedArtifact{}, err
	}
	var staged upgrade.StagedArtifact
	if err := json.Unmarshal(data, &staged); err != nil {
		return upgrade.StagedArtifact{}, err
	}
	return staged, nil
}

func verifyStagedFile(staged upgrade.StagedArtifact) error {
	if staged.Path == "" || filepath.Base(staged.Path) == "." || filepath.Base(staged.Path) == ".." {
		return errors.New("staged artifact path is invalid")
	}
	info, err := os.Lstat(staged.Path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("staged artifact is not a regular file")
	}
	file, err := os.Open(staged.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	if staged.Size < 0 || staged.Size > 8<<30 {
		return errors.New("staged artifact size is invalid")
	}
	// Read and hash locally; DownloadArtifact intentionally handles only remote URLs.
	hasher := sha256.New()
	written, err := io.Copy(hasher, file)
	if err != nil || written != staged.Size {
		return errors.New("staged artifact size does not match metadata")
	}
	if !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), staged.SHA256) {
		return errors.New("staged artifact digest does not match metadata")
	}
	return nil
}

func (c *ServiceUpgradeController) GetTask(ctx *gin.Context) {
	if !c.local {
		response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{Protocol: upgrade.UpdaterProtocolVersion, Action: upgrade.ActionGetTask, TaskID: ctx.Param("id")})
		if err != nil {
			c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
			return
		}
		if !response.OK || response.Task == nil {
			c.FailAndAbort(ctx, "升级任务不存在", upgradeRPCResponseError(response), http.StatusNotFound)
			return
		}
		c.Success(ctx, publicUpgradeTask(response.Task))
		return
	}
	store, err := c.ensureStore()
	if err != nil {
		c.FailAndAbort(ctx, "读取升级任务失败", err, http.StatusServiceUnavailable)
		return
	}
	record, err := store.Get(ctx.Param("id"))
	if err != nil {
		c.FailAndAbort(ctx, "升级任务不存在", err, http.StatusNotFound)
		return
	}
	c.Success(ctx, publicUpgradeTask(&record))
}

func (c *ServiceUpgradeController) CancelTask(ctx *gin.Context) {
	if !c.local {
		response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{Protocol: upgrade.UpdaterProtocolVersion, Action: upgrade.ActionCancel, TaskID: ctx.Param("id")})
		if err != nil {
			c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
			return
		}
		if !response.OK {
			c.FailAndAbort(ctx, "当前阶段不可取消", upgradeRPCResponseError(response), http.StatusConflict)
			return
		}
		c.SuccessWithMessage(ctx, "升级任务已取消", publicUpgradeTask(response.Task))
		return
	}
	store, err := c.ensureStore()
	if err != nil {
		c.FailAndAbort(ctx, "取消升级任务失败", err, http.StatusServiceUnavailable)
		return
	}
	if err := store.Cancel(ctx.Param("id")); err != nil {
		c.FailAndAbort(ctx, "当前阶段不可取消", err, http.StatusConflict)
		return
	}
	c.SuccessWithMessage(ctx, "升级任务已取消", nil)
}

// ApplyTask forwards the explicit confirmation to the local privileged
// updater. The HTTP request remains short and never performs service stop or
// migration work itself.
func (c *ServiceUpgradeController) ApplyTask(ctx *gin.Context) {
	record, err := c.getTask(ctx)
	if err != nil {
		return
	}
	if c.local && record.Phase == upgrade.PhaseAwaitingConfirmation {
		if err := c.authorizeManualApply(ctx.Request.Context(), record); err != nil {
			c.failPendingTask(record.TaskID, err)
			c.FailAndAbort(ctx, "升级前置条件已变化，请重新创建升级任务", err, http.StatusConflict)
			return
		}
	}
	response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{Protocol: upgrade.UpdaterProtocolVersion, Action: upgrade.ActionApply, TaskID: record.TaskID, TargetDigest: record.TargetDigest, Confirmation: true})
	if err != nil {
		c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
		return
	}
	if !response.OK {
		c.FailAndAbort(ctx, "升级助手拒绝应用", errors.New(response.Error), http.StatusConflict)
		return
	}
	c.SuccessWithMessage(ctx, "升级已提交给助手", publicUpgradeTask(response.Task))
}

func (c *ServiceUpgradeController) authorizeManualApply(ctx context.Context, record upgrade.TaskRecord) error {
	if record.Preflight == nil {
		return errors.New("manual upgrade task has no installation preflight snapshot")
	}
	current, err := c.collectPreflight(ctx)
	if err != nil {
		return fmt.Errorf("recheck installation preflight: %w", err)
	}
	if current != *record.Preflight {
		return errors.New("installation state changed since preparation; a new preflight and confirmation are required")
	}
	return validateManualCompatibility(record.Compatibility, current)
}

func (c *ServiceUpgradeController) RollbackTask(ctx *gin.Context) {
	record, err := c.getTask(ctx)
	if err != nil {
		return
	}
	response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{Protocol: upgrade.UpdaterProtocolVersion, Action: upgrade.ActionRollback, TaskID: record.TaskID})
	if err != nil {
		c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
		return
	}
	if !response.OK {
		c.FailAndAbort(ctx, "升级助手拒绝回退", errors.New(response.Error), http.StatusConflict)
		return
	}
	c.SuccessWithMessage(ctx, "回退已提交给助手", publicUpgradeTask(response.Task))
}

func (c *ServiceUpgradeController) getTask(ctx *gin.Context) (upgrade.TaskRecord, error) {
	if !c.local {
		response, err := c.callUpgradeRPC(ctx.Request.Context(), upgrade.ProtocolRequest{Protocol: upgrade.UpdaterProtocolVersion, Action: upgrade.ActionGetTask, TaskID: ctx.Param("id")})
		if err != nil {
			c.FailAndAbort(ctx, "升级助手不可用", err, http.StatusServiceUnavailable)
			return upgrade.TaskRecord{}, err
		}
		if !response.OK || response.Task == nil {
			err := upgradeRPCResponseError(response)
			c.FailAndAbort(ctx, "升级任务不存在", err, http.StatusNotFound)
			return upgrade.TaskRecord{}, err
		}
		return *response.Task, nil
	}
	store, err := c.ensureStore()
	if err != nil {
		c.FailAndAbort(ctx, "读取升级任务失败", err, http.StatusServiceUnavailable)
		return upgrade.TaskRecord{}, err
	}
	record, err := store.Get(ctx.Param("id"))
	if err != nil {
		c.FailAndAbort(ctx, "升级任务不存在", err, http.StatusNotFound)
		return upgrade.TaskRecord{}, err
	}
	return record, nil
}

func (c *ServiceUpgradeController) updaterSocket() string {
	if value := strings.TrimSpace(os.Getenv("UVP_UPDATER_SOCKET")); value != "" {
		return value
	}
	return "/run/uvp/updater.sock"
}

func (c *ServiceUpgradeController) deployment() string {
	if app.ConfigYml != nil {
		if value := app.ConfigYml.GetString("upgrade.deployment"); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(os.Getenv("UVP_UPGRADE_DEPLOYMENT")); value != "" {
		return value
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "compose"
	}
	return "standalone"
}

func (c *ServiceUpgradeController) currentVersion() string {
	version, _, err := c.currentReleaseIdentity()
	if err != nil {
		return ""
	}
	return version
}

func (c *ServiceUpgradeController) currentReleaseIdentity() (string, string, error) {
	data, err := os.ReadFile(c.versionPath)
	if err != nil {
		return "", "", fmt.Errorf("read current version: %w", err)
	}
	var identity struct {
		Version string `json:"version"`
		BuildID string `json:"build_id"`
	}
	if err := json.Unmarshal(data, &identity); err != nil {
		return "", "", fmt.Errorf("decode current version: %w", err)
	}
	identity.Version = strings.TrimSpace(identity.Version)
	identity.BuildID = strings.TrimSpace(identity.BuildID)
	if identity.Version == "" {
		return "", "", errors.New("current version is missing")
	}
	if identity.BuildID == "" {
		return identity.Version, "", nil
	}
	runtimeVersion, runtimeBuildID := c.runtimeIdentity()
	runtimeVersion = strings.TrimSpace(runtimeVersion)
	runtimeBuildID = strings.TrimSpace(runtimeBuildID)
	if runtimeBuildID == "" {
		return "", "", errors.New("running binary has no embedded build identity")
	}
	if runtimeVersion != "" && runtimeVersion != identity.Version {
		return "", "", errors.New("version file does not match running binary")
	}
	if runtimeBuildID != identity.BuildID {
		return "", "", errors.New("version file does not match running binary")
	}
	return identity.Version, runtimeBuildID, nil
}

func (c *ServiceUpgradeController) configuredCheck() func(context.Context) (upgrade.Manifest, error) {
	if app.ConfigYml == nil {
		return nil
	}
	source := c.releaseSourceValue()
	trusted := c.trustedKeys()
	if len(trusted) == 0 {
		return nil
	}
	return func(ctx context.Context) (upgrade.Manifest, error) {
		channelEnvelope, err := upgrade.FetchSignedManifest(ctx, c.httpClient, source)
		if err != nil {
			return upgrade.Manifest{}, err
		}
		manifest, channel, err := upgrade.FetchChannelRelease(ctx, c.httpClient, source, channelEnvelope, trusted, time.Now())
		if err != nil {
			return upgrade.Manifest{}, err
		}
		if err := c.acceptSequence(channel.Channel, channel.Sequence, channelEnvelope); err != nil {
			return upgrade.Manifest{}, err
		}
		return manifest, nil
	}
}

func (c *ServiceUpgradeController) trustedKeys() map[string]ed25519.PublicKey {
	// The embedded trust root always applies. Configured keys are merged on top
	// so a self-hosted channel can add its own signer without dropping the
	// official one.
	keys := upgrade.OfficialPublicKeys()
	if app.ConfigYml == nil {
		return keys
	}
	for id, key := range configuredUpgradeKeys(app.ConfigYml.Get("upgrade.trusted_keys")) {
		keys[id] = key
	}
	return keys
}

func (c *ServiceUpgradeController) stagingRoot() string {
	return filepath.Join(upgradeStateRoot(c.versionPath), "staging")
}

const maxUpgradeUploadSize int64 = 8 << 30

func (c *ServiceUpgradeController) inboxRoot() string {
	if value := strings.TrimSpace(os.Getenv("UVP_UPGRADE_INBOX")); value != "" {
		return filepath.Clean(value)
	}
	if app.ConfigYml != nil {
		for _, key := range []string{"upgrade.inbox", "upgrade.inbox_root"} {
			if value := strings.TrimSpace(app.ConfigYml.GetString(key)); value != "" {
				return filepath.Clean(value)
			}
		}
	}
	// Registration.StagingInbox uses this same default when no administrator
	// supplied staging_root, while stagingRoot remains the local test seam.
	return filepath.Join(upgradeStateRoot(c.versionPath), "staging")
}

func validateUpgradeInboxDirectory(root string) error {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) || root == "." {
		return errors.New("upgrade inbox root must be an absolute path")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("upgrade inbox root must be a real directory")
	}
	return nil
}

func writeUpgradeInboxUpload(ctx context.Context, root string, input io.Reader) (string, error) {
	if err := validateUpgradeInboxDirectory(root); err != nil {
		return "", err
	}
	if input == nil {
		return "", errors.New("upgrade upload is required")
	}
	temporary, err := os.CreateTemp(root, ".upload-*")
	if err != nil {
		return "", fmt.Errorf("create upgrade inbox temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := ""
	defer func() {
		_ = temporary.Close()
		if committed == "" {
			_ = os.Remove(temporaryPath)
		}
	}()
	written, err := io.Copy(temporary, io.LimitReader(input, maxUpgradeUploadSize+1))
	if err != nil {
		return "", fmt.Errorf("read upgrade upload: %w", err)
	}
	if written > maxUpgradeUploadSize {
		return "", fmt.Errorf("upgrade upload exceeds %d bytes", maxUpgradeUploadSize)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync upgrade upload: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close upgrade upload: %w", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", fmt.Errorf("create upgrade inbox token: %w", err)
		}
		token := hex.EncodeToString(raw[:])
		destination := filepath.Join(root, token)
		if _, err := os.Lstat(destination); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err := os.Rename(temporaryPath, destination); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", fmt.Errorf("commit upgrade upload: %w", err)
		}
		committed = destination
		if err := syncUpgradeDirectory(root); err != nil {
			_ = os.Remove(destination)
			committed = ""
			return "", fmt.Errorf("sync upgrade inbox: %w", err)
		}
		return destination, nil
	}
	return "", errors.New("unable to allocate upgrade inbox token")
}

// syncUpgradeDirectory delegates to the upgrade package so that the platform
// split lives in one place: Windows cannot fsync a directory handle, Unix must.
func syncUpgradeDirectory(path string) error {
	return upgrade.SyncDirectory(path)
}

func removeUpgradeInboxFile(path string) {
	if path != "" {
		_ = os.Remove(path)
		_ = syncUpgradeDirectory(filepath.Dir(path))
	}
}

func (c *ServiceUpgradeController) releaseSourceValue() string {
	if source := strings.TrimSpace(c.releaseSource); source != "" {
		return source
	}
	if app.ConfigYml != nil {
		for _, key := range []string{"upgrade.release_source", "upgrade.releaseSource"} {
			if source := strings.TrimSpace(app.ConfigYml.GetString(key)); source != "" {
				return source
			}
		}
	}
	// The official channel is built into the binary and is the normal case for
	// a shipped appliance; configuration above only overrides it for self-hosted
	// channels.
	return upgrade.OfficialReleaseSource
}

func configuredUpgradeKeys(value any) map[string]ed25519.PublicKey {
	keys := make(map[string]ed25519.PublicKey)
	add := func(id, encoded string) {
		id, encoded = strings.TrimSpace(id), strings.TrimSpace(encoded)
		if id == "" || encoded == "" {
			return
		}
		var raw []byte
		if len(encoded) == ed25519.PublicKeySize*2 {
			if decoded, err := hex.DecodeString(encoded); err == nil {
				raw = decoded
			}
		} else if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
			raw = decoded
		}
		if len(raw) == ed25519.PublicKeySize {
			keys[id] = ed25519.PublicKey(raw)
		}
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		for id, encoded := range typed {
			if text, ok := encoded.(string); ok {
				add(id, text)
			}
		}
	case []interface{}:
		for _, item := range typed {
			if text, ok := item.(string); ok {
				parts := strings.SplitN(text, "=", 2)
				if len(parts) == 2 {
					add(parts[0], parts[1])
				}
			}
		}
	case []string:
		for _, text := range typed {
			parts := strings.SplitN(text, "=", 2)
			if len(parts) == 2 {
				add(parts[0], parts[1])
			}
		}
	}
	return keys
}

func (c *ServiceUpgradeController) acceptSequence(channel string, sequence uint64, envelope []byte) error {
	c.sequenceMu.Lock()
	defer c.sequenceMu.Unlock()
	state := map[string]struct {
		Sequence uint64 `json:"sequence"`
		Digest   string `json:"digest"`
	}{}
	if data, err := os.ReadFile(c.sequencePath); err == nil {
		if json.Unmarshal(data, &state) != nil {
			return errors.New("upgrade sequence state is corrupt")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	digest := sha256Hex(envelope)
	if previous, ok := state[channel]; ok {
		if sequence < previous.Sequence {
			return errors.New("manifest sequence is older than accepted sequence")
		}
		if sequence == previous.Sequence && digest != previous.Digest {
			return errors.New("manifest sequence points to different payload")
		}
		if sequence == previous.Sequence {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(c.sequencePath), 0700); err != nil {
		return err
	}
	state[channel] = struct {
		Sequence uint64 `json:"sequence"`
		Digest   string `json:"digest"`
	}{sequence, digest}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp := c.sequencePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.sequencePath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func activeTask(records []upgrade.TaskRecord) *upgrade.TaskRecord {
	for i := range records {
		switch records[i].Phase {
		case upgrade.PhaseSucceeded, upgrade.PhaseFailed, upgrade.PhaseRolledBack:
		default:
			return &records[i]
		}
	}
	return nil
}

// publicUpgradeTask is the management API boundary. The helper needs absolute
// staging, backup, and previous-target paths to recover a task, but exposing
// those paths to a browser leaks host layout and backup locations. Keep the
// diagnostic fields that an administrator can act on and omit execution
// internals, preflight snapshots, compatibility declarations, and policy
// credentials.
type publicUpgradeTaskRecord struct {
	ReleaseNotes   string            `json:"release_notes,omitempty"`
	PublishedAt    string            `json:"published_at,omitempty"`
	TaskID         string            `json:"task_id"`
	IdempotencyKey string            `json:"idempotency_key"`
	Actor          string            `json:"actor"`
	TargetDigest   string            `json:"target_digest"`
	Source         string            `json:"source,omitempty"`
	Version        string            `json:"version,omitempty"`
	BuildID        string            `json:"build_id,omitempty"`
	Phase          upgrade.TaskPhase `json:"phase"`
	FailurePhase   upgrade.TaskPhase `json:"failure_phase,omitempty"`
	Committed      bool              `json:"committed"`
	Error          string            `json:"error,omitempty"`
	PolicyVersion  uint64            `json:"policy_version,omitempty"`
	UpdatedAt      string            `json:"updated_at,omitempty"`
}

type publicUpgradeImportRecord struct {
	UploadID     string               `json:"upload_id"`
	JobID        string               `json:"job_id"`
	Phase        upgrade.ImportPhase  `json:"phase"`
	Status       upgrade.ImportPhase  `json:"status"`
	Error        string               `json:"error,omitempty"`
	Message      string               `json:"message,omitempty"`
	CreatedAt    string               `json:"created_at"`
	UpdatedAt    string               `json:"updated_at"`
	Filename     string               `json:"filename,omitempty"`
	Size         int64                `json:"size,omitempty"`
	SHA256       string               `json:"sha256,omitempty"`
	ReleaseID    string               `json:"release_id,omitempty"`
	ReleaseNotes string               `json:"release_notes,omitempty"`
	PublishedAt  string               `json:"published_at,omitempty"`
	Version      string               `json:"version,omitempty"`
	BuildID      string               `json:"build_id,omitempty"`
	Deployment   string               `json:"deployment,omitempty"`
	OS           string               `json:"os,omitempty"`
	Arch         string               `json:"arch,omitempty"`
	Staged       *publicStagedUpgrade `json:"staged,omitempty"`
}

type publicStagedUpgrade struct {
	Filename      string                            `json:"filename"`
	Size          int64                             `json:"size"`
	SHA256        string                            `json:"sha256"`
	ReleaseID     string                            `json:"release_id"`
	Version       string                            `json:"version"`
	BuildID       string                            `json:"build_id"`
	ReleaseNotes  string                            `json:"release_notes,omitempty"`
	PublishedAt   string                            `json:"published_at,omitempty"`
	Deployment    string                            `json:"deployment"`
	OS            string                            `json:"os"`
	Arch          string                            `json:"arch"`
	Images        []upgrade.ImageArtifact           `json:"images,omitempty"`
	Compatibility *upgrade.CompatibilityDeclaration `json:"compatibility,omitempty"`
}

func publicUpgradeImport(record *upgrade.ImportRecord) *publicUpgradeImportRecord {
	if record == nil {
		return nil
	}
	public := &publicUpgradeImportRecord{
		UploadID: record.InboxToken, JobID: record.ImportID, Phase: record.Phase, Status: record.Phase, Error: record.Error, Message: record.Error,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
	if record.Staged != nil {
		staged := record.Staged
		public.Filename, public.Size, public.SHA256 = staged.Filename, staged.Size, staged.SHA256
		public.ReleaseID, public.ReleaseNotes, public.PublishedAt = staged.ReleaseID, staged.ReleaseNotes, staged.PublishedAt
		public.Version, public.BuildID, public.Deployment, public.OS, public.Arch = staged.Version, staged.BuildID, staged.Deployment, staged.OS, staged.Arch
		public.Staged = &publicStagedUpgrade{
			Filename: staged.Filename, Size: staged.Size, SHA256: staged.SHA256,
			ReleaseID: staged.ReleaseID, Version: staged.Version, BuildID: staged.BuildID,
			ReleaseNotes: staged.ReleaseNotes, PublishedAt: staged.PublishedAt,
			Deployment: staged.Deployment, OS: staged.OS, Arch: staged.Arch,
			Images:        append([]upgrade.ImageArtifact(nil), staged.Images...),
			Compatibility: upgrade.CloneCompatibility(staged.Compatibility),
		}
	}
	return public
}

func publicUpgradeTask(record *upgrade.TaskRecord) *publicUpgradeTaskRecord {
	if record == nil {
		return nil
	}
	return &publicUpgradeTaskRecord{
		TaskID: record.TaskID, IdempotencyKey: record.IdempotencyKey, Actor: record.Actor,
		TargetDigest: record.TargetDigest, Source: record.Source, Version: record.Version,
		BuildID: record.BuildID, Phase: record.Phase, FailurePhase: record.FailurePhase,
		Committed: record.Committed, Error: record.Error, PolicyVersion: record.PolicyVersion,
		UpdatedAt: record.UpdatedAt, ReleaseNotes: record.ReleaseNotes, PublishedAt: record.PublishedAt,
	}
}

func publicUpgradeTasks(records []upgrade.TaskRecord) []*publicUpgradeTaskRecord {
	result := make([]*publicUpgradeTaskRecord, 0, len(records))
	for i := range records {
		result = append(result, publicUpgradeTask(&records[i]))
	}
	return result
}

func fmtUint(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
