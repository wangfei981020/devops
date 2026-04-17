package services

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"opsplatform-probe-backend/config"
)

// ImageImportResult is the outcome of pulling an image and extracting a binary.
type ImageImportResult struct {
	Binary []byte
	SHA256 string
	Size   int64
}

// PullAndExtract pulls a container image and extracts the file at binaryPath.
// binaryPath should not start with leading "/", e.g. "app/probe-agent".
func PullAndExtract(cfg *config.Config, imageRef, binaryPath string) (*ImageImportResult, error) {
	binaryPath = strings.TrimPrefix(binaryPath, "/")

	opts := []crane.Option{}
	if cfg.ImageRegistryUsername != "" {
		opts = append(opts, crane.WithAuth(&authn.Basic{
			Username: cfg.ImageRegistryUsername,
			Password: cfg.ImageRegistryPassword,
		}))
	}

	img, err := crane.Pull(imageRef, opts...)
	if err != nil {
		return nil, fmt.Errorf("pull image: %w", err)
	}

	// Flatten image into a tar (squashed filesystem)
	var buf bytes.Buffer
	if err := crane.Export(img, &buf); err != nil {
		return nil, fmt.Errorf("export image: %w", err)
	}

	binary, err := extractFromTar(&buf, binaryPath)
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", binaryPath, err)
	}

	hash := sha256.Sum256(binary)
	return &ImageImportResult{
		Binary: binary,
		SHA256: hex.EncodeToString(hash[:]),
		Size:   int64(len(binary)),
	}, nil
}

func extractFromTar(r io.Reader, target string) ([]byte, error) {
	target = strings.TrimPrefix(target, "/")
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		name = strings.TrimPrefix(name, "/")
		if name == target {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("file %q not found in image", target)
}

// silence unused import warning if v1 not directly referenced
var _ = v1.Hash{}
