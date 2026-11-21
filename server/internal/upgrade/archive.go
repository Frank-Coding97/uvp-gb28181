package upgrade

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// packageEntry is one regular file or directory inside a release artifact.
// The Unix release ships tar.gz; the Windows release ships zip, so both the
// extractor and the capacity preflight read artifacts through this neutral
// shape instead of duplicating tar/zip parsing.
type packageEntry struct {
	Name string
	Size int64
	Dir  bool
	// Exec is the archive's executable hint. Windows artifacts never set it;
	// the control script is invoked explicitly instead of by shebang.
	Exec bool
	// Body streams the entry contents. It is only valid until the next call to
	// Next.
	Body io.Reader
}

type packageArchive interface {
	Next() (packageEntry, error)
	Close() error
}

// openPackageArchive selects the reader by suffix. An unknown suffix is treated
// as an uncompressed tar, which keeps the Linux artifact path unchanged.
func openPackageArchive(path string) (packageArchive, error) {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".zip") {
		reader, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		return &zipArchive{reader: reader}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var reader io.Reader = file
	var gz *gzip.Reader
	if strings.HasSuffix(lower, ".gz") || strings.HasSuffix(lower, ".tgz") {
		gz, err = gzip.NewReader(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		reader = gz
	}
	return &tarArchive{file: file, gz: gz, reader: tar.NewReader(reader)}, nil
}

type tarArchive struct {
	file   *os.File
	gz     *gzip.Reader
	reader *tar.Reader
}

func (a *tarArchive) Next() (packageEntry, error) {
	for {
		header, err := a.reader.Next()
		if err != nil {
			return packageEntry{}, err
		}
		switch header.Typeflag {
		case tar.TypeDir, tar.TypeReg, tar.TypeRegA:
		default:
			return packageEntry{}, fmt.Errorf("archive entry %q is not a regular file or directory", header.Name)
		}
		if header.Size < 0 {
			return packageEntry{}, errors.New("archive entry has a negative size")
		}
		return packageEntry{
			Name: header.Name,
			Size: header.Size,
			Dir:  header.Typeflag == tar.TypeDir,
			Exec: header.Mode&0o111 != 0,
			Body: a.reader,
		}, nil
	}
}

func (a *tarArchive) Close() error {
	if a.gz != nil {
		// Draining the compressed stream validates the gzip CRC and rejects an
		// artifact whose data continues past the tar end markers, which the
		// previous limit-reader implementation also enforced.
		_, _ = io.Copy(io.Discard, a.gz)
		_ = a.gz.Close()
	}
	return a.file.Close()
}

type zipArchive struct {
	reader  *zip.ReadCloser
	index   int
	current io.Closer
}

func (a *zipArchive) Next() (packageEntry, error) {
	// zip hands out one open handle per entry; close the previous one before
	// opening the next so a 700-file artifact does not exhaust descriptors.
	if a.current != nil {
		_ = a.current.Close()
		a.current = nil
	}
	for a.index < len(a.reader.File) {
		file := a.reader.File[a.index]
		a.index++
		info := file.FileInfo()
		switch {
		case info.IsDir():
			return packageEntry{Name: file.Name, Dir: true}, nil
		case info.Mode().IsRegular():
		default:
			return packageEntry{}, fmt.Errorf("archive entry %q is not a regular file or directory", file.Name)
		}
		body, err := file.Open()
		if err != nil {
			return packageEntry{}, err
		}
		a.current = body
		return packageEntry{
			Name: file.Name,
			Size: int64(file.UncompressedSize64),
			Exec: info.Mode()&0o111 != 0,
			Body: body,
		}, nil
	}
	return packageEntry{}, io.EOF
}

func (a *zipArchive) Close() error {
	if a.current != nil {
		_ = a.current.Close()
		a.current = nil
	}
	return a.reader.Close()
}
