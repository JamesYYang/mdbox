// Package config 读取 mdbox 的配置文件。
//
// 配置里的 admin 只用于首次启动时在 users.yaml 里初始化第一个用户，
// 之后 admin 与普通注册用户没有区别（改这里的密码不会影响已存在的 admin）。
// secret 用于分享链接的签名——只要 secret 不变，同一篇文档的分享地址就始终相同；
// 一旦更换 secret，此前所有分享链接立即失效。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultPath 是默认的配置文件路径。
const DefaultPath = "config.yaml"

// 默认初始账号，仅在配置文件缺失或字段为空时使用。
const (
	defaultUsername = "admin"
	defaultPassword = "mdbox@111!!!"
)

// Admin 是初始化用的账号。
type Admin struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// Config 是 mdbox 的全部配置。
type Config struct {
	Admin  Admin  `yaml:"admin"`
	Secret string `yaml:"secret"`
	// AllowRegister 控制是否开放注册，缺省为开放。
	AllowRegister *bool `yaml:"allow_register"`
}

// RegisterOpen 返回当前是否允许注册。
func (c *Config) RegisterOpen() bool { return c.AllowRegister == nil || *c.AllowRegister }

// Load 读取配置文件；文件不存在时写入一份带默认值的配置。
// 已有的配置里若缺少必填字段，会补齐后回写。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// 文件不存在：用默认值生成一份
		cfg := &Config{}
		_ = normalize(cfg)
		if err := save(path, cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if normalize(&cfg) {
		if err := save(path, &cfg); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

// normalize 补齐缺失字段（初始账号、secret、allow_register），返回是否有改动。
func normalize(cfg *Config) bool {
	changed := false
	if cfg.Admin.Username == "" {
		cfg.Admin.Username = defaultUsername
		changed = true
	}
	if cfg.Admin.Password == "" {
		cfg.Admin.Password = defaultPassword
		changed = true
	}
	if cfg.Secret == "" {
		cfg.Secret = randomHex(32)
		changed = true
	}
	if cfg.AllowRegister == nil {
		open := true
		cfg.AllowRegister = &open
		changed = true
	}
	return changed
}

func save(path string, cfg *Config) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	header := "# mdbox 配置文件\n" +
		"# admin：首次启动时写入 users.yaml 的初始账号；之后与普通用户无区别。\n" +
		"# secret：分享链接签名密钥，修改后已分享的链接会全部失效。\n" +
		"# allow_register：是否开放用户注册（true/false）。\n" +
		"# agent 的 token 不在这里配置：每个用户注册时自动生成，见 users.yaml / Web 的 MCP 接入弹窗。\n"
	return os.WriteFile(path, append([]byte(header), b...), 0o600)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
