package metrics

import "testing"

// T1.1-U1: TxKind.String() 全 8 项有效
func TestTxKind_String_AllEight(t *testing.T) {
	cases := []struct {
		k    TxKind
		want string
	}{
		{TxRegister, "REGISTER"},
		{TxKeepalive, "KEEPALIVE"},
		{TxCatalog, "CATALOG"},
		{TxInvite, "INVITE"},
		{TxRecord, "RECORD"},
		{TxAlarm, "ALARM"},
		{TxPTZ, "PTZ"},
		{TxBye, "BYE"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("TxKind(%d).String() = %q, want %q", c.k, got, c.want)
		}
	}
}

// T1.1-U2: TxKind.LabelZh() 全 8 项有中文
func TestTxKind_LabelZh_AllNonEmpty(t *testing.T) {
	all := []TxKind{TxRegister, TxKeepalive, TxCatalog, TxInvite, TxRecord, TxAlarm, TxPTZ, TxBye}
	for _, k := range all {
		if got := k.LabelZh(); got == "" || got == "未知" {
			t.Errorf("TxKind(%d).LabelZh() = %q, want non-empty Chinese", k, got)
		}
	}
}

// T1.1-U3: unknown TxKind 处理
func TestTxKind_Unknown(t *testing.T) {
	if got := TxKind(99).String(); got != "UNKNOWN" {
		t.Errorf("unknown TxKind.String() = %q, want UNKNOWN", got)
	}
	if got := TxKind(99).LabelZh(); got != "未知" {
		t.Errorf("unknown TxKind.LabelZh() = %q, want 未知", got)
	}
}

// T1.1-U4: Direction.String()
func TestDirection_String(t *testing.T) {
	if DirIn.String() != "in" {
		t.Errorf("DirIn = %q, want in", DirIn.String())
	}
	if DirOut.String() != "out" {
		t.Errorf("DirOut = %q, want out", DirOut.String())
	}
}

// T1.1-U5: AllTxKinds 顺序固定(供 snapshot 输出 8 格用)
func TestAllTxKinds_OrderStable(t *testing.T) {
	if len(AllTxKinds) != 8 {
		t.Fatalf("AllTxKinds len = %d, want 8", len(AllTxKinds))
	}
	want := []TxKind{TxRegister, TxKeepalive, TxCatalog, TxInvite, TxRecord, TxAlarm, TxPTZ, TxBye}
	for i, k := range want {
		if AllTxKinds[i] != k {
			t.Errorf("AllTxKinds[%d] = %v, want %v", i, AllTxKinds[i], k)
		}
	}
}
