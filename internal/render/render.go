// Package render 把 Markdown 渲染为 HTML，供 Web 预览使用。
// 放在后端渲染的好处是前端零依赖，不依赖任何 CDN。
package render

import (
	"bytes"

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
		htmlrenderer.WithUnsafe(), // 允许内联 HTML；仅建议自用或可信内容场景
	),
)

// Markdown 渲染 Markdown 为 HTML 片段。
func Markdown(src string) (string, error) {
	var buf bytes.Buffer
	if err := engine.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}
