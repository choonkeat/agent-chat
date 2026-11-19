// mcp-apps-sample is a TEST-ONLY MCP server that implements the MCP Apps
// extension (SEP-1865, spec 2026-01-26). It is not shipped: it lives under
// testdata/, which `go build ./...` / `go vet ./...` skip and the npm package
// (files: ["bin"]) never includes.
//
// It serves one interactive page (ui://mcp-apps-sample/calendar.html): a small
// availability grid. Clicking a free cell asks the host to call book_slot; if
// the host can't relay tool calls, the page falls back to sending a chat
// message (ui/message) asking the agent to book it.
//
// Run (stdio, newline-delimited JSON-RPC):
//
//	go run ./testdata/mcp-apps-sample [--ui=auto|always|never]
//
// --ui=auto   (default, what the spec recommends) expose the page only when the
//
//	client's initialize advertises
//	capabilities.extensions["io.modelcontextprotocol/ui"].mimeTypes
//	containing "text/html;profile=mcp-app".
//
// --ui=always expose the page to every client (simulates a lenient server).
// --ui=never  never expose it (simulates a text-only server).
package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

//go:embed calendar.html
var calendarHTML string

const (
	uiExtension = "io.modelcontextprotocol/ui"
	uiMime      = "text/html;profile=mcp-app"
	pageURI     = "ui://mcp-apps-sample/calendar.html"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type server struct {
	mode     string // auto | always | never
	mu       sync.Mutex
	uiOn     bool
	people   []string
	days     []string
	bookings map[string]string // "person|day" -> "booked" | "pending"
	out      *json.Encoder
}

func newServer(mode string, out *os.File) *server {
	return &server{
		mode:   mode,
		people: []string{"Alice", "Bob", "Chen"},
		days:   []string{"Mon 23", "Tue 24", "Wed 25"},
		bookings: map[string]string{
			"Alice|Tue 24": "booked", "Alice|Wed 25": "pending",
			"Bob|Mon 23": "booked",
			"Chen|Tue 24": "pending", "Chen|Wed 25": "booked",
		},
		out: json.NewEncoder(out),
	}
}

func (s *server) reply(id json.RawMessage, result any) {
	s.out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *server) fail(id json.RawMessage, code int, msg string) {
	s.out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

// clientAdvertisesUI reports whether initialize params carry the MCP Apps
// capability with our MIME type.
func clientAdvertisesUI(params json.RawMessage) bool {
	var p struct {
		Capabilities struct {
			Extensions map[string]struct {
				MimeTypes []string `json:"mimeTypes"`
			} `json:"extensions"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(params, &p) != nil {
		return false
	}
	for _, m := range p.Capabilities.Extensions[uiExtension].MimeTypes {
		if m == uiMime {
			return true
		}
	}
	return false
}

func (s *server) grid() map[string]any {
	cells := map[string]map[string]string{}
	for _, p := range s.people {
		cells[p] = map[string]string{}
		for _, d := range s.days {
			st := s.bookings[p+"|"+d]
			if st == "" {
				st = "free"
			}
			cells[p][d] = st
		}
	}
	return map[string]any{"people": s.people, "days": s.days, "cells": cells}
}

func (s *server) gridText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-6s", "")
	for _, d := range s.days {
		fmt.Fprintf(&b, " | %-8s", d)
	}
	b.WriteString("\n")
	g := s.grid()["cells"].(map[string]map[string]string)
	for _, p := range s.people {
		fmt.Fprintf(&b, "%-6s", p)
		for _, d := range s.days {
			fmt.Fprintf(&b, " | %-8s", g[p][d])
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (s *server) tools() []any {
	show := map[string]any{
		"name":        "show_calendar",
		"title":       "Show availability calendar",
		"description": "Show who is free on which day. Returns a text table; MCP Apps hosts also get an interactive page.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	}
	book := map[string]any{
		"name":        "book_slot",
		"title":       "Book a slot",
		"description": "Mark a free slot as pending for a person on a day. Returns the updated table.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"person": map[string]any{"type": "string", "enum": s.people},
				"day":    map[string]any{"type": "string", "enum": s.days},
			},
			"required": []string{"person", "day"},
		},
	}
	if s.uiOn {
		show["_meta"] = map[string]any{"ui": map[string]any{"resourceUri": pageURI}}
		book["_meta"] = map[string]any{"ui": map[string]any{"visibility": []string{"model", "app"}}}
	}
	return []any{show, book}
}

func (s *server) callResult(text string) map[string]any {
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": text}},
		"structuredContent": s.grid(),
	}
}

func (s *server) handle(r request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case "initialize":
		switch s.mode {
		case "always":
			s.uiOn = true
		case "never":
			s.uiOn = false
		default:
			s.uiOn = clientAdvertisesUI(r.Params)
		}
		log.Printf("initialize: mode=%s ui=%v", s.mode, s.uiOn)
		caps := map[string]any{"tools": map[string]any{}}
		if s.uiOn {
			caps["resources"] = map[string]any{}
		}
		s.reply(r.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    caps,
			"serverInfo":      map[string]any{"name": "mcp-apps-sample", "version": "0.1.0"},
		})
	case "ping":
		s.reply(r.ID, map[string]any{})
	case "tools/list":
		s.reply(r.ID, map[string]any{"tools": s.tools()})
	case "tools/call":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		json.Unmarshal(r.Params, &p)
		switch p.Name {
		case "show_calendar":
			s.reply(r.ID, s.callResult(s.gridText()))
		case "book_slot":
			key := p.Arguments["person"] + "|" + p.Arguments["day"]
			valid := false
			for _, pp := range s.people {
				for _, d := range s.days {
					valid = valid || key == pp+"|"+d
				}
			}
			if !valid {
				s.reply(r.ID, map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "unknown person or day: " + key}}})
				return
			}
			if s.bookings[key] != "" {
				s.reply(r.ID, map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": key + " is already " + s.bookings[key]}}})
				return
			}
			s.bookings[key] = "pending"
			s.reply(r.ID, s.callResult("Pencilled in "+p.Arguments["person"]+" on "+p.Arguments["day"]+".\n\n"+s.gridText()))
		default:
			s.fail(r.ID, -32602, "unknown tool: "+p.Name)
		}
	case "resources/list":
		if !s.uiOn {
			s.fail(r.ID, -32601, "method not found")
			return
		}
		s.reply(r.ID, map[string]any{"resources": []any{map[string]any{
			"uri": pageURI, "name": "Availability calendar", "mimeType": uiMime,
		}}})
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		json.Unmarshal(r.Params, &p)
		if !s.uiOn {
			s.fail(r.ID, -32601, "method not found")
			return
		}
		if p.URI != pageURI {
			s.fail(r.ID, -32002, "resource not found: "+p.URI)
			return
		}
		s.reply(r.ID, map[string]any{"contents": []any{map[string]any{
			"uri": pageURI, "mimeType": uiMime, "text": calendarHTML,
			"_meta": map[string]any{"ui": map[string]any{"prefersBorder": true}},
		}}})
	default:
		if len(r.ID) > 0 {
			s.fail(r.ID, -32601, "method not found: "+r.Method)
		}
		// notifications (initialized, cancelled, ...) need no reply
	}
}

func main() {
	mode := flag.String("ui", "auto", "auto | always | never")
	flag.Parse()
	if *mode != "auto" && *mode != "always" && *mode != "never" {
		fmt.Fprintln(os.Stderr, "--ui must be auto, always or never")
		os.Exit(2)
	}
	log.SetOutput(os.Stderr)
	log.SetPrefix("mcp-apps-sample: ")
	s := newServer(*mode, os.Stdout)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var r request
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			s.fail(nil, -32700, "parse error")
			continue
		}
		s.handle(r)
	}
}
