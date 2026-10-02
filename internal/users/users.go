// Package users 管理多用户：注册表保存在单个 users.yaml 里（不引入数据库），
// 每个用户的文档在 {data}/users/{username}/ 下，对应一个独立的 store.Store。
//
// users.yaml 含密码哈希与 MCP token，所以放在 data/ 之外，避免被 git 备份带走。
package users

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"

	"mdbox/internal/store"
)

var (
	ErrInvalidName  = errors.New("用户名只能包含小写字母、数字、下划线和连字符，长度 3-32")
	ErrWeakPassword = errors.New("密码长度需要在 8-72 个字符之间")
	ErrExists       = errors.New("用户名已被占用")
	ErrNotFound     = errors.New("用户不存在")
)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)

// 目录名直接取自用户名，Windows 保留设备名不能当目录名。
var reserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true,
}

// User 是 users.yaml 里的一条记录。
type User struct {
	Username     string    `yaml:"username"`
	PasswordHash string    `yaml:"password_hash"`
	Token        string    `yaml:"token"`
	Created      time.Time `yaml:"created"`
}

// dummyHash 用于用户不存在时也做一次 bcrypt 比较，避免通过耗时枚举用户名。
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("mdbox-dummy"), bcrypt.DefaultCost)

// Registry 是用户注册表，并发安全。
type Registry struct {
	path    string
	dataDir string

	mu     sync.RWMutex
	list   []User
	stores map[string]*store.Store
}

// Open 读取 users.yaml（不存在则视为空），dataDir 是文档根目录。
func Open(path, dataDir string) (*Registry, error) {
	r := &Registry{path: path, dataDir: dataDir, stores: map[string]*store.Store{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(raw, &r.list); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	return r, nil
}

// ValidName 校验用户名格式。
func ValidName(name string) error {
	if !nameRe.MatchString(name) || reserved[name] {
		return ErrInvalidName
	}
	return nil
}

// Register 注册新用户并生成 MCP token。
func (r *Registry) Register(name, password string) (*User, error) {
	if err := ValidName(name); err != nil {
		return nil, err
	}
	if n := len(password); n < 8 || n > 72 { // bcrypt 只取前 72 字节
		return nil, ErrWeakPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.indexLocked(name) >= 0 {
		return nil, ErrExists
	}
	u := User{Username: name, PasswordHash: string(hash), Token: randomHex(24), Created: time.Now()}
	r.list = append(r.list, u)
	if err := r.saveLocked(); err != nil {
		r.list = r.list[:len(r.list)-1]
		return nil, err
	}
	return &u, nil
}

// EnsureUser 在用户不存在时创建，用于用 config.yaml 里的 admin 初始化。
func (r *Registry) EnsureUser(name, password string) error {
	r.mu.RLock()
	exists := r.indexLocked(name) >= 0
	r.mu.RUnlock()
	if exists {
		return nil
	}
	_, err := r.Register(name, password)
	return err
}

// Authenticate 校验用户名密码。
func (r *Registry) Authenticate(name, password string) (*User, bool) {
	u, ok := r.Get(name)
	hash := dummyHash
	if ok {
		hash = []byte(u.PasswordHash)
	}
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))
	if !ok || err != nil {
		return nil, false
	}
	return u, true
}

// Get 按用户名查找。
func (r *Registry) Get(name string) (*User, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if i := r.indexLocked(name); i >= 0 {
		u := r.list[i]
		return &u, true
	}
	return nil, false
}

// ByToken 按 MCP/API token 查找用户。
func (r *Registry) ByToken(tok string) (*User, bool) {
	if tok == "" {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var found *User
	for i := range r.list { // 不提前退出，比较耗时与命中位置无关
		if subtle.ConstantTimeCompare([]byte(tok), []byte(r.list[i].Token)) == 1 {
			u := r.list[i]
			found = &u
		}
	}
	return found, found != nil
}

// ErrWrongPassword 表示修改密码时旧密码不正确。
var ErrWrongPassword = errors.New("旧密码不正确")

// ChangePassword 校验旧密码后改为新密码。
func (r *Registry) ChangePassword(name, oldPassword, newPassword string) error {
	if n := len(newPassword); n < 8 || n > 72 {
		return ErrWeakPassword
	}
	if _, ok := r.Authenticate(name, oldPassword); !ok {
		return ErrWrongPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.indexLocked(name)
	if i < 0 {
		return ErrNotFound
	}
	old := r.list[i].PasswordHash
	r.list[i].PasswordHash = string(hash)
	if err := r.saveLocked(); err != nil {
		r.list[i].PasswordHash = old
		return err
	}
	return nil
}

// ResetToken 为用户重新生成 token，旧 token 立即失效。
func (r *Registry) ResetToken(name string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.indexLocked(name)
	if i < 0 {
		return "", ErrNotFound
	}
	old := r.list[i].Token
	r.list[i].Token = randomHex(24)
	if err := r.saveLocked(); err != nil {
		r.list[i].Token = old
		return "", err
	}
	return r.list[i].Token, nil
}

// Store 返回已存在用户的文档仓库（懒加载）。用户不存在时不会创建目录。
func (r *Registry) Store(name string) (*store.Store, error) {
	r.mu.RLock()
	st, ok := r.stores[name]
	exists := r.indexLocked(name) >= 0
	r.mu.RUnlock()
	if ok {
		return st, nil
	}
	if !exists {
		return nil, ErrNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if st, ok := r.stores[name]; ok {
		return st, nil
	}
	st, err := store.New(filepath.Join(r.dataDir, "users", name))
	if err != nil {
		return nil, err
	}
	r.stores[name] = st
	return st, nil
}

func (r *Registry) indexLocked(name string) int {
	for i := range r.list {
		if r.list[i].Username == name {
			return i
		}
	}
	return -1
}

// saveLocked 先写临时文件再 rename，避免写一半崩溃留下损坏的 users.yaml。
func (r *Registry) saveLocked() error {
	b, err := yaml.Marshal(r.list)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(r.path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
