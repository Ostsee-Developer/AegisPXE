package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ostsee-Developer/AegisPXE/internal/agentidentity"
)

var version = "dev"

type publicIdentity struct {
	AgentID        string   `json:"agent_id"`
	InstallationID string   `json:"installation_id"`
	MachineID      string   `json:"machine_id"`
	InstanceID     string   `json:"instance_id"`
	ControllerURL  string   `json:"controller_url"`
	Version        string   `json:"version"`
	Generation     int      `json:"generation"`
	Architecture   string   `json:"architecture"`
	Capabilities   []string `json:"capabilities"`
}

func main() {
	showVersion := flag.Bool("version", false, "print version")
	showIdentity := flag.Bool("identity", false, "print non-secret sealed identity")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	identity, err := agentidentity.ReadExecutable()
	if err != nil {
		logger.Error("agent startup rejected", "component", "agent.runtime", "operation", "load_identity", "result", "failure", "error", err)
		os.Exit(1)
	}
	if identity.Version != version {
		logger.Error("agent binary version does not match sealed identity", "component", "agent.runtime", "operation", "validate_identity", "agent_id", identity.AgentID, "generation", identity.Generation, "binary_version", version, "identity_version", identity.Version, "result", "failure")
		os.Exit(1)
	}
	if *showIdentity {
		payload := publicIdentity{AgentID: identity.AgentID, InstallationID: identity.InstallationID, MachineID: identity.MachineID, InstanceID: identity.InstanceID, ControllerURL: identity.ControllerURL, Version: identity.Version, Generation: identity.Generation, Architecture: identity.Architecture, Capabilities: append([]string(nil), identity.CapabilityCeiling...)}
		if err := json.NewEncoder(os.Stdout).Encode(payload); err != nil {
			logger.Error("could not encode agent identity", "component", "agent.runtime", "operation", "print_identity", "result", "failure", "error", err)
			os.Exit(1)
		}
		return
	}

	logger.Info("managed agent runtime started", "component", "agent.runtime", "operation", "start", "agent_id", identity.AgentID, "installation_id", identity.InstallationID, "machine_id", identity.MachineID, "instance_id", identity.InstanceID, "generation", identity.Generation, "version", identity.Version, "architecture", identity.Architecture, "capability_count", len(identity.CapabilityCeiling), "result", "ready")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	logger.Info("managed agent runtime stopped", "component", "agent.runtime", "operation", "shutdown", "agent_id", identity.AgentID, "generation", identity.Generation, "result", "success")
}
