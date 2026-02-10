package main

import (
	"context"
	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed instruction-reference.md
var instructionReferenceMD string

//go:embed diagramming-guide.md
var diagrammingGuideMD string

//go:embed quick-reference.md
var quickReferenceMD string

func registerResources(server *mcp.Server) {
	server.AddResource(&mcp.Resource{
		URI:         "whiteboard://instructions",
		Name:        "instruction-reference",
		Description: "Complete reference of all drawing instruction types with their fields and parameters.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      "whiteboard://instructions",
					MIMEType: "text/markdown",
					Text:     instructionReferenceMD,
				},
			},
		}, nil
	})

	server.AddResource(&mcp.Resource{
		URI:         "whiteboard://diagramming-guide",
		Name:        "diagramming-guide",
		Description: "Read this before drawing: how to draw so humans can understand.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      "whiteboard://diagramming-guide",
					MIMEType: "text/markdown",
					Text:     diagrammingGuideMD,
				},
			},
		}, nil
	})

	server.AddResource(&mcp.Resource{
		URI:         "whiteboard://quick-reference",
		Name:        "quick-reference",
		Description: "Condensed cheat sheet: essential instructions, JSON format, colors, and arrows.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      "whiteboard://quick-reference",
					MIMEType: "text/markdown",
					Text:     quickReferenceMD,
				},
			},
		}, nil
	})
}
