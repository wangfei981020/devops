package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"log"
	"os"

	"sso-backend/config"
)

var PrivateKey *rsa.PrivateKey

func InitKeys() {
	// Try load from file
	if config.RSAPrivateKeyPath != "" {
		data, err := os.ReadFile(config.RSAPrivateKeyPath)
		if err == nil {
			block, _ := pem.Decode(data)
			if block != nil {
				key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
				if err == nil {
					PrivateKey = key
					log.Println("[Keys] RSA private key loaded from file")
					return
				}
			}
		}
		log.Printf("[Keys] Warning: failed to load RSA key from %s, generating new key", config.RSAPrivateKeyPath)
	}

	// Auto-generate
	var err error
	PrivateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal("[Keys] Failed to generate RSA key:", err)
	}
	log.Println("[Keys] RSA key auto-generated (will change on restart)")
}
