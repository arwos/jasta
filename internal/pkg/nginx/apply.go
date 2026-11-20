package nginx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

type Runner interface {
	Run(context.Context, string, ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

var applyMu sync.Mutex

// Apply tests the candidate in isolation, atomically installs it, then tests
// the complete Nginx configuration. Any failure restores the prior include
// before returning. Reload is attempted only after both syntax checks pass.
func Apply(ctx context.Context, settings Settings, content []byte, runner Runner) (err error) {
	settings.SetDefaults()
	if err = settings.Validate(); err != nil {
		return err
	}
	if len(content) == 0 {
		return errors.New("nginx config is empty")
	}
	if runner == nil {
		runner = execRunner{}
	}
	applyMu.Lock()
	defer applyMu.Unlock()
	directory := filepath.Dir(settings.ConfigPath)
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("inspect nginx config directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("nginx config parent is not a directory")
	}
	if err = testCandidate(ctx, settings.NginxBinary, content, directory, runner); err != nil {
		return fmt.Errorf("test candidate nginx config: %w", err)
	}

	previous, previousMode, hadPrevious, err := readPrevious(settings.ConfigPath)
	if err != nil {
		return err
	}
	if err = atomicWrite(settings.ConfigPath, content, 0o640); err != nil {
		return fmt.Errorf("install nginx config candidate: %w", err)
	}
	rollback := func() error {
		if hadPrevious {
			return atomicWrite(settings.ConfigPath, previous, previousMode)
		}
		if removeErr := os.Remove(settings.ConfigPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
		return nil
	}
	if err = runner.Run(ctx, settings.NginxBinary, "-t"); err != nil {
		if rollbackErr := rollback(); rollbackErr != nil {
			return errors.Join(fmt.Errorf("test installed nginx config: %w", err), fmt.Errorf("restore previous nginx config: %w", rollbackErr))
		}
		return fmt.Errorf("test installed nginx config: %w", err)
	}
	if err = runner.Run(ctx, settings.Systemctl, "reload", "nginx"); err != nil {
		if rollbackErr := rollback(); rollbackErr != nil {
			return errors.Join(fmt.Errorf("reload nginx: %w", err), fmt.Errorf("restore previous nginx config: %w", rollbackErr))
		}
		return fmt.Errorf("reload nginx: %w", err)
	}
	return nil
}

func testCandidate(ctx context.Context, binary string, content []byte, directory string, runner Runner) error {
	candidate, err := os.CreateTemp(directory, ".jasta-candidate-*.conf")
	if err != nil {
		return fmt.Errorf("create temporary nginx candidate: %w", err)
	}
	candidatePath := candidate.Name()
	defer func() { _ = os.Remove(candidatePath) }()
	if err = candidate.Chmod(0o600); err == nil {
		_, err = candidate.Write(content)
	}
	if closeErr := candidate.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write temporary nginx candidate: %w", err)
	}
	main, err := os.CreateTemp(directory, ".jasta-test-*.conf")
	if err != nil {
		return fmt.Errorf("create temporary nginx test config: %w", err)
	}
	mainPath := main.Name()
	defer func() { _ = os.Remove(mainPath) }()
	_, err = fmt.Fprintf(main, "pid %s;\nerror_log stderr;\nevents {}\nhttp { include %s; }\n", nginxQuote(filepath.Join(directory, ".jasta-test.pid")), nginxQuote(candidatePath))
	if closeErr := main.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write temporary nginx test config: %w", err)
	}
	if err = runner.Run(ctx, binary, "-t", "-c", mainPath); err != nil {
		return fmt.Errorf("nginx candidate syntax check failed: %w", err)
	}
	return nil
}

func readPrevious(path string) ([]byte, os.FileMode, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, fmt.Errorf("inspect existing nginx config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, false, errors.New("existing nginx config is not a regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, false, fmt.Errorf("read existing nginx config: %w", err)
	}
	return content, info.Mode().Perm(), true, nil
}

func atomicWrite(path string, content []byte, mode os.FileMode) (err error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".jasta-apply-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() {
		_ = os.Remove(temporary)
	}()
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(content)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
