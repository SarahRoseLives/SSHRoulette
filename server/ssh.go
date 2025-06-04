package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

func LoadOrGenerateKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err == nil {
		return ssh.ParsePrivateKey(keyBytes)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %v", err)
	}

	privateKey := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}

	keyFile, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create key file: %v", err)
	}
	defer keyFile.Close()

	if err := pem.Encode(keyFile, privateKey); err != nil {
		return nil, fmt.Errorf("failed to write key to file: %v", err)
	}

	if err := keyFile.Chmod(0600); err != nil {
		return nil, fmt.Errorf("failed to set key file permissions: %v", err)
	}

	return ssh.NewSignerFromKey(key)
}
