package users

import (
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := Open(filepath.Join(dir, "users.yaml"), filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func TestValidName(t *testing.T) {
	for _, n := range []string{"alice", "a_b-c9", "bob123"} {
		if ValidName(n) != nil {
			t.Errorf("%q 应合法", n)
		}
	}
	for _, n := range []string{"", "ab", "Alice", "../x", "a/b", "a b", "con", "-abc", "中文名字"} {
		if ValidName(n) == nil {
			t.Errorf("%q 应非法", n)
		}
	}
}

func TestRegisterAuthenticate(t *testing.T) {
	r, _ := open(t)
	u, err := r.Register("alice", "password1")
	if err != nil || u.Token == "" {
		t.Fatalf("注册失败: %v", err)
	}
	if _, err := r.Register("alice", "password2"); err != ErrExists {
		t.Errorf("重复注册应返回 ErrExists，得到 %v", err)
	}
	if _, err := r.Register("bob", "short"); err != ErrWeakPassword {
		t.Errorf("弱密码应被拒绝，得到 %v", err)
	}
	if _, ok := r.Authenticate("alice", "password1"); !ok {
		t.Error("正确密码应通过")
	}
	if _, ok := r.Authenticate("alice", "wrong-pass"); ok {
		t.Error("错误密码不应通过")
	}
	if _, ok := r.Authenticate("nobody", "password1"); ok {
		t.Error("不存在的用户不应通过")
	}
}

func TestChangePassword(t *testing.T) {
	r, _ := open(t)
	if _, err := r.Register("alice", "password1"); err != nil {
		t.Fatal(err)
	}
	if err := r.ChangePassword("alice", "wrong-pass", "newpass123"); err != ErrWrongPassword {
		t.Errorf("旧密码错误应返回 ErrWrongPassword，得到 %v", err)
	}
	if err := r.ChangePassword("alice", "password1", "short"); err != ErrWeakPassword {
		t.Errorf("弱新密码应被拒绝，得到 %v", err)
	}
	if err := r.ChangePassword("alice", "password1", "newpass123"); err != nil {
		t.Fatalf("改密码失败: %v", err)
	}
	if _, ok := r.Authenticate("alice", "password1"); ok {
		t.Error("旧密码应失效")
	}
	if _, ok := r.Authenticate("alice", "newpass123"); !ok {
		t.Error("新密码应通过")
	}
}

func TestTokenAndReset(t *testing.T) {
	r, _ := open(t)
	u, _ := r.Register("alice", "password1")
	if got, ok := r.ByToken(u.Token); !ok || got.Username != "alice" {
		t.Fatal("token 应能查到用户")
	}
	if _, ok := r.ByToken(""); ok {
		t.Error("空 token 不应命中")
	}
	nt, err := r.ResetToken("alice")
	if err != nil || nt == u.Token {
		t.Fatalf("重置 token 失败: %v", err)
	}
	if _, ok := r.ByToken(u.Token); ok {
		t.Error("旧 token 应失效")
	}
}

func TestPersistAndStoreIsolation(t *testing.T) {
	r, dir := open(t)
	r.Register("alice", "password1")
	r.Register("bob", "password2")

	r2, err := Open(filepath.Join(dir, "users.yaml"), filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r2.Authenticate("alice", "password1"); !ok {
		t.Error("重新加载后应仍能登录")
	}

	sa, _ := r2.Store("alice")
	sb, _ := r2.Store("bob")
	d, err := sa.Create(storeInput("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Get(d.ID); err == nil {
		t.Error("bob 不应读到 alice 的文档")
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "users", "alice", "docs", d.ID+".md")); err != nil {
		t.Errorf("文档应落在用户目录下: %v", err)
	}
	if _, err := r2.Store("ghost"); err != ErrNotFound {
		t.Errorf("未注册用户不应有 Store，得到 %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "users", "ghost")); err == nil {
		t.Error("不应为未注册用户创建目录")
	}
}
