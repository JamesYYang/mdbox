// Package i18n 提供后端文案的中英文目录与请求语言解析。
// 只覆盖浏览器可见的错误文案；CLI / 日志等部署者视角的字符串不在此列。
package i18n

import "strings"

// Lang 是受支持的语言代码。
type Lang string

const (
	ZH Lang = "zh"
	EN Lang = "en"

	// Default 是无法识别语言时的兜底（面向英文推广）。
	Default = EN
)

// Key 是文案键，命名形如 "<域>.<项>"。前后端共用同一套键名。
type Key string

const (
	KeyUnauthorized      Key = "error.unauthorized"
	KeyInternal          Key = "error.internal"
	KeyBadJSON           Key = "error.bad_json"
	KeyBadForm           Key = "error.bad_form"
	KeyBodyTooLarge      Key = "error.body_too_large"
	KeyBadCredentials    Key = "auth.bad_credentials"
	KeyRegisterClosed    Key = "auth.register_closed"
	KeyRegisterLimited   Key = "auth.register_limited"
	KeyShareGone         Key = "share.gone"
	KeyBadShareMode      Key = "share.bad_mode"
	KeyUserNameInvalid   Key = "user.name_invalid"
	KeyUserPasswordWeak  Key = "user.password_weak"
	KeyUserExists        Key = "user.exists"
	KeyUserNotFound      Key = "user.not_found"
	KeyUserPasswordWrong Key = "user.password_wrong"
	KeyDocNotFound       Key = "doc.not_found"
	KeyDocTooLarge       Key = "doc.too_large"
	KeyQuotaExceeded     Key = "doc.quota_exceeded"
	KeyNotText           Key = "doc.not_text"
	KeyImageTooLarge     Key = "image.too_large"
	KeyUnsupportedImage  Key = "image.unsupported"
)

var catalog = map[Lang]map[Key]string{
	EN: {
		KeyUnauthorized:      "Sign-in required",
		KeyInternal:          "Internal server error",
		KeyBadJSON:           "Invalid request",
		KeyBadForm:           "Invalid request",
		KeyBodyTooLarge:      "Request body too large",
		KeyBadCredentials:    "Incorrect username or password",
		KeyRegisterClosed:    "Registration is closed",
		KeyRegisterLimited:   "Too many attempts, please try again later",
		KeyShareGone:         "This share link is invalid or has expired",
		KeyBadShareMode:      "Unknown share mode",
		KeyUserNameInvalid:   "Username may contain only lowercase letters, digits, underscores and hyphens, 3-32 characters",
		KeyUserPasswordWeak:  "Password must be 8-72 characters",
		KeyUserExists:        "Username is already taken",
		KeyUserNotFound:      "User not found",
		KeyUserPasswordWrong: "Current password is incorrect",
		KeyDocNotFound:       "Document not found",
		KeyDocTooLarge:       "Document is too large (2 MB limit per document)",
		KeyQuotaExceeded:     "Storage quota reached (200 MB per user, documents and images) — delete or archive something first",
		KeyNotText:           "Only UTF-8 text files are supported",
		KeyImageTooLarge:     "Image is too large (5 MB limit per image)",
		KeyUnsupportedImage:  "Only PNG, JPEG, GIF and WebP images are supported",
	},
	ZH: {
		KeyUnauthorized:      "需要登录",
		KeyInternal:          "服务器内部错误",
		KeyBadJSON:           "请求格式错误",
		KeyBadForm:           "请求格式错误",
		KeyBodyTooLarge:      "请求体过大",
		KeyBadCredentials:    "用户名或密码错误",
		KeyRegisterClosed:    "注册已关闭",
		KeyRegisterLimited:   "注册过于频繁，请稍后再试",
		KeyShareGone:         "分享不存在或已失效",
		KeyBadShareMode:      "未知的分享模式",
		KeyUserNameInvalid:   "用户名只能包含小写字母、数字、下划线和连字符，长度 3-32",
		KeyUserPasswordWeak:  "密码长度需要在 8-72 个字符之间",
		KeyUserExists:        "用户名已被占用",
		KeyUserNotFound:      "用户不存在",
		KeyUserPasswordWrong: "旧密码不正确",
		KeyDocNotFound:       "文档不存在",
		KeyDocTooLarge:       "文档过大，单篇上限 2MB",
		KeyQuotaExceeded:     "存储配额已满（每人 200MB，含文档与图片），请先删除或归档一些内容",
		KeyNotText:           "只支持 UTF-8 文本文件",
		KeyImageTooLarge:     "图片过大，单张上限 5MB",
		KeyUnsupportedImage:  "只支持 PNG、JPEG、GIF、WebP 图片",
	},
}

// T 返回给定语言下键对应的文案；缺翻译回退英文，再回退键名本身。
func T(l Lang, k Key) string {
	if s, ok := catalog[l][k]; ok {
		return s
	}
	if s, ok := catalog[Default][k]; ok {
		return s
	}
	return string(k)
}

// Known 判断 s 是否为已知文案键（供错误输出决定是否翻译）。
func Known(s string) bool {
	_, ok := catalog[Default][Key(s)]
	return ok
}

// FromAcceptLanguage 取 Accept-Language 中第一个可识别语言：前缀 zh → 中文，
// 前缀 en → 英文，都不命中（如 fr）继续往后找；全部未命中则默认英文。
func FromAcceptLanguage(h string) Lang {
	for _, part := range strings.Split(h, ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		tag = strings.ToLower(tag)
		switch {
		case strings.HasPrefix(tag, "zh"):
			return ZH
		case strings.HasPrefix(tag, "en"):
			return EN
		}
	}
	return Default
}
