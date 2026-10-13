package store

import (
	"errors"
	"strings"
	"testing"
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
