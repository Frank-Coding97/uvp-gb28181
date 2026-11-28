package casbinhelper

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type reloadPolicyAdapter struct {
	loads  atomic.Int32
	writes atomic.Int32
}

func (a *reloadPolicyAdapter) LoadPolicy(m model.Model) error {
	a.loads.Add(1)
	for _, policy := range [][]string{{"g", "user_7", "role_1", "*"}, {"p", "role_1", "/items/:id", "GET", "*"}} {
		if err := persist.LoadPolicyArray(policy, m); err != nil {
			return err
		}
	}
	return nil
}

func (a *reloadPolicyAdapter) SavePolicy(model.Model) error { a.writes.Add(1); return nil }
func (a *reloadPolicyAdapter) AddPolicy(string, string, []string) error {
	a.writes.Add(1)
	return nil
}
func (a *reloadPolicyAdapter) RemovePolicy(string, string, []string) error {
	a.writes.Add(1)
	return nil
}
func (a *reloadPolicyAdapter) RemoveFilteredPolicy(string, string, int, ...string) error {
	a.writes.Add(1)
	return nil
}
func (a *reloadPolicyAdapter) AddPolicies(string, string, [][]string) error {
	a.writes.Add(1)
	return nil
}
func (a *reloadPolicyAdapter) RemovePolicies(string, string, [][]string) error {
	a.writes.Add(1)
	return nil
}

type manualPolicyReloader interface{ ReloadPolicy() error }

func TestPolicyReloadReadsPersistedPolicyWithoutWritingIt(t *testing.T) {
	adapter := &reloadPolicyAdapter{}
	helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, adapter)}
	reloader, ok := any(helper).(manualPolicyReloader)
	require.True(t, ok, "helper must support a read-only policy reload")
	require.NoError(t, helper.RemovePolicyForRole(1, "/items/:id", "GET"))
	allowed, err := helper.Enforce("user_7", "/items/42", "GET", "*")
	require.NoError(t, err)
	require.False(t, allowed)
	writes, loads := adapter.writes.Load(), adapter.loads.Load()
	require.NoError(t, reloader.ReloadPolicy())
	require.Equal(t, writes, adapter.writes.Load())
	require.Greater(t, adapter.loads.Load(), loads)
	allowed, err = helper.Enforce("user_7", "/items/42", "GET", "*")
	require.NoError(t, err)
	require.True(t, allowed)
	uninitialized, ok := any(&CasbinHelper{}).(manualPolicyReloader)
	require.True(t, ok)
	require.Error(t, uninitialized.ReloadPolicy())
}

func TestPolicyReloadBlocksReadersUntilLoadingFinishes(t *testing.T) {
	installCasbinObserver(t)
	adapter := newBlockingPolicyAdapter()
	helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, adapter)}
	adapter.block.Store(true)
	var release sync.Once
	defer release.Do(func() { close(adapter.release) })
	startAutoLoadForTest(t, helper, time.Millisecond)
	select {
	case <-adapter.started:
	case <-time.After(2 * time.Second):
		t.Fatal("policy reload did not enter adapter")
	}
	read := make(chan error, 1)
	go func() {
		_, err := helper.Enforce("user_7", "/items/42", "GET", "*")
		read <- err
	}()
	early := false
	select {
	case <-read:
		t.Error("Enforce ran while the policy model was being reloaded")
		early = true
	case <-time.After(20 * time.Millisecond):
	}
	release.Do(func() { close(adapter.release) })
	helper.Close()
	if early {
		return
	}
	select {
	case err := <-read:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("reader did not finish after policy reload")
	}
}

func TestPolicyReloadConcurrentReaders(t *testing.T) {
	installCasbinObserver(t)
	adapter := &reloadPolicyAdapter{}
	helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, adapter)}
	startAutoLoadForTest(t, helper, time.Microsecond)
	defer helper.Close()
	var readers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for i := 0; i < 200; i++ {
				_, err := helper.Enforce("user_7", "/items/42", "GET", "*")
				if err != nil {
					t.Error(err)
				}
				_, err = helper.GetRolesForUserByID(7)
				if err != nil {
					t.Error(err)
				}
				_, _ = helper.GetUsersForRole(1)
				_, _ = helper.HasRoleForUser(7, 1)
				_, _ = helper.GetPermissionsForUser(7)
			}
		}()
	}
	readers.Wait()
	helper.Close()
	require.Greater(t, adapter.loads.Load(), int32(1))
}

func TestPolicyReloadConcurrentPolicyMutations(t *testing.T) {
	installCasbinObserver(t)
	adapter := &reloadPolicyAdapter{}
	helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, adapter)}
	startAutoLoadForTest(t, helper, time.Microsecond)
	defer helper.Close()
	var workers sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := 0; i < 50; i++ {
				for _, err := range []error{
					helper.AddRolesForUserByID(9, []uint{8}),
					helper.AddRoleInheritance(8, 2),
					helper.AddPolicyForRole(2, "/private", "GET"),
					helper.AddPoliciesForRole(2, [][]string{{"/batch", "GET"}}),
					helper.RemovePolicyForRole(2, "/private", "GET"),
					helper.RemoveAllPoliciesForRole(2),
					helper.DeleteRoleInheritance(8, 2),
					helper.DeleteRolesForUserByID(9, []uint{8}),
					helper.ReloadPolicy(),
				} {
					if err != nil {
						t.Error(err)
						return
					}
				}
			}
		}()
	}
	workers.Wait()
	helper.Close()
	require.Greater(t, adapter.loads.Load(), int32(1))
	require.Greater(t, adapter.writes.Load(), int32(0))
}

func TestPolicyReloadReinitializationDoesNotDeadlockReadersOrClose(t *testing.T) {
	installCasbinObserver(t)
	previous := app.ConfigYml
	app.ConfigYml = staticCasbinConfig{}
	t.Cleanup(func() { app.ConfigYml = previous })
	db := transactionPolicyDB(t, false)
	helper := NewCasbinHelper()
	require.NoError(t, helper.InitCasbin(db, testModelConfig))
	require.NoError(t, helper.AddRolesForUserByID(7, []uint{1}))
	require.NoError(t, helper.AddPolicyForRole(1, "/items/:id", "GET"))
	startAutoLoadForTest(t, helper, time.Microsecond)
	defer helper.Close()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for i := 0; i < 200; i++ {
			allowed, err := helper.Enforce("user_7", "/items/42", "GET", "*")
			if err != nil || !allowed {
				t.Errorf("request lost initialized policy: allowed=%v err=%v", allowed, err)
				return
			}
		}
	}()
	for i := 0; i < 3; i++ {
		require.NoError(t, helper.InitCasbin(db, testModelConfig))
		startAutoLoadForTest(t, helper, time.Microsecond)
	}
	helper.Close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("reader did not finish after reinitialization and close")
	}
}
