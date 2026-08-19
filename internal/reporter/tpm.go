package reporter

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Ostsee-Developer/AegisPXE/internal/boottrust"
	legacytpm2 "github.com/google/go-tpm/legacy/tpm2"
	"github.com/google/go-tpm/tpmutil"
)

const lifecycleOAEPLabel = "AegisPXE lifecycle credential v1"

type TPMKey struct {
	rw          io.ReadWriteCloser
	handle      tpmutil.Handle
	publicKey   *rsa.PublicKey
	publicPEM   string
	fingerprint string
}

func OpenTPMKey() (*TPMKey, error) {
	var rw io.ReadWriteCloser
	var err error
	for _, path := range []string{"/dev/tpmrm0", "/dev/tpm0"} {
		if _, statErr := os.Stat(path); statErr != nil {
			continue
		}
		rw, err = tpmutil.OpenTPM(path)
		if err == nil {
			break
		}
	}
	if rw == nil {
		if err == nil {
			err = errors.New("no TPM 2.0 device found")
		}
		return nil, err
	}

	template := legacytpm2.Public{
		Type:    legacytpm2.AlgRSA,
		NameAlg: legacytpm2.AlgSHA256,
		Attributes: legacytpm2.FlagFixedTPM |
			legacytpm2.FlagFixedParent |
			legacytpm2.FlagSensitiveDataOrigin |
			legacytpm2.FlagUserWithAuth |
			legacytpm2.FlagSign |
			legacytpm2.FlagDecrypt |
			legacytpm2.FlagNoDA,
		RSAParameters: &legacytpm2.RSAParams{KeyBits: 2048},
	}
	handle, public, err := legacytpm2.CreatePrimary(rw, legacytpm2.HandleOwner, legacytpm2.PCRSelection{}, "", "", template)
	if err != nil {
		_ = rw.Close()
		return nil, fmt.Errorf("create deterministic TPM reporter key: %w", err)
	}
	rsaPublic, ok := public.(*rsa.PublicKey)
	if !ok {
		_ = legacytpm2.FlushContext(rw, handle)
		_ = rw.Close()
		return nil, errors.New("TPM reporter key is not RSA")
	}
	der, err := x509.MarshalPKIXPublicKey(rsaPublic)
	if err != nil {
		_ = legacytpm2.FlushContext(rw, handle)
		_ = rw.Close()
		return nil, fmt.Errorf("marshal TPM reporter public key: %w", err)
	}
	pemValue := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	_, fingerprint, err := boottrust.ParseRSAPublicKeyPEM(pemValue)
	if err != nil {
		_ = legacytpm2.FlushContext(rw, handle)
		_ = rw.Close()
		return nil, err
	}
	return &TPMKey{rw: rw, handle: handle, publicKey: rsaPublic, publicPEM: pemValue, fingerprint: fingerprint}, nil
}

func (k *TPMKey) Close() error {
	if k == nil || k.rw == nil {
		return nil
	}
	_ = legacytpm2.FlushContext(k.rw, k.handle)
	return k.rw.Close()
}

func (k *TPMKey) PublicKeyPEM() string { return k.publicPEM }
func (k *TPMKey) Fingerprint() string  { return k.fingerprint }

func (k *TPMKey) Sign(message []byte) ([]byte, error) {
	digest := sha256.Sum256(message)
	signature, err := legacytpm2.Sign(k.rw, k.handle, "", digest[:], nil, &legacytpm2.SigScheme{Alg: legacytpm2.AlgRSASSA, Hash: legacytpm2.AlgSHA256})
	if err != nil {
		return nil, fmt.Errorf("TPM sign boot trust challenge: %w", err)
	}
	if signature == nil || signature.RSA == nil || signature.RSA.HashAlg != legacytpm2.AlgSHA256 {
		return nil, errors.New("TPM returned an unexpected boot trust signature")
	}
	if err := rsa.VerifyPKCS1v15(k.publicKey, crypto.SHA256, digest[:], signature.RSA.Signature); err != nil {
		return nil, fmt.Errorf("self-verify TPM signature: %w", err)
	}
	return append([]byte(nil), signature.RSA.Signature...), nil
}

func (k *TPMKey) DecryptLifecycleCredential(ciphertext []byte) (string, error) {
	plaintext, err := legacytpm2.RSADecrypt(k.rw, k.handle, "", ciphertext, &legacytpm2.AsymScheme{Alg: legacytpm2.AlgOAEP, Hash: legacytpm2.AlgSHA256}, lifecycleOAEPLabel)
	if err != nil {
		return "", fmt.Errorf("TPM decrypt lifecycle credential: %w", err)
	}
	if len(plaintext) < 32 || len(plaintext) > 128 {
		return "", errors.New("decrypted lifecycle credential has invalid length")
	}
	return string(plaintext), nil
}
