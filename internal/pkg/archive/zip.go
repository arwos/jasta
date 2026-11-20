package archive

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	zipEOCDSignature      = 0x06054b50
	zip64LocatorSignature = 0x07064b50
	zip64EndSignature     = 0x06064b50
	zipCentralSignature   = 0x02014b50
)

func extractZIP(input io.Reader, root *os.Root, stage string, limits Limits) error {
	tmp, err := os.CreateTemp(stage, ".jasta-upload-*.zip")
	if err != nil {
		return fmt.Errorf("create temporary ZIP: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	limited := &io.LimitedReader{R: input, N: limits.MaxArchiveBytes + 1}
	n, err := io.Copy(tmp, limited)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save ZIP input: %w", err)
	}
	if n > limits.MaxArchiveBytes {
		_ = tmp.Close()
		return ErrArchiveTooLarge
	}
	if n == 0 {
		_ = tmp.Close()
		return errors.New("empty ZIP input")
	}
	if err = preflightZIP(tmp, n, limits.MaxEntries); err != nil {
		_ = tmp.Close()
		return err
	}
	r, err := zip.NewReader(tmp, n)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("open ZIP archive: %w", err)
	}
	if len(r.File) > limits.MaxEntries {
		_ = tmp.Close()
		return ErrTooManyEntries
	}
	x := newExtractor(root, limits)
	for _, f := range r.File {
		mode := f.Mode()
		if mode.IsDir() {
			entry, openErr := f.Open()
			if openErr != nil {
				err = fmt.Errorf("open ZIP directory %q: %w", f.Name, openErr)
				break
			}
			written, readErr := io.Copy(io.Discard, io.LimitReader(entry, 1))
			closeErr := entry.Close()
			if readErr != nil {
				err = fmt.Errorf("read ZIP directory %q: %w", f.Name, readErr)
			} else if written != 0 {
				err = fmt.Errorf("ZIP directory %q contains file data", f.Name)
			} else if closeErr != nil {
				err = fmt.Errorf("close ZIP directory %q: %w", f.Name, closeErr)
			}
			if err != nil {
				break
			}
			if err = x.addDirectory(f.Name); err != nil {
				break
			}
			continue
		}
		if mode&os.ModeSymlink != 0 {
			entry, openErr := f.Open()
			if openErr != nil {
				err = fmt.Errorf("open ZIP symlink %q: %w", f.Name, openErr)
				break
			}
			readErr := x.readSymlink(f.Name, entry)
			closeErr := entry.Close()
			if readErr != nil {
				err = readErr
			} else if closeErr != nil {
				err = closeErr
			}
			if err != nil {
				break
			}
			continue
		}
		if !mode.IsRegular() {
			err = fmt.Errorf("unsupported ZIP entry type for %q", f.Name)
			break
		}
		entry, openErr := f.Open()
		if openErr != nil {
			err = fmt.Errorf("open ZIP entry %q: %w", f.Name, openErr)
			break
		}
		copyErr := x.addFile(f.Name, entry)
		closeErr := entry.Close()
		if copyErr != nil {
			err = copyErr
		} else if closeErr != nil {
			err = fmt.Errorf("close ZIP entry %q: %w", f.Name, closeErr)
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		err = x.finish()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return fmt.Errorf("close temporary ZIP: %w", closeErr)
	}
	return nil
}

// preflightZIP checks the declared central-directory count before archive/zip
// allocates its File slice, then checks every central record boundary.
func preflightZIP(f *os.File, size int64, maxEntries int) error {
	count, dirOffset, dirSize, dirEnd, err := readZIPDirectoryBounds(f, size)
	if err != nil {
		return err
	}
	if count > uint64(maxEntries) {
		return ErrTooManyEntries
	}
	if dirSize > uint64(size) || dirOffset > uint64(size) || dirSize+dirOffset > uint64(size) {
		return errors.New("malformed ZIP central directory bounds")
	}
	if dirSize+dirOffset > uint64(dirEnd) {
		return errors.New("malformed ZIP central directory bounds")
	}
	base := uint64(dirEnd) - dirSize - dirOffset
	if base+dirOffset+dirSize > uint64(size) {
		return errors.New("malformed ZIP central directory bounds")
	}
	return validateZIPCentralDirectory(f, base+dirOffset, dirSize, count)
}

func readZIPDirectoryBounds(f *os.File, size int64) (count, dirOffset, dirSize uint64, dirEnd int64, err error) {
	const maxComment = 1<<16 - 1
	tailSize := int64(22 + maxComment)
	if tailSize > size {
		tailSize = size
	}
	tail := make([]byte, int(tailSize))
	if _, err := f.ReadAt(tail, size-tailSize); err != nil && err != io.EOF {
		return 0, 0, 0, 0, err
	}
	eocd := -1
	for i := len(tail) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) != zipEOCDSignature {
			continue
		}
		comment := int(binary.LittleEndian.Uint16(tail[i+20:]))
		if i+22+comment == len(tail) {
			eocd = i
			break
		}
	}
	if eocd < 0 {
		return 0, 0, 0, 0, errors.New("malformed ZIP end record")
	}
	end := tail[eocd:]
	if binary.LittleEndian.Uint16(end[4:]) != 0 || binary.LittleEndian.Uint16(end[6:]) != 0 || binary.LittleEndian.Uint16(end[8:]) != binary.LittleEndian.Uint16(end[10:]) {
		return 0, 0, 0, 0, errors.New("multi-disk ZIP archives are unsupported")
	}
	count = uint64(binary.LittleEndian.Uint16(end[10:]))
	dirSize = uint64(binary.LittleEndian.Uint32(end[12:]))
	dirOffset = uint64(binary.LittleEndian.Uint32(end[16:]))
	dirEnd = size - tailSize + int64(eocd)
	if count == 0xffff || dirSize == 0xffffffff || dirOffset == 0xffffffff {
		if size < 56 {
			return 0, 0, 0, 0, errors.New("truncated ZIP64 archive")
		}
		locOffset := dirEnd - 20
		if locOffset < 0 {
			return 0, 0, 0, 0, errors.New("malformed ZIP64 locator")
		}
		loc := make([]byte, 20)
		if _, err := f.ReadAt(loc, locOffset); err != nil {
			return 0, 0, 0, 0, err
		}
		if binary.LittleEndian.Uint32(loc) != zip64LocatorSignature || binary.LittleEndian.Uint32(loc[4:]) != 0 || binary.LittleEndian.Uint32(loc[16:]) != 1 {
			return 0, 0, 0, 0, errors.New("malformed ZIP64 locator")
		}
		zoff := binary.LittleEndian.Uint64(loc[8:])
		if zoff > uint64(size-56) {
			return 0, 0, 0, 0, errors.New("malformed ZIP64 end record offset")
		}
		z := make([]byte, 56)
		if _, err := f.ReadAt(z, int64(zoff)); err != nil {
			return 0, 0, 0, 0, err
		}
		if binary.LittleEndian.Uint32(z) != zip64EndSignature || binary.LittleEndian.Uint64(z[4:]) < 44 {
			return 0, 0, 0, 0, errors.New("malformed ZIP64 end record")
		}
		if binary.LittleEndian.Uint32(z[16:]) != 0 || binary.LittleEndian.Uint32(z[20:]) != 0 {
			return 0, 0, 0, 0, errors.New("multi-disk ZIP64 archives are unsupported")
		}
		if binary.LittleEndian.Uint64(z[24:]) != binary.LittleEndian.Uint64(z[32:]) {
			return 0, 0, 0, 0, errors.New("inconsistent ZIP64 entry count")
		}
		count = binary.LittleEndian.Uint64(z[32:])
		dirSize = binary.LittleEndian.Uint64(z[40:])
		dirOffset = binary.LittleEndian.Uint64(z[48:])
		dirEnd = int64(zoff)
	}
	return count, dirOffset, dirSize, dirEnd, nil
}

func validateZIPCentralDirectory(f *os.File, offset, size, count uint64) error {
	reader := io.NewSectionReader(f, int64(offset), int64(size))
	for i := uint64(0); i < count; i++ {
		h := make([]byte, 46)
		if _, err := io.ReadFull(reader, h); err != nil {
			return fmt.Errorf("malformed ZIP central directory: %w", err)
		}
		if binary.LittleEndian.Uint32(h) != zipCentralSignature {
			return errors.New("malformed ZIP central directory record")
		}
		extra := int64(binary.LittleEndian.Uint16(h[28:])) + int64(binary.LittleEndian.Uint16(h[30:])) + int64(binary.LittleEndian.Uint16(h[32:]))
		pos, _ := reader.Seek(0, io.SeekCurrent)
		if extra > reader.Size()-pos {
			return errors.New("malformed ZIP central directory record length")
		}
		if _, err := io.CopyN(io.Discard, reader, extra); err != nil {
			return err
		}
	}
	pos, _ := reader.Seek(0, io.SeekCurrent)
	if pos != reader.Size() {
		return errors.New("malformed ZIP central directory size")
	}
	return nil
}
