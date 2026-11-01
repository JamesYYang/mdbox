package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("建仓库失败: %v", err)
	}
	return st
}

func ptr[T any](v T) *T { return &v }

// TestCreateRejectsNonText 二进制改名成 .md 上传是真实发生过的滥用方式，必须在入库前挡下。
func TestCreateRejectsNonText(t *testing.T) {
	cases := []struct{ name, content string }{
		{"PNG 头", "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00\xff", 64)},
		{"UTF-8 非法字节序列", "正常开头\xc3\x28结尾"},
		{"含 NUL 字节", "看起来正常\x00但带空字节"},
	}
	for _, c := range cases {
		st := newTestStore(t)
		if _, err := st.Create(CreateInput{Title: "x", Content: c.content}); !errors.Is(err, ErrNotText) {
			t.Errorf("%s: err = %v, want ErrNotText", c.name, err)
		}
		if docs, _ := st.List(ListFilter{Status: "all"}); len(docs) != 0 {
			t.Errorf("%s: 被拒绝后仍写入了 %d 篇文档", c.name, len(docs))
		}
	}
}

func TestCreateAcceptsUTF8Text(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Create(CreateInput{Title: "标题", Content: "# 你好 👋\n\n正文 typographic — dash\n"}); err != nil {
		t.Fatalf("正常 UTF-8 文本被拒: %v", err)
	}
}

func TestCreateEnforcesDocSizeLimit(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Create(CreateInput{Content: strings.Repeat("a", MaxDocBytes+1)}); !errors.Is(err, ErrDocTooLarge) {
		t.Errorf("超单篇上限: err = %v, want ErrDocTooLarge", err)
	}
	// 正好等于上限应当放行（边界不能多扣一个字节）
	if _, err := st.Create(CreateInput{Content: strings.Repeat("a", MaxDocBytes)}); err != nil {
		t.Errorf("正好等于上限被拒: %v", err)
	}
}

func TestCreateEnforcesQuota(t *testing.T) {
	st := newTestStore(t)
	st.quota = 10

	if _, err := st.Create(CreateInput{Content: strings.Repeat("a", 11)}); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("超配额: err = %v, want ErrQuotaExceeded", err)
	}
	if _, err := st.Create(CreateInput{Content: strings.Repeat("a", 10)}); err != nil {
		t.Fatalf("正好用满配额被拒: %v", err)
	}
	if _, err := st.Create(CreateInput{Content: "a"}); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("已用满后再写: err = %v, want ErrQuotaExceeded", err)
	}
}

// TestQuotaCountsArchived 归档只是软删除，文件还在磁盘上，因此仍然占配额。
func TestQuotaCountsArchived(t *testing.T) {
	st := newTestStore(t)
	st.quota = 10
	d, err := st.Create(CreateInput{Content: strings.Repeat("a", 10)})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, err := st.Archive(d.ID); err != nil {
		t.Fatalf("归档失败: %v", err)
	}
	if _, err := st.Create(CreateInput{Content: "a"}); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("归档后腾出的空间不该被释放: err = %v, want ErrQuotaExceeded", err)
	}
}

// TestUpdateReplacesOwnSizeWithinQuota 更新是覆盖同一篇文档，配额里要先扣掉它自己的旧体积，
// 否则「改一篇已经用满配额的文档」会被误判成超额。
func TestUpdateReplacesOwnSizeWithinQuota(t *testing.T) {
	st := newTestStore(t)
	st.quota = 10
	d, err := st.Create(CreateInput{Content: strings.Repeat("a", 10)})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	same := strings.Repeat("b", 10)
	if _, err := st.Update(d.ID, UpdateInput{Content: &same}); err != nil {
		t.Errorf("同体积替换被误拒: %v", err)
	}
	bigger := strings.Repeat("b", 11)
	if _, err := st.Update(d.ID, UpdateInput{Content: &bigger}); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("替换成更大内容: err = %v, want ErrQuotaExceeded", err)
	}
}

