package agenttrust

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/agent"
)

const (
	caKeyFile      = "agent-ca.key"
	caCertFile     = "agent-ca.crt"
	updateKeyFile  = "agent-update.key"
	caLifetime     = 10 * 365 * 24 * time.Hour
	serverLifetime = 30 * 24 * time.Hour
	clientLifetime = 365 * 24 * time.Hour
)

type Authority struct {
	instanceID       string
	caCertificate    *x509.Certificate
	caPrivateKey     ed25519.PrivateKey
	caPEM            []byte
	updatePrivateKey ed25519.PrivateKey
	logger           *slog.Logger
}

type IssuedCertificate struct {
	PEM         []byte
	Serial      string
	Fingerprint string
	ExpiresAt   time.Time
}

func LoadOrCreate(directory string, logger *slog.Logger) (*Authority, error) {
	if logger == nil {
		logger = slog.Default()
	}
	directory = filepath.Clean(strings.TrimSpace(directory))
	if !filepath.IsAbs(directory) {
		return nil, errors.New("agent trust directory must be absolute")
	}
	if err := ensurePrivateDirectory(directory); err != nil {
		return nil, err
	}

	caKeyPath := filepath.Join(directory, caKeyFile)
	caCertPath := filepath.Join(directory, caCertFile)
	updateKeyPath := filepath.Join(directory, updateKeyFile)
	exists := []bool{fileExists(caKeyPath), fileExists(caCertPath), fileExists(updateKeyPath)}
	created := !exists[0] && !exists[1] && !exists[2]
	if created {
		if err := createAuthorityFiles(caKeyPath, caCertPath, updateKeyPath); err != nil {
			return nil, err
		}
	} else if !exists[0] || !exists[1] || !exists[2] {
		return nil, errors.New("agent trust material is incomplete")
	}

	caKey, err := loadPrivateKey(caKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load agent CA key: %w", err)
	}
	caPEM, err := readRegularFile(caCertPath, false)
	if err != nil {
		return nil, fmt.Errorf("load agent CA certificate: %w", err)
	}
	caCertificate, err := parseSingleCertificate(caPEM)
	if err != nil || !caCertificate.IsCA {
		return nil, errors.New("agent CA certificate is invalid")
	}
	caPublicKey, ok := caCertificate.PublicKey.(ed25519.PublicKey)
	if !ok || !caPublicKey.Equal(caKey.Public()) {
		return nil, errors.New("agent CA key does not match certificate")
	}
	updateKey, err := loadPrivateKey(updateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load agent update key: %w", err)
	}

	fingerprint := sha256.Sum256(caCertificate.RawSubjectPublicKeyInfo)
	authority := &Authority{
		instanceID:       "aegispxe_" + hex.EncodeToString(fingerprint[:12]),
		caCertificate:    caCertificate,
		caPrivateKey:     caKey,
		caPEM:            append([]byte(nil), caPEM...),
		updatePrivateKey: updateKey,
		logger:           logger,
	}
	logger.Info("agent trust authority ready",
		"component", "agent.trust",
		"operation", "load",
		"instance_id", authority.instanceID,
		"ca_fingerprint", certificateFingerprint(caCertificate.Raw),
		"result", map[bool]string{true: "created", false: "loaded"}[created],
	)
	return authority, nil
}

func (a *Authority) InstanceID() string { return a.instanceID }

func (a *Authority) CAPEM() string { return strings.TrimSpace(string(a.caPEM)) }

func (a *Authority) UpdateVerifyKeyB64() string {
	publicKey := a.updatePrivateKey.Public().(ed25519.PublicKey)
	return base64.RawURLEncoding.EncodeToString(publicKey)
}

func (a *Authority) SignUpdateManifest(payload []byte) (string, error) {
	if a == nil || len(a.updatePrivateKey) != ed25519.PrivateKeySize {
		return "", errors.New("agent update signing authority is unavailable")
	}
	if len(payload) == 0 {
		return "", errors.New("agent update manifest is empty")
	}
	signature := ed25519.Sign(a.updatePrivateKey, payload)
	return base64.RawURLEncoding.EncodeToString(signature), nil
}

func (a *Authority) ClientCAPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(a.caCertificate)
	return pool
}

func (a *Authority) NewServerCertificate(controllerURL string, now time.Time) (tls.Certificate, error) {
	origin, err := url.Parse(strings.TrimSpace(controllerURL))
	if err != nil || origin.Scheme != "https" || origin.Hostname() == "" {
		return tls.Certificate{}, errors.New("agent controller URL is invalid")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := randomSerial()
	if err != nil {
		return tls.Certificate{}, err
	}
	now = now.UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "AegisPXE Agent Control Plane"},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(serverLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(origin.Hostname()); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{origin.Hostname()}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.caCertificate, publicKey, a.caPrivateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}

func (a *Authority) IssueClientCertificate(agentID string, publicKey ed25519.PublicKey, now time.Time) (IssuedCertificate, error) {
	agentID = strings.TrimSpace(agentID)
	if err := agent.ValidateID(agentID); err != nil || len(publicKey) != ed25519.PublicKeySize {
		return IssuedCertificate{}, errors.New("agent certificate request is invalid")
	}
	serial, err := randomSerial()
	if err != nil {
		return IssuedCertificate{}, err
	}
	now = now.UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "aegispxe-agent:" + agentID},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(clientLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.caCertificate, publicKey, a.caPrivateKey)
	if err != nil {
		return IssuedCertificate{}, err
	}
	return IssuedCertificate{
		PEM:         pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		Serial:      serial.Text(16),
		Fingerprint: certificateFingerprint(der),
		ExpiresAt:   template.NotAfter,
	}, nil
}

func createAuthorityFiles(caKeyPath, caCertPath, updateKeyPath string) error {
	caPublic, caPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	caTemplate := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "AegisPXE Managed Agent CA"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(caLifetime),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageCRLSign,
		MaxPathLenZero:        true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caPublic, caPrivate)
	if err != nil {
		return err
	}
	caKeyDER, err := x509.MarshalPKCS8PrivateKey(caPrivate)
	if err != nil {
		return err
	}
	_, updatePrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	updateDER, err := x509.MarshalPKCS8PrivateKey(updatePrivate)
	if err != nil {
		return err
	}
	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{caKeyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: caKeyDER}), 0o600},
		{caCertPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644},
		{updateKeyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: updateDER}), 0o600},
	}
	for _, file := range files {
		if err := writeAtomic(file.path, file.data, file.mode); err != nil {
			return err
		}
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("agent trust path is not a directory")
		}
		if info.Mode().Perm()&0o077 != 0 {
			return errors.New("agent trust directory permissions are too broad")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.MkdirAll(path, 0o700)
}

func readRegularFile(path string, private bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("trust file is not a regular file")
	}
	if private && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("private trust file permissions are too broad")
	}
	return os.ReadFile(path)
}

func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := readRegularFile(path, true)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("private key PEM is invalid")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("private key is not Ed25519")
	}
	return key, nil
}

func parseSingleCertificate(data []byte) (*x509.Certificate, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("certificate PEM is invalid")
	}
	return x509.ParseCertificate(block.Bytes)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".aegispxe-agent-trust-*")
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

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func certificateFingerprint(der []byte) string {
	digest := sha256.Sum256(der)
	return hex.EncodeToString(digest[:])
}
