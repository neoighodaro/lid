package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarballOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Line(name string, data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), name)
}

func TestOnlyABinaryMatchingThePublishedChecksumIsAccepted(t *testing.T) {
	tarball := []byte("the release everyone downloaded")
	checksums := sha256Line("lid-v1.3.0-darwin-arm64.tar.gz", tarball) +
		sha256Line("lid-v1.3.0-linux-amd64.tar.gz", []byte("another platform"))

	if err := VerifyChecksum([]byte(checksums), "lid-v1.3.0-darwin-arm64.tar.gz", tarball); err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}
	if err := VerifyChecksum([]byte(checksums), "lid-v1.3.0-darwin-arm64.tar.gz", []byte("something else entirely")); err == nil {
		t.Error("a tarball that is not what the release published was accepted and would have been installed")
	}
	if err := VerifyChecksum([]byte(checksums), "lid-v1.3.0-windows-amd64.tar.gz", tarball); err == nil {
		t.Error("an asset with no checksum entry was accepted unverified")
	}
}

func TestAChecksumsFileWrittenByShasumIsReadTheSameWay(t *testing.T) {
	tarball := []byte("release bytes")
	sum := sha256.Sum256(tarball)
	binary := fmt.Sprintf("%s *lid-v1.3.0-darwin-arm64.tar.gz\n", hex.EncodeToString(sum[:]))

	if err := VerifyChecksum([]byte(binary), "lid-v1.3.0-darwin-arm64.tar.gz", tarball); err != nil {
		t.Errorf("a checksums.txt in binary mode was rejected: %v", err)
	}
}

func TestTheLidBinaryIsFoundWhereverItSitsInTheArchive(t *testing.T) {
	binary, err := ExtractBinary(tarballOf(t, map[string]string{"lid": "the new lid"}))
	if err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	if string(binary) != "the new lid" {
		t.Errorf("binary = %q, want the archived lid", binary)
	}

	binary, err = ExtractBinary(tarballOf(t, map[string]string{"lid-v1.3.0-darwin-arm64/lid": "nested lid"}))
	if err != nil {
		t.Fatalf("ExtractBinary from a nested archive: %v", err)
	}
	if string(binary) != "nested lid" {
		t.Errorf("binary = %q, want the lid inside the release directory", binary)
	}
}

func TestAnArchiveWithoutALidBinaryIsRefused(t *testing.T) {
	if _, err := ExtractBinary(tarballOf(t, map[string]string{"README.md": "no binary here"})); err == nil {
		t.Fatal("an archive holding no lid was unpacked anyway, so nothing would be installed")
	}
	if _, err := ExtractBinary([]byte("not a gzip stream")); err == nil {
		t.Fatal("a body that is not an archive was unpacked anyway")
	}
}

func TestTheAssetsTheMakefilePacksAreTheOnesUpdateInstalls(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sums, err := os.ReadFile(filepath.Join(root, "dist", "checksums.txt"))
	if err != nil {
		t.Skip("no dist/checksums.txt; run 'make assets' to check the packaging end to end")
	}

	checked := 0
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		tarball, err := os.ReadFile(filepath.Join(root, "dist", name))
		if err != nil {
			t.Fatalf("checksums.txt names %s, which is not in dist/", name)
		}
		if err := VerifyChecksum(sums, name, tarball); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := ExtractBinary(tarball); err != nil {
			t.Errorf("'lid update' cannot unpack the %s that 'make assets' wrote: %v", name, err)
		}
		checked++
	}
	if checked == 0 {
		t.Error("dist/checksums.txt names no assets")
	}
}
