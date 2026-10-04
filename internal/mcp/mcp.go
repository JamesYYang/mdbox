// Package mcp 把文档仓库暴露成 MCP 工具，让任意支持 MCP 的 agent
// 都能读取、检索、写入文档——这是 agent 与知识库之间的双向通道。
package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"mdbox/internal/i18n"
	"mdbox/internal/store"
)

// New 构建 MCP server，名字与版本会体现在握手响应里。
func New(st *store.Store, name, version string) *server.MCPServer {
	s := server.NewMCPServer(name, version)

	s.AddTool(mcp.NewTool("list_docs",
		mcp.WithDescription("List documents in the knowledge base (without bodies). Optionally filter by tag or category; returns id/title/tags/updated time"),
		mcp.WithString("tag", mcp.Description("Filter by tag; empty means no filter")),
		mcp.WithString("category", mcp.Description("Filter by category")),
		mcp.WithNumber("limit", mcp.Description("Max number of documents to return, default 20")),
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
		mcp.WithDescription("Full-text search across titles, tags and bodies; returns matching document summaries"),
		mcp.WithString("query", mcp.Description("Search keywords"), mcp.Required()),
		mcp.WithNumber("limit", mcp.Description("Max number of documents to return, default 10")),
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
		mcp.WithDescription("Read the full body of a document by id. Get ids from list_docs or search_docs"),
		mcp.WithString("id", mcp.Description("Document id"), mcp.Required()),
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
		mcp.WithDescription("Write a new Markdown document to the knowledge base and return its id. Good for saving conclusions after a discussion"),
		mcp.WithString("title", mcp.Description("Document title"), mcp.Required()),
		mcp.WithString("content", mcp.Description("Markdown body"), mcp.Required()),
		mcp.WithString("tags", mcp.Description("Tags, comma-separated, e.g. architecture,decisions")),
		mcp.WithString("category", mcp.Description("Category")),
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
		return toolJSON(map[string]any{"id": d.ID, "title": d.Title, "message": "Written to the knowledge base"})
	})

	s.AddTool(mcp.NewTool("update_doc",
		mcp.WithDescription("Update an existing document's body, title or tags; leave a field empty to keep it unchanged"),
		mcp.WithString("id", mcp.Description("Document id"), mcp.Required()),
		mcp.WithString("title", mcp.Description("New title")),
		mcp.WithString("content", mcp.Description("New Markdown body")),
		mcp.WithString("tags", mcp.Description("New tags, comma-separated")),
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
		return toolJSON(map[string]any{"id": d.ID, "title": d.Title, "message": "Updated"})
	})

	s.AddTool(mcp.NewTool("list_tags",
		mcp.WithDescription("List all tags in the knowledge base with their document counts, to help decide what to search for"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return toolJSON(st.Tags())
	})

	s.AddTool(mcp.NewTool("archive_doc",
		mcp.WithDescription("Move a document to the archive (the file is not deleted). Good for outdated content"),
		mcp.WithString("id", mcp.Description("Document id"), mcp.Required()),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return toolErr(err)
		}
		if _, err := st.Archive(id); err != nil {
			return toolErr(err)
		}
		return toolJSON(map[string]any{"id": id, "message": "Archived"})
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

// errText 把已知文案键翻成英文（MCP 面向 agent，固定英文）。
func errText(err error) string {
	msg := err.Error()
	if i18n.Known(msg) {
		return i18n.T(i18n.EN, i18n.Key(msg))
	}
	return msg
}

func toolErr(err error) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText("error: " + errText(err)), nil
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
