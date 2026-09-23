// Package authoritytest provides isolated real-process fixtures, not test
// authority tokens. Production code must not import this package.
package authoritytest

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
)

const leafEnv = "UVP_AUTHORITY_TEST_LEAF"

var fixture struct {
	sync.Mutex
	db        *sql.DB
	authority *processauthority.Authority
}

// Authority reuses only the one concrete authority of the current test process.
// A second database is a fixture bug, not permission to register again.
func Authority(t *testing.T, db *gorm.DB) *processauthority.Authority {
	t.Helper()
	fixture.Lock()
	defer fixture.Unlock()
	raw, err := db.DB()
	require.NoError(t, err)
	if fixture.db == nil {
		fixture.db = raw
		fixture.authority = Register(t, db, "")
	} else {
		require.Same(t, fixture.db, raw, "one authorized leaf must use one database")
	}
	return fixture.authority
}

// InProcess belongs at the start of the actual fixture-owning leaf, before
// resources are created. The parent returns normally after verifying the child.
func InProcess(t *testing.T) bool {
	t.Helper()
	if target := os.Getenv(leafEnv); target != "" {
		// Go's slash matcher also visits prefix/sibling names. Those visits
		// must not create another subprocess or another authorized database.
		return target == t.Name()
	}
	parts := strings.Split(t.Name(), "/")
	for i, part := range parts {
		parts[i] = "^" + regexp.QuoteMeta(part) + "$"
	}
	binary, err := os.Executable()
	require.NoError(t, err)
	limit := time.Now().Add(60 * time.Second)
	if deadline, ok := t.Deadline(); ok && deadline.Before(limit) {
		limit = deadline
	}
	ctx, cancel := context.WithDeadline(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run="+strings.Join(parts, "/"), "-test.count=1", "-test.v")
	cmd.WaitDelay = time.Second
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, leafEnv+"=") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, leafEnv+"="+t.Name())
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "authorized child %s: %s", t.Name(), out)
	pass := regexp.MustCompile(`(?m)^\s*--- PASS: ` + regexp.QuoteMeta(t.Name()) + ` \(`)
	require.True(t, pass.Match(out), "child must execute and pass exact leaf, not skip: %s", out)
	t.Logf("authorized child output:\n%s", out)
	return false
}

// Register creates genuine authority once in this OS process. Call before
// registering owner cleanup, so LIFO cleanup joins owners before releasing it.
func Register(t *testing.T, db *gorm.DB, stateDir string) *processauthority.Authority {
	t.Helper()
	if stateDir == "" {
		stateDir = StateDirectory(t)
	}
	require.NoError(t, db.AutoMigrate(&models.ProcessGeneration{}, &models.ProcessAuthority{}))
	lock, err := processauthority.AcquireLocalLock(stateDir)
	require.NoError(t, err)
	a, err := processauthority.Register(context.Background(), db, lock)
	if err != nil {
		_ = lock.Close()
	}
	require.NoError(t, err)
	t.Cleanup(func() { a.Seal(); require.NoError(t, lock.Close()) })
	return a
}

func StateDirectory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	privateDirectory(t, dir)
	return dir
}
