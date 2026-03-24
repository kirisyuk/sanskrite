package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
)

var (
	projectID   = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID    = flag.String("tenant", "global", "tenant id")
	seedPath    = flag.String("seed", "", "seed product canonical path")
	direction   = flag.String("direction", "downstream", "stream direction: downstream|upstream")
	hops        = flag.Int("hops", 3, "max hop depth")
	limitPerHop = flag.Int("limit-per-hop", 24, "max edges per hop")
)

func main() {
	flag.Parse()
	if strings.TrimSpace(*seedPath) == "" {
		log.Fatal("--seed is required")
	}
	if *hops <= 0 {
		*hops = 3
	}
	if *limitPerHop <= 0 {
		*limitPerHop = 24
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	runtime := productdict.NewRuntime(fsClient.Client()).WithTenantID(strings.TrimSpace(*tenantID))
	expansion, err := runtime.ExpandProductStreamForTenant(
		ctx,
		strings.TrimSpace(*tenantID),
		strings.TrimSpace(*seedPath),
		strings.TrimSpace(*direction),
		*hops,
		*limitPerHop,
	)
	if err != nil {
		log.Fatalf("expand failed: %v", err)
	}

	fmt.Printf(
		"stream seed=%s tenant=%s direction=%s hops=%d limitPerHop=%d truncated=%t redis=%t firestore=%t\n",
		expansion.SeedPath,
		strings.TrimSpace(*tenantID),
		expansion.Direction,
		expansion.MaxHops,
		*limitPerHop,
		expansion.Truncated,
		expansion.UsedRedis,
		expansion.UsedFs,
	)

	nodes := append([]productdict.ProductStreamNode(nil), expansion.Nodes...)
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Hop != nodes[j].Hop {
			return nodes[i].Hop < nodes[j].Hop
		}
		return nodes[i].CanonicalPath < nodes[j].CanonicalPath
	})

	fmt.Printf("\nnodes\n")
	if len(nodes) == 0 {
		fmt.Printf("  none\n")
	} else {
		for _, node := range nodes {
			fmt.Printf("  - hop=%d path=%s\n", node.Hop, node.CanonicalPath)
		}
	}

	edges := append([]productdict.ProductTransformPair(nil), expansion.Edges...)
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].FromPath != edges[j].FromPath {
			return edges[i].FromPath < edges[j].FromPath
		}
		if edges[i].ToPath != edges[j].ToPath {
			return edges[i].ToPath < edges[j].ToPath
		}
		return edges[i].RelationKind < edges[j].RelationKind
	})

	fmt.Printf("\nedges\n")
	if len(edges) == 0 {
		fmt.Printf("  none\n")
		return
	}
	for _, edge := range edges {
		fmt.Printf("  - %s -> %s kind=%s\n", edge.FromPath, edge.ToPath, edge.RelationKind)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
