package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pagingFixtureTasks(count int) []TaskRecord {
	records := make([]TaskRecord, 0, count)
	for i := 0; i < count; i++ {
		records = append(records, TaskRecord{
			TaskID:       fmt.Sprintf("%032d", i),
			Version:      fmt.Sprintf("1.1.%d", i),
			Phase:        PhaseSucceeded,
			ReleaseNotes: strings.Repeat("说明", 100),
			UpdatedAt:    fmt.Sprintf("2026-11-%02dT00:00:00Z", (i%28)+1),
		})
	}
	return records
}

func TestRecentTaskSummariesKeepsBoundedWindowWithoutReleaseNotes(t *testing.T) {
	records := pagingFixtureTasks(45)
	summaries := RecentTaskSummaries(records, StatusTaskLimit)

	if len(summaries) != StatusTaskLimit {
		t.Fatalf("summary count = %d, want %d", len(summaries), StatusTaskLimit)
	}
	for _, summary := range summaries {
		if summary.ReleaseNotes != "" {
			t.Fatalf("状态摘要不应携带更新说明: %q", summary.ReleaseNotes)
		}
	}
	for i := 1; i < len(summaries); i++ {
		if summaries[i-1].UpdatedAt < summaries[i].UpdatedAt {
			t.Fatalf("摘要未按更新时间倒序: %s < %s", summaries[i-1].UpdatedAt, summaries[i].UpdatedAt)
		}
	}
	// 原切片不能被就地修改，否则后续分页会丢说明。
	if records[0].ReleaseNotes == "" {
		t.Fatal("RecentTaskSummaries 改写了调用方的记录")
	}
}

func TestPageTasksSlicesByUpdatedAtAndReportsTotal(t *testing.T) {
	records := pagingFixtureTasks(23)

	first, total := PageTasks(records, 1, 10)
	if total != 23 || len(first) != 10 {
		t.Fatalf("第一页 = %d 条 / total %d", len(first), total)
	}
	if first[0].UpdatedAt < first[9].UpdatedAt {
		t.Fatal("分页结果未按更新时间倒序")
	}
	last, _ := PageTasks(records, 3, 10)
	if len(last) != 3 {
		t.Fatalf("末页 = %d 条, want 3", len(last))
	}
	beyond, beyondTotal := PageTasks(records, 9, 10)
	if len(beyond) != 0 || beyondTotal != 23 {
		t.Fatalf("越界页 = %d 条 / total %d", len(beyond), beyondTotal)
	}
}

func TestPageTasksClampsPageSize(t *testing.T) {
	records := pagingFixtureTasks(120)
	paged, _ := PageTasks(records, 1, MaxTaskPageSize+500)
	if len(paged) != MaxTaskPageSize {
		t.Fatalf("每页条数 = %d, want %d", len(paged), MaxTaskPageSize)
	}
	fallback, _ := PageTasks(records, 1, 0)
	if len(fallback) != 10 {
		t.Fatalf("缺省每页条数 = %d, want 10", len(fallback))
	}
}

func TestProtocolPagingFieldsAreOnlyAcceptedForTaskList(t *testing.T) {
	list := ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionTaskList, Page: 2, PageSize: 20}
	if err := list.Validate(); err != nil {
		t.Fatalf("task_list 分页请求应被接受: %v", err)
	}
	status := ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionStatus, Page: 1}
	if err := status.Validate(); err == nil {
		t.Fatal("status 带分页字段必须被拒绝（老助手会按未知字段拒收）")
	}
	tooBig := ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionTaskList, PageSize: MaxTaskPageSize + 1}
	if err := tooBig.Validate(); err == nil {
		t.Fatal("超过每页上限必须被拒绝")
	}
	negative := ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionTaskList, Page: -1}
	if err := negative.Validate(); err == nil {
		t.Fatal("负页码必须被拒绝")
	}
}

func TestDaemonTaskListServesSinglePageWithTotal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	records := pagingFixtureTasks(25)
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tasks.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenTaskStore(root)
	if err != nil {
		t.Fatal(err)
	}
	control := &ControlService{Store: store}
	server := &UnixServer{Store: store, Control: control}

	response := server.dispatch(context.Background(), ProtocolRequest{
		Protocol: UpdaterProtocolVersion, Action: ActionTaskList, Page: 2, PageSize: 10,
	})
	if !response.OK {
		t.Fatalf("task_list 失败: %s", response.Error)
	}
	if response.TaskTotal != 25 {
		t.Fatalf("total = %d, want 25", response.TaskTotal)
	}
	if len(response.Tasks) != 10 {
		t.Fatalf("第二页条数 = %d, want 10", len(response.Tasks))
	}
	if response.Tasks[0].ReleaseNotes == "" {
		t.Fatal("历史分页必须带更新说明，否则「更新说明」按钮会空白")
	}
}
