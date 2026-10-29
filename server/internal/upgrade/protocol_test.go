package upgrade

import "testing"

func TestProtocolRejectsShellPathAndUnknownFields(t *testing.T) {
	valid := `{"protocol":1,"action":"prepare","task_id":"0123456789abcdef0123456789abcdef","target_digest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`
	if _, err := DecodeProtocolRequest([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		`{"protocol":1,"action":"prepare","task_id":"../../etc/passwd","target_digest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`,
		`{"protocol":1,"action":"prepare","task_id":"0123456789abcdef0123456789abcdef","target_digest":"x"}`,
		`{"protocol":1,"action":"apply","task_id":"0123456789abcdef0123456789abcdef","confirmation":true,"command":"rm -rf /"}`,
		`{"protocol":1,"action":"status","target_digest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`,
	} {
		if _, err := DecodeProtocolRequest([]byte(input)); err == nil {
			t.Fatalf("unsafe protocol request accepted: %s", input)
		}
	}
}

func TestProtocolApplyRequiresExplicitConfirmation(t *testing.T) {
	base := `{"protocol":1,"action":"apply","task_id":"0123456789abcdef0123456789abcdef"}`
	if _, err := DecodeProtocolRequest([]byte(base)); err == nil {
		t.Fatal("apply without confirmation accepted")
	}
	confirmed := `{"protocol":1,"action":"apply","task_id":"0123456789abcdef0123456789abcdef","confirmation":true}`
	if _, err := DecodeProtocolRequest([]byte(confirmed)); err != nil {
		t.Fatal(err)
	}
	confirmedWithDigest := `{"protocol":1,"action":"apply","task_id":"0123456789abcdef0123456789abcdef","target_digest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","confirmation":true}`
	if _, err := DecodeProtocolRequest([]byte(confirmedWithDigest)); err != nil {
		t.Fatal(err)
	}
	invalidDigest := `{"protocol":1,"action":"apply","task_id":"0123456789abcdef0123456789abcdef","target_digest":"x","confirmation":true}`
	if _, err := DecodeProtocolRequest([]byte(invalidDigest)); err == nil {
		t.Fatal("apply with invalid optional digest accepted")
	}
}

// Automatic upgrades were removed end to end; the wire protocol must not keep
// accepting their actions, otherwise an old business process could still hand
// the helper a policy after the feature is gone.
func TestProtocolRejectsRemovedAutomaticUpgradeActions(t *testing.T) {
	for _, action := range []string{"policy_get", "policy_set"} {
		if _, err := DecodeProtocolRequest([]byte(`{"protocol":1,"action":"` + action + `"}`)); err == nil {
			t.Fatalf("removed action %q accepted", action)
		}
	}
}

func TestProtocolControlRequestsAllowOnlyBoundedFields(t *testing.T) {
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	taskID := "0123456789abcdef0123456789abcdef"
	valid := []string{
		`{"protocol":1,"action":"status"}`,
		`{"protocol":1,"action":"check"}`,
		`{"protocol":1,"action":"task_list"}`,
		`{"protocol":1,"action":"events","event_limit":100}`,
		`{"protocol":1,"action":"import","inbox_token":"0123456789abcdef0123456789abcdef"}`,
		`{"protocol":1,"action":"get_import","import_id":"0123456789abcdef0123456789abcdef"}`,
		`{"protocol":1,"action":"create_task","create":{"idempotency_key":"request-1","actor":"admin","target_digest":"` + digest + `"}}`,
		`{"protocol":1,"action":"get_task","task_id":"` + taskID + `"}`,
		`{"protocol":1,"action":"prepare_task","prepare":{"idempotency_key":"request-1","actor":"admin","target_digest":"` + digest + `","inbox_token":"0123456789abcdef0123456789abcdef"}}`,
	}
	for _, input := range valid {
		if _, err := DecodeProtocolRequest([]byte(input)); err != nil {
			t.Fatalf("valid control request rejected: %s: %v", input, err)
		}
	}
	invalid := []string{
		`{"protocol":1,"action":"events","event_limit":101}`,
		`{"protocol":1,"action":"import","inbox_token":"/var/lib/uvp/staging/release.tar"}`,
		`{"protocol":1,"action":"import","inbox_token":"0123456789abcdef0123456789abcdef","import_id":"0123456789abcdef0123456789abcdef"}`,
		`{"protocol":1,"action":"get_import"}`,
		`{"protocol":1,"action":"get_import","import_id":"../../etc/passwd"}`,
		`{"protocol":1,"action":"get_import","import_id":"0123456789abcdef0123456789abcdef","inbox_token":"0123456789abcdef0123456789abcdef"}`,
		`{"protocol":1,"action":"create_task","create":{"idempotency_key":"request-1","actor":"admin","target_digest":"` + digest + `","staging_path":"/var/lib/uvp/staging/release.tar"}}`,
		`{"protocol":1,"action":"prepare_task","prepare":{"idempotency_key":"request-1","actor":"admin","target_digest":"` + digest + `","staging_path":"/var/lib/uvp/staging/release.tar","source":"offline"}}`,
		`{"protocol":1,"action":"get_task","task_id":"` + taskID + `","target_digest":"` + digest + `"}`,
		`{"protocol":1,"action":"task_list","command":"systemctl stop uvp"}`,
	}
	for _, input := range invalid {
		if _, err := DecodeProtocolRequest([]byte(input)); err == nil {
			t.Fatalf("unsafe control request accepted: %s", input)
		}
	}
}
