package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/registry"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/transport/a2a"
)

func main() {
	var (
		agentID = flag.String("agent", "mesh.scout-orchestrator", "system agent ID")
		baseURL = flag.String("base-url", strings.TrimSpace(os.Getenv("AGENTMESH_PUBLIC_BASE_URL")), "AgentMesh public base URL")
	)
	flag.Parse()

	if strings.TrimSpace(*baseURL) == "" {
		log.Fatal("--base-url is required")
	}
	keyVersion := strings.TrimSpace(os.Getenv("AGENTMESH_AGENT_CARD_KMS_KEY_VERSION"))
	if keyVersion == "" {
		log.Fatal("AGENTMESH_AGENT_CARD_KMS_KEY_VERSION is required")
	}

	ctx := context.Background()
	signer, err := registry.NewKMSSigner(ctx, keyVersion, strings.TrimSpace(os.Getenv("AGENTMESH_AGENT_CARD_KEY_ID")))
	if err != nil {
		log.Fatalf("kms signer: %v", err)
	}
	defer signer.Close()

	registrySvc := registry.NewService(signer)
	if err := registrySvc.BootstrapSystemAgents(ctx, strings.TrimRight(strings.TrimSpace(*baseURL), "/")); err != nil {
		log.Fatalf("bootstrap system agents: %v", err)
	}
	agent, ok := registrySvc.GetAgent(strings.TrimSpace(*agentID))
	if !ok {
		log.Fatalf("agent %q not found", strings.TrimSpace(*agentID))
	}
	encoded, err := a2a.EncodeAgentCard(agent)
	if err != nil {
		log.Fatalf("encode agent card: %v", err)
	}

	fmt.Println(encoded)
}