// TestUpdateRejectedLeavesDocUntouched 校验失败时不能留下「内存索引改了、磁盘没改」的半截状态。
func TestUpdateRejectedLeavesDocUntouched(t *testing.T) {
	st := newTestStore(t)
	d, err := st.Create(CreateInput{Title: "原标题", Content: "原正文"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	bad := "二进制\x00内容"
	if _, err := st.Update(d.ID, UpdateInput{Title: ptr("新标题"), Content: &bad}); !errors.Is(err, ErrNotText) {
		t.Fatalf("err = %v, want ErrNotText", err)
	}
	got, err := st.Get(d.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.Title != "原标题" {
		t.Errorf("标题被污染: %q, want 原标题", got.Title)
	}
	if !strings.Contains(got.Content, "原正文") {
		t.Errorf("正文被污染: %q", got.Content)
	}

	// 只改元数据的更新不涉及正文，应当照常放行
	if _, err := st.Update(d.ID, UpdateInput{Title: ptr("新标题")}); err != nil {
		t.Errorf("纯元数据更新被拒: %v", err)
	}
}

func TestOverviewCountsNewAndAgentSources(t *testing.T) {
	st := newTestStore(t)
	for _, src := range []string{"agent", "codebuddy", "web", ""} {
		if _, err := st.Create(CreateInput{Title: src, Content: "x", Source: src}); err != nil {
			t.Fatalf("创建失败: %v", err)
		}
	}

	o := st.Overview(time.Now().Add(-time.Minute))
	if o.New != 4 {
		t.Errorf("new = %d, want 4", o.New)
	}
	if o.Agent != 2 {
		t.Errorf("agent = %d, want 2（agent + codebuddy）", o.Agent)
	}
	if o.Updated != 0 || o.Archived != 0 {
		t.Errorf("updated/archived = %d/%d, want 0/0", o.Updated, o.Archived)
	}
}

func TestOverviewCountsUpdateWithoutDoubleCountingNew(t *testing.T) {
	st := newTestStore(t)
	d, err := st.Create(CreateInput{Title: "t", Content: "x", Source: "web"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	since := time.Now()
	time.Sleep(20 * time.Millisecond)
	if _, err := st.Update(d.ID, UpdateInput{Title: ptr("t2")}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}

	o := st.Overview(since)
	if o.Updated != 1 || o.New != 0 {
		t.Errorf("overview = %+v, want updated=1 new=0", o)
	}
}

func TestOverviewCountsArchived(t *testing.T) {
	st := newTestStore(t)
	d, err := st.Create(CreateInput{Title: "t", Content: "x", Source: "web"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	since := time.Now()
	time.Sleep(20 * time.Millisecond)
	if _, err := st.Archive(d.ID); err != nil {
		t.Fatalf("归档失败: %v", err)
	}

	o := st.Overview(since)
	if o.Archived != 1 || o.New != 0 || o.Updated != 0 {
		t.Errorf("overview = %+v, want archived=1 new=0 updated=0", o)
	}
}

func TestOverviewIgnoresActivityOutsideWindow(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Create(CreateInput{Title: "t", Content: "x", Source: "agent"}); err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	if o := st.Overview(time.Now().Add(time.Hour)); o != (Overview{}) {
		t.Errorf("窗口外 overview = %+v, want 全 0", o)
	}
}

// TestArchiveUnarchiveRoundTrip 归档是移到 archive/，恢复必须原样搬回 docs/，
// 正文与标题都不能丢——这正是「误归档只能真删重写」的痛点所在。
func TestArchiveUnarchiveRoundTrip(t *testing.T) {
	st := newTestStore(t)
	d, err := st.Create(CreateInput{Title: "旧稿", Content: "# 旧稿\n\n正文"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	// 归档前先取一份正文，恢复后精确比对，避免「恢复成空文件」也能过。
	want, err := st.Get(d.ID)
	if err != nil {
		t.Fatalf("归档前读取失败: %v", err)
	}
	if _, err := st.Archive(d.ID); err != nil {
		t.Fatalf("归档失败: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	got, err := st.Unarchive(d.ID)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("status = %q, want active", got.Status)
	}
	if !got.Updated.After(d.Updated) {
		t.Errorf("updated 未前进: %v -> %v", d.Updated, got.Updated)
	}
	if got.Title != "旧稿" {
		t.Errorf("title = %q, want 旧稿", got.Title)
	}
	if _, err := os.Stat(filepath.Join(st.root, "docs", d.ID+".md")); err != nil {
		t.Errorf("docs/ 下应存在该文件: %v", err)
	}
	if _, err := os.Stat(filepath.Join(st.root, "archive", d.ID+".md")); !os.IsNotExist(err) {
		t.Errorf("archive/ 下不应残留文件: err = %v", err)
	}
	full, err := st.Get(d.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if full.Content != want.Content {
		t.Errorf("正文 = %q, want %q", full.Content, want.Content)
	}
	active, err := st.List(ListFilter{Status: "active"})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(active) != 1 || active[0].ID != d.ID {
		t.Errorf("活动列表 = %+v, want 仅 %s", active, d.ID)
	}
}

// TestUnarchiveActiveDocIsNoop 对活动文档点「恢复」不该改任何东西，尤其不该刷新 updated。
func TestUnarchiveActiveDocIsNoop(t *testing.T) {
	st := newTestStore(t)
	d, err := st.Create(CreateInput{Title: "t", Content: "x"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	got, err := st.Unarchive(d.ID)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if !got.Updated.Equal(d.Updated) {
		t.Errorf("updated 被改写: %v -> %v", d.Updated, got.Updated)
	}
}

func TestUnarchiveUnknownID(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Unarchive("20260101-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestUnarchiveDocInDocsWithArchivedFrontmatter 用户手工把文件从 archive/ 挪回 docs/ 时，
// frontmatter 里仍是 archived，于是 path 在 docs/ 而 Status 是 archived。恢复必须就地
// 重写成 active，不能「写到同一个路径再把刚写好的文件删掉」，那等于静默丢文档。
func TestUnarchiveDocInDocsWithArchivedFrontmatter(t *testing.T) {
	st := newTestStore(t)
	d, err := st.Create(CreateInput{Title: "手改稿", Content: "# 手改稿\n\n正文"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	path := filepath.Join(st.root, "docs", d.ID+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	edited := strings.Replace(string(raw), "status: active", "status: archived", 1)
	if edited == string(raw) {
		t.Fatalf("frontmatter 里没找到 status: active，原始内容:\n%s", raw)
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatalf("写回失败: %v", err)
	}

	// 重建仓库，让索引按磁盘内容重建：此时 path 在 docs/，Status 却是 archived。
	st, err = New(st.root)
	if err != nil {
		t.Fatalf("重建仓库失败: %v", err)
	}
	got, err := st.Unarchive(d.ID)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("status = %q, want active", got.Status)
	}
	full, err := st.Get(d.ID)
	if err != nil {
		t.Fatalf("恢复后文件应仍然存在: %v", err)
	}
	if full.Title != "手改稿" {
		t.Errorf("title = %q, want 手改稿", full.Title)
	}
	if !strings.Contains(full.Content, "正文") {
		t.Errorf("正文丢失: %q", full.Content)
	}
}

// TestUnarchiveMissingSourceFileErrors 外部删掉 archive/ 下的文件而内存索引还陈旧时，
// 恢复不能把空正文写进 docs/ 并当成功返回——那是静默毁掉内容。
func TestUnarchiveMissingSourceFileErrors(t *testing.T) {
	st := newTestStore(t)
	d, err := st.Create(CreateInput{Title: "旧稿", Content: "# 旧稿\n\n正文"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, err := st.Archive(d.ID); err != nil {
		t.Fatalf("归档失败: %v", err)
	}
	if err := os.Remove(filepath.Join(st.root, "archive", d.ID+".md")); err != nil {
		t.Fatalf("删源文件失败: %v", err)
	}

	if _, err := st.Unarchive(d.ID); err == nil {
		t.Fatal("源文件缺失时 Unarchive 应返回错误，实际返回 nil")
	}
	if _, err := os.Stat(filepath.Join(st.root, "docs", d.ID+".md")); !os.IsNotExist(err) {
		t.Errorf("docs/ 下不应被写出空文档: err = %v", err)
	}
}

// TestArchiveIsIdempotentOnArchivedDoc 对已归档文档再归档一次（重复点归档、
// agent 对旧 id 再调 archive_doc）时，目标 dst 就是源文件本身。旧实现「写完再删源」
// 删掉的正是刚写好的文件，文档从磁盘上消失。归档必须幂等。
func TestArchiveIsIdempotentOnArchivedDoc(t *testing.T) {
	st := newTestStore(t)
	d, err := st.Create(CreateInput{Title: "旧稿", Content: "# 旧稿\n\n正文"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, err := st.Archive(d.ID); err != nil {
		t.Fatalf("首次归档失败: %v", err)
	}
	again, err := st.Archive(d.ID)
	if err != nil {
		t.Fatalf("重复归档失败: %v", err)
	}
	if again.Status != "archived" {
		t.Errorf("status = %q, want archived", again.Status)
	}
	if _, err := os.Stat(filepath.Join(st.root, "archive", d.ID+".md")); err != nil {
		t.Fatalf("archive/ 下的文件被删掉了: %v", err)
	}
	full, err := st.Get(d.ID)
	if err != nil {
		t.Fatalf("重复归档后读取失败: %v", err)
	}
	if full.Status != "archived" {
		t.Errorf("Get 返回 status = %q, want archived", full.Status)
	}
	if !strings.Contains(full.Content, "正文") {
		t.Errorf("正文丢失: %q", full.Content)
	}
}
