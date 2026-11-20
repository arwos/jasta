package archive

import (
	"errors"
	"fmt"
)

const (
	DefaultMaxArchiveBytes = 100 << 20
	DefaultMaxUnpacked     = 1 << 30
	DefaultMaxEntries      = 100_000
)

// Limits bounds the uploaded archive, extracted file data, and number of
// logical entries. Zero values are replaced with the safe defaults.
type Limits struct {
	MaxArchiveBytes int64 `yaml:"max_archive_bytes" json:"max_archive_bytes"`
	MaxUnpacked     int64 `yaml:"max_unpacked_bytes" json:"max_unpacked_bytes"`
	MaxEntries      int   `yaml:"max_entries" json:"max_entries"`
}

func DefaultLimits() Limits {
	return Limits{
		MaxArchiveBytes: DefaultMaxArchiveBytes,
		MaxUnpacked:     DefaultMaxUnpacked,
		MaxEntries:      DefaultMaxEntries,
	}
}

func (l *Limits) SetDefaults() {
	defaults := DefaultLimits()
	if l.MaxArchiveBytes == 0 {
		l.MaxArchiveBytes = defaults.MaxArchiveBytes
	}
	if l.MaxUnpacked == 0 {
		l.MaxUnpacked = defaults.MaxUnpacked
	}
	if l.MaxEntries == 0 {
		l.MaxEntries = defaults.MaxEntries
	}
}

func (l Limits) Validate() error {
	switch {
	case l.MaxArchiveBytes <= 0:
		return errors.New("archive.max_archive_bytes must be greater than zero")
	case l.MaxUnpacked <= 0:
		return errors.New("archive.max_unpacked_bytes must be greater than zero")
	case l.MaxEntries <= 0:
		return errors.New("archive.max_entries must be greater than zero")
	default:
		return nil
	}
}

func (l Limits) withDefaults() (Limits, error) {
	l.SetDefaults()
	if err := l.Validate(); err != nil {
		return Limits{}, fmt.Errorf("invalid archive limits: %w", err)
	}
	return l, nil
}
