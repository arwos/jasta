package archive

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

func extractTARGZ(input io.Reader, root *os.Root, limits Limits) error {
	compressed := &io.LimitedReader{R: input, N: limits.MaxArchiveBytes + 1}
	buffered := bufio.NewReader(compressed)
	gz, err := gzip.NewReader(buffered)
	if err != nil {
		return fmt.Errorf("open TAR.GZ archive: %w", err)
	}
	gz.Multistream(false)
	const maxLimited = math.MaxInt64 - 1
	streamLimit := limits.MaxUnpacked
	if streamLimit > maxLimited-(1<<20) {
		streamLimit = maxLimited
	} else {
		streamLimit += 1 << 20
	}
	if int64(limits.MaxEntries) > (math.MaxInt64-streamLimit)/1024 {
		streamLimit = maxLimited
	} else {
		streamLimit += int64(limits.MaxEntries) * 1024
		if streamLimit > maxLimited {
			streamLimit = maxLimited
		}
	}
	boundedStream := &io.LimitedReader{R: gz, N: streamLimit + 1}
	tarReader := tar.NewReader(boundedStream)
	x := newExtractor(root, limits)
	err = extractTAREntries(tarReader, x)
	if err == nil {
		err = x.finish()
	}
	if err == nil {
		if _, drainErr := io.Copy(io.Discard, boundedStream); drainErr != nil {
			err = drainErr
		}
		if err == nil && boundedStream.N == 0 {
			err = ErrUnpackedTooLarge
		}
	}
	// Reading boundedStream to EOF above also verifies the gzip checksum.
	closeErr := gz.Close()
	if err == nil && closeErr != nil {
		err = fmt.Errorf("close TAR.GZ stream: %w", closeErr)
	}
	// Read through the bounded source to account for trailing input and reject
	// concatenated members or other data after the single expected gzip member.
	trailing, trailErr := io.Copy(io.Discard, buffered)
	if err == nil && trailErr != nil {
		err = fmt.Errorf("read TAR.GZ trailing data: %w", trailErr)
	}
	if err == nil && trailing != 0 {
		err = errors.New("trailing data after TAR.GZ member")
	}
	if err == nil && compressed.N == 0 {
		err = ErrArchiveTooLarge
	}
	return err
}

func extractTAREntries(tarReader *tar.Reader, x *extractor) error {
	for {
		h, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			return nil
		}
		if nextErr != nil {
			return fmt.Errorf("read TAR header: %w", nextErr)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if h.Size != 0 {
				return fmt.Errorf("TAR directory %q contains file data", h.Name)
			}
			if err := x.addDirectory(h.Name); err != nil {
				return err
			}
		case tar.TypeReg:
			if h.Size < 0 {
				return fmt.Errorf("negative TAR file size for %q", h.Name)
			}
			if err := x.addFile(h.Name, tarReader); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if h.Size != 0 {
				return fmt.Errorf("TAR symlink %q contains file data", h.Name)
			}
			if err := x.addSymlink(h.Name, h.Linkname, true); err != nil {
				return err
			}
		case tar.TypeLink:
			if h.Size != 0 {
				return fmt.Errorf("TAR hardlink %q contains file data", h.Name)
			}
			if err := x.addHardlink(h.Name, h.Linkname); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported TAR entry type %d for %q", h.Typeflag, h.Name)
		}
	}
}
