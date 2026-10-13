// Package render 把 Markdown 渲染为 HTML，供 Web 预览使用。
// 放在后端渲染的好处是前端零依赖，不依赖任何 CDN。
package render

import (
	"bytes"
	"regexp"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	htmlrenderer "github.com/yuin/goldmark/renderer/html"
)

var engine = goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.DefinitionList),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(
		htmlrenderer.WithHardWraps(),
		htmlrenderer.WithUnsafe(), // 允许内联 HTML，安全性由下面的 policy 清洗兜底
	),
)

// policy 是文档 HTML 的白名单。文档正文由用户或 agent 写入，前端又把渲染结果直接
// 塞进 innerHTML，所以渲染产物一律视为不可信：脚本、内联事件属性（onerror 等）、
// iframe/style/base/meta、javascript: 之类的 URL 都会被剔除。
// 策略配置完成后是并发安全的，可全局共用。
var policy = newPolicy()

func newPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// GFM 任务列表的复选框是 input，UGCPolicy 默认不允许，这里放回只读的那一种。
	p.AllowAttrs("type").Matching(regexp.MustCompile(`(?i)^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	p.AllowElements("kbd") // 键盘按键标记，纯展示、无风险
	return p
}

// Markdown 渲染 Markdown 为 HTML 片段，并做白名单清洗。
func Markdown(src string) (string, error) {
	var buf bytes.Buffer
	if err := engine.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return policy.Sanitize(buf.String()), nil
}
