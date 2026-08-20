package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
)

func ensureManagedAgentBuildReadyTx(ctx context.Context, tx *sql.Tx, installationID string) error {
	var agentState agent.State
	var desiredGeneration int
	var buildState agent.BuildState
	var packagePath, packageSHA256, manifestSHA256 string
	var manifestSignature []byte
	err := tx.QueryRowContext(ctx, `SELECT a.state,a.desired_generation,b.state,b.package_path,b.package_sha256,b.manifest_sha256,b.manifest_signature
		FROM agents a
		JOIN agent_builds b ON b.agent_id=a.id AND b.generation=a.desired_generation
		WHERE a.installation_id=?`, installationID).Scan(&agentState, &desiredGeneration, &buildState, &packagePath, &packageSHA256, &manifestSHA256, &manifestSignature)
	if errors.Is(err, sql.ErrNoRows) {
		return fault.New(fault.AgentBuildNotReady, "installation managed agent build is missing", err)
	}
	if err != nil {
		return err
	}
	if desiredGeneration < 1 || agentState != agent.StateReady || buildState != agent.BuildStateReady || packagePath == "" || !validSHA256Hex(packageSHA256) || !validSHA256Hex(manifestSHA256) || !validManifestSignature(string(manifestSignature)) {
		return fault.New(fault.AgentBuildNotReady, "installation managed agent build is not ready", nil)
	}
	return nil
}
