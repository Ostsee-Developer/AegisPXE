package agentbuild

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const maxAgentPackageBytes = 64 * 1024 * 1024

type debFile struct {
	Path string
	Mode int64
	Data []byte
}

func buildDebianPackage(version, architecture string, binary []byte) ([]byte, error) {
	version = strings.TrimSpace(version)
	architecture = strings.TrimSpace(architecture)
	if version == "" || len(version) > 128 || strings.ContainsAny(version, "\r\n\x00") {
		return nil, errors.New("agent Debian version is invalid")
	}
	if architecture != "amd64" && architecture != "arm64" {
		return nil, fmt.Errorf("unsupported Debian architecture %q", architecture)
	}
	if len(binary) == 0 || len(binary) > maxAgentPackageBytes/2 {
		return nil, errors.New("agent binary size is invalid")
	}

	control := "Package: aegispxe-agent\n" +
		"Version: " + version + "\n" +
		"Section: admin\n" +
		"Priority: optional\n" +
		"Architecture: " + architecture + "\n" +
		"Maintainer: Ostsee-Developer\n" +
		"Depends: adduser, ca-certificates\n" +
		"Description: Per-installation AegisPXE managed agent\n"

	postinst := `#!/bin/sh
set -e
if ! getent group aegispxe-agent >/dev/null 2>&1; then
  addgroup --system aegispxe-agent
fi
if ! getent passwd aegispxe-agent >/dev/null 2>&1; then
  adduser --system --ingroup aegispxe-agent --no-create-home --home /var/lib/aegispxe-agent --shell /usr/sbin/nologin aegispxe-agent
fi
install -d -o aegispxe-agent -g aegispxe-agent -m 0700 /var/lib/aegispxe-agent
install -d -o aegispxe-agent -g aegispxe-agent -m 0700 /var/cache/aegispxe-agent
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  systemctl daemon-reload
  systemctl enable aegispxe-agent.service
fi
`
	prerm := `#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  if systemctl is-active --quiet aegispxe-agent.service; then
    systemctl stop aegispxe-agent.service
  fi
  if systemctl is-enabled --quiet aegispxe-agent.service; then
    systemctl disable aegispxe-agent.service
  fi
fi
`
	postrm := `#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  systemctl daemon-reload
fi
`
	service := `[Unit]
Description=AegisPXE managed agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/aegispxe-agent
Restart=on-failure
RestartSec=5s
User=aegispxe-agent
Group=aegispxe-agent
StateDirectory=aegispxe-agent
StateDirectoryMode=0700
CacheDirectory=aegispxe-agent
CacheDirectoryMode=0700
UMask=0077
NoNewPrivileges=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
RestrictRealtime=yes
CapabilityBoundingSet=
AmbientCapabilities=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6

[Install]
WantedBy=multi-user.target
`

	controlTar, err := gzipTar([]debFile{
		{Path: "control", Mode: 0o644, Data: []byte(control)},
		{Path: "postinst", Mode: 0o755, Data: []byte(postinst)},
		{Path: "prerm", Mode: 0o755, Data: []byte(prerm)},
		{Path: "postrm", Mode: 0o755, Data: []byte(postrm)},
	})
	if err != nil {
		return nil, fmt.Errorf("build control archive: %w", err)
	}
	dataTar, err := gzipTar([]debFile{
		{Path: "usr/bin/aegispxe-agent", Mode: 0o755, Data: binary},
		{Path: "lib/systemd/system/aegispxe-agent.service", Mode: 0o644, Data: []byte(service)},
	})
	if err != nil {
		return nil, fmt.Errorf("build data archive: %w", err)
	}

	var out bytes.Buffer
	out.WriteString("!<arch>\n")
	for _, member := range []struct {
		Name string
		Data []byte
	}{
		{Name: "debian-binary", Data: []byte("2.0\n")},
		{Name: "control.tar.gz", Data: controlTar},
		{Name: "data.tar.gz", Data: dataTar},
	} {
		if err := writeArMember(&out, member.Name, member.Data); err != nil {
			return nil, err
		}
	}
	if out.Len() > maxAgentPackageBytes {
		return nil, errors.New("agent Debian package exceeds size limit")
	}
	return out.Bytes(), nil
}

func gzipTar(files []debFile) ([]byte, error) {
	var out bytes.Buffer
	gz, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	gz.Header.ModTime = time.Unix(0, 0).UTC()
	gz.Header.OS = 255
	tw := tar.NewWriter(gz)
	for _, file := range files {
		path := strings.TrimPrefix(strings.TrimSpace(file.Path), "/")
		if path == "" || strings.Contains(path, "..") || strings.ContainsAny(path, "\r\n\x00") {
			_ = tw.Close()
			_ = gz.Close()
			return nil, fmt.Errorf("invalid package path %q", file.Path)
		}
		header := &tar.Header{
			Name:       path,
			Mode:       file.Mode,
			Size:       int64(len(file.Data)),
			ModTime:    time.Unix(0, 0).UTC(),
			AccessTime: time.Unix(0, 0).UTC(),
			ChangeTime: time.Unix(0, 0).UTC(),
			Uid:        0,
			Gid:        0,
			Uname:      "root",
			Gname:      "root",
			Typeflag:   tar.TypeReg,
			Format:     tar.FormatGNU,
		}
		if err := tw.WriteHeader(header); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			return nil, err
		}
		if _, err := tw.Write(file.Data); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeArMember(w io.Writer, name string, data []byte) error {
	if name == "" || len(name) > 15 || strings.ContainsAny(name, " /\r\n\x00") {
		return fmt.Errorf("invalid ar member name %q", name)
	}
	// System V ar header: name, timestamp, owner, group, mode, size, magic.
	header := fmt.Sprintf("%-16s%-12s%-6s%-6s%-8s%-10s`\n",
		name+"/", "0", "0", "0", "100644", strconv.Itoa(len(data)))
	if len(header) != 60 {
		return errors.New("invalid ar header length")
	}
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if len(data)%2 != 0 {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}
