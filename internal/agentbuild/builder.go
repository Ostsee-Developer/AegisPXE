package agentbuild

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
	"github.com/Ostsee-Developer/AegisPXE/internal/agentidentity"
)

const maxTemplateBytes = 32 * 1024 * 1024

var debVersionSanitizer = regexp.MustCompile(`[^0-9A-Za-z.+~]`)

type Authority interface {
	InstanceID() string
	CAPEM() string
	UpdateVerifyKeyB64() string
	SignUpdateManifest([]byte) (string, error)
}

type Config struct {
	TemplatePath  string
	OutputDir     string
	ControllerURL string
	Version       string
}

type Artifact struct {
	PackagePath       string
	PackageSHA256     string
	PackageSize       int64
	ManifestSHA256    string
	ManifestSignature string
}

type Builder struct {
	config    Config
	authority Authority
	logger    *slog.Logger
}

func New(config Config, authority Authority, logger *slog.Logger) (*Builder, error) {
	config.TemplatePath = filepath.Clean(strings.TrimSpace(config.TemplatePath))
	config.OutputDir = filepath.Clean(strings.TrimSpace(config.OutputDir))
	config.ControllerURL = strings.TrimSpace(config.ControllerURL)
	config.Version = strings.TrimSpace(config.Version)
	controller, err := url.Parse(config.ControllerURL)
	if err != nil || controller.Scheme != "https" || controller.Host == "" || controller.User != nil || controller.RawQuery != "" || controller.Fragment != "" || (controller.Path != "" && controller.Path != "/") {
		return nil, errors.New("agent controller URL must be an HTTPS origin")
	}
	if !filepath.IsAbs(config.TemplatePath) || !filepath.IsAbs(config.OutputDir) {
		return nil, errors.New("agent template and output paths must be absolute")
	}
	if config.Version == "" || len(config.Version) > 128 {
		return nil, errors.New("agent version is invalid")
	}
	if authority == nil || strings.TrimSpace(authority.InstanceID()) == "" || strings.TrimSpace(authority.CAPEM()) == "" || strings.TrimSpace(authority.UpdateVerifyKeyB64()) == "" {
		return nil, errors.New("agent build authority is unavailable")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Builder{config: config, authority: authority, logger: logger}, nil
}

func (b *Builder) Build(ctx context.Context, record agent.Record, build agent.Build) (Artifact, error) {
	if err := record.Validate(); err != nil {
		return Artifact{}, fmt.Errorf("validate managed agent: %w", err)
	}
	if build.AgentID != record.ID || build.Generation != record.DesiredGeneration {
		return Artifact{}, errors.New("agent build does not match desired agent generation")
	}
	if build.Architecture != "amd64" && build.Architecture != "arm64" {
		return Artifact{}, fmt.Errorf("unsupported agent build architecture %q", build.Architecture)
	}
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}

	template, err := readTemplate(b.config.TemplatePath)
	if err != nil {
		return Artifact{}, err
	}
	identity, err := (agentidentity.Identity{
		SchemaVersion:      agentidentity.SchemaVersion,
		AgentID:            record.ID,
		InstallationID:     record.InstallationID,
		MachineID:          record.MachineID,
		InstanceID:         b.authority.InstanceID(),
		ControllerURL:      b.config.ControllerURL,
		Version:            b.config.Version,
		Generation:         build.Generation,
		Architecture:       build.Architecture,
		CapabilityCeiling:  append([]string(nil), build.CapabilityCeiling...),
		ServerCAPEM:        b.authority.CAPEM(),
		UpdateVerifyKeyB64: b.authority.UpdateVerifyKeyB64(),
	}).Normalize()
	if err != nil {
		return Artifact{}, fmt.Errorf("create agent build identity: %w", err)
	}
	sealed, err := agentidentity.Seal(template, identity)
	if err != nil {
		return Artifact{}, fmt.Errorf("seal agent binary: %w", err)
	}
	debVersion := debianVersion(b.config.Version, record.ID, build.Generation)
	packageBytes, err := buildDebianPackage(debVersion, build.Architecture, sealed)
	if err != nil {
		return Artifact{}, err
	}
	packageSHA := Digest(packageBytes)
	manifest := Manifest{
		SchemaVersion:     ManifestSchemaVersion,
		AgentID:           record.ID,
		InstallationID:    record.InstallationID,
		MachineID:         record.MachineID,
		InstanceID:        b.authority.InstanceID(),
		Version:           b.config.Version,
		Generation:        build.Generation,
		Architecture:      build.Architecture,
		CapabilityCeiling: append([]string(nil), build.CapabilityCeiling...),
		PackageSHA256:     packageSHA,
		PackageSize:       int64(len(packageBytes)),
	}
	manifestJSON, err := manifest.CanonicalJSON()
	if err != nil {
		return Artifact{}, fmt.Errorf("create agent build manifest: %w", err)
	}
	signature, err := b.authority.SignUpdateManifest(manifestJSON)
	if err != nil {
		return Artifact{}, fmt.Errorf("sign agent build manifest: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	if err := os.MkdirAll(b.config.OutputDir, 0o700); err != nil {
		return Artifact{}, fmt.Errorf("create agent build output directory: %w", err)
	}
	if info, err := os.Lstat(b.config.OutputDir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return Artifact{}, errors.New("agent build output directory is not a private regular directory")
	}

	base := fmt.Sprintf("aegispxe-agent_%s_%s", debVersion, build.Architecture)
	packageName := base + ".deb"
	manifestName := base + ".manifest.json"
	signatureName := base + ".manifest.sig"
	for _, file := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{name: packageName, data: packageBytes, mode: 0o600},
		{name: manifestName, data: manifestJSON, mode: 0o600},
		{name: signatureName, data: []byte(signature + "\n"), mode: 0o600},
	} {
		if err := writeAtomic(filepath.Join(b.config.OutputDir, file.name), file.data, file.mode); err != nil {
			return Artifact{}, fmt.Errorf("persist agent build artifact %s: %w", file.name, err)
		}
	}

	b.logger.InfoContext(ctx, "managed agent package built",
		"component", "agent.build",
		"operation", "build",
		"agent_id", record.ID,
		"installation_id", record.InstallationID,
		"machine_id", record.MachineID,
		"generation", build.Generation,
		"version", b.config.Version,
		"architecture", build.Architecture,
		"package_sha256", packageSHA,
		"package_size", len(packageBytes),
		"manifest_sha256", Digest(manifestJSON),
		"result", "success",
	)
	return Artifact{
		PackagePath:       packageName,
		PackageSHA256:     packageSHA,
		PackageSize:       int64(len(packageBytes)),
		ManifestSHA256:    Digest(manifestJSON),
		ManifestSignature: signature,
	}, nil
}

func readTemplate(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect agent template: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > maxTemplateBytes {
		return nil, errors.New("agent template must be a bounded regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent template: %w", err)
	}
	if agentidentity.HasIdentity(data) {
		return nil, errors.New("packaged agent template must not already contain an identity")
	}
	return data, nil
}

func debianVersion(version, agentID string, generation int) string {
	base := strings.TrimSpace(version)
	base = strings.ReplaceAll(base, "-", "~")
	base = debVersionSanitizer.ReplaceAllString(base, ".")
	shortID := strings.ReplaceAll(agentID, "-", "")[:8]
	return fmt.Sprintf("%s+agent.%s.g%d", base, shortID, generation)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".aegispxe-agent-build-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
