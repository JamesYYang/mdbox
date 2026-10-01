// Package mcp 把文档仓库暴露成 MCP 工具，让任意支持 MCP 的 agent
// 都能读取、检索、写入文档——这是 agent 与知识库之间的双向通道。
package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"mdbox/internal/store"
)

// New 构建 MCP server，名字与版本会体现在握手响应里。
func New(st *store.Store, name, version string) *server.MCPServer {
	s := server.NewMCPServer(name, version)

	s.AddTool(mcp.NewTool("list_docs",
		mcp.WithDescription("列出知识库中的文档（不含正文）。可按标签或分类过滤，返回 id/标题/标签/更新时间"),
		mcp.WithString("tag", mcp.Description("按标签过滤，为空表示不过滤")),
		mcp.WithString("category", mcp.Description("按分类过滤")),
		mcp.WithNumber("limit", mcp.Description("最多返回篇数，默认 20")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		docs, err := st.List(store.ListFilter{
			Tag:      req.GetString("tag", ""),
			Category: req.GetString("category", ""),
			Limit:    req.GetInt("limit", 20),
		})
		if err != nil {
			return toolErr(err)
		}
		return toolJSON(docs)
	})

	s.AddTool(mcp.NewTool("search_docs",
		mcp.WithDescription("全文检索知识库，在标题、标签和正文中匹配关键词，返回命中的文档摘要"),
		mcp.WithString("query", mcp.Description("检索关键词"), mcp.Required()),
		mcp.WithNumber("limit", mcp.Description("最多返回篇数，默认 10")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q, err := req.RequireString("query")
		if err != nil {
			return toolErr(err)
		}
		docs, err := st.List(store.ListFilter{Query: q, Limit: req.GetInt("limit", 10)})
		if err != nil {
			return toolErr(err)
		}
		return toolJSON(docs)
	})

	s.AddTool(mcp.NewTool("read_doc",
		mcp.WithDescription("按 id 读取一篇文档的完整正文。id 可从 list_docs 或 search_docs 获得"),
		mcp.WithString("id", mcp.Description("文档 id"), mcp.Required()),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return toolErr(err)
		}
		d, err := st.Get(id)
		if err != nil {
			return toolErr(err)
		}
		return mcp.NewToolResultText(d.Content), nil
	})

	s.AddTool(mcp.NewTool("write_doc",
		mcp.WithDescription("把一篇新的 Markdown 文档写入知识库，返回文档 id。适合在讨论结束后归档结论"),
		mcp.WithString("title", mcp.Description("文档标题"), mcp.Required()),
		mcp.WithString("content", mcp.Description("Markdown 正文"), mcp.Required()),
		mcp.WithString("tags", mcp.Description("标签，英文逗号分隔，如：架构,决策")),
		mcp.WithString("category", mcp.Description("分类")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		title, err := req.RequireString("title")
		if err != nil {
			return toolErr(err)
		}
		content, err := req.RequireString("content")
		if err != nil {
			return toolErr(err)
		}
		d, err := st.Create(store.CreateInput{
			Title:    title,
			Content:  content,
			Tags:     splitTags(req.GetString("tags", "")),
			Category: req.GetString("category", ""),
			Source:   "agent",
		})
		if err != nil {
			return toolErr(err)
		}
		return toolJSON(map[string]any{"id": d.ID, "title": d.Title, "message": "已写入知识库"})
	})

	s.AddTool(mcp.NewTool("update_doc",
		mcp.WithDescription("更新已有文档的正文、标题或标签，字段留空表示不修改"),
		mcp.WithString("id", mcp.Description("文档 id"), mcp.Required()),
		mcp.WithString("title", mcp.Description("新标题")),
		mcp.WithString("content", mcp.Description("新的 Markdown 正文")),
		mcp.WithString("tags", mcp.Description("新标签，英文逗号分隔")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return toolErr(err)
		}
		in := store.UpdateInput{}
		if v := req.GetString("title", ""); v != "" {
			in.Title = &v
		}
		if v := req.GetString("content", ""); v != "" {
			in.Content = &v
		}
		if v := req.GetString("tags", ""); v != "" {
			in.Tags = splitTags(v)
		}
		d, err := st.Update(id, in)
		if err != nil {
			return toolErr(err)
		}
		return toolJSON(map[string]any{"id": d.ID, "title": d.Title, "message": "已更新"})
	})

	s.AddTool(mcp.NewTool("list_tags",
		mcp.WithDescription("列出知识库中所有标签及其文档数，用于决定检索方向"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return toolJSON(st.Tags())
	})

	s.AddTool(mcp.NewTool("archive_doc",
		mcp.WithDescription("把文档移入归档区（不删除文件），适合处理已过时的内容"),
		mcp.WithString("id", mcp.Description("文档 id"), mcp.Required()),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return toolErr(err)
		}
		if _, err := st.Archive(id); err != nil {
			return toolErr(err)
		}
		return toolJSON(map[string]any{"id": id, "message": "已归档"})
	})

	return s
}

func toolJSON(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return toolErr(err)
	}
	return mcp.NewToolResultText(string(b)), nil
}

func toolErr(err error) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText("error: " + err.Error()), nil
}

func splitTags(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
