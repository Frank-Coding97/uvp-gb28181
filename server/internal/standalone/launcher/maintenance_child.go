package launcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
	"uvplatform.cn/uvp-gb28181/internal/standalone/winprocess"
)

// The caller owns the instance lock until the entire maintenance transaction
// ends. This runner never removes the gate or authorizes a business startup.
func runMaintenanceChild(ctx context.Context, paths standalone.Paths, operationID, purpose, version string) error {
	if ctx == nil {
		return errors.New("maintenance child requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	args, err := maintenanceChildArgs(purpose)
	if err != nil {
		return err
	}
	release, err := standalone.LoadReleaseVersion(paths.InstallDir, version)
	if err != nil {
		return err
	}
	job, err := winprocess.NewJob()
	if err != nil {
		return err
	}
	defer job.Close()
	input, inputWriter, err := os.Pipe()
	if err != nil {
		return err
	}
	defer input.Close()
	defer inputWriter.Close()
	output, outputWriter, err := os.Pipe()
	if err != nil {
		return err
	}
	defer output.Close()
	defer outputWriter.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	frame, err := standalone.IssueMaintenancePermit(paths.InstallDir, operationID, purpose, version)
	if err != nil {
		return err
	}
	defer clear(frame)
	if err := ctx.Err(); err != nil {
		return err
	}
	// Candidate resources must correspond to the selected immutable release.
	paths.ResourceDir, paths.WebDir = release.ResourceDir, release.WebDir
	process, err := job.Start(winprocess.StartSpec{
		NoConsole: true, Path: release.BackendExe, Args: args, Dir: release.ReleaseDir,
		Env: componentEnvironment(paths), Stdin: input, Stdout: outputWriter,
	})
	if err != nil {
		return errors.New("maintenance child could not start")
	}
	defer process.Close()
	_ = input.Close()
	_ = outputWriter.Close()
	var captured maintenanceChildOutput
	outputDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(&captured, output)
		outputDone <- err
	}()
	inputDone := make(chan error, 1)
	go func() {
		_, err := inputWriter.Write(frame)
		_ = inputWriter.Close()
		inputDone <- err
	}()
	type outcome struct {
		code uint32
		err  error
	}
	waited := make(chan outcome, 1)
	go func() {
		code, err := process.Wait()
		waited <- outcome{code, err}
	}()
	bounded, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var result outcome
	select {
	case result = <-waited:
	case <-bounded.Done():
		_ = job.Close()
		result = <-waited
	}
	closeErr := job.Close()
	writeErr, readErr := <-inputDone, <-outputDone
	if err := bounded.Err(); err != nil {
		return err
	}
	if result.err != nil || result.code != 0 || closeErr != nil || writeErr != nil || readErr != nil || captured.overflow {
		return errors.New("maintenance child failed")
	}
	return validateMaintenanceChildOutput(captured.Bytes(), purpose, version)
}

func maintenanceChildArgs(purpose string) ([]string, error) {
	switch purpose {
	case "bootstrap_db":
		return []string{"-bootstrap-db"}, nil
	case "migrate_up":
		return []string{"-migrate-up"}, nil
	case "db_check":
		return []string{"-db-check"}, nil
	case "candidate_health", "revoke_sessions":
		return nil, nil
	default:
		return nil, errors.New("unsupported maintenance child purpose")
	}
}

type maintenanceChildOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *maintenanceChildOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *maintenanceChildOutput) Write(p []byte) (int, error) {
	const limit = 1 << 20
	n := len(p)
	remaining := limit - b.buffer.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.buffer.Write(p)
	return n, nil // Always drain the pipe, including after the capture limit.
}

func validateMaintenanceChildOutput(raw []byte, purpose, version string) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	type completion struct {
		Status  string `json:"status"`
		Purpose string `json:"purpose"`
		Version string `json:"version"`
	}
	var last completion
	for {
		var item json.RawMessage
		err := decoder.Decode(&item)
		if errors.Is(err, io.EOF) {
			break
		}
		last = completion{}
		if err != nil || json.Unmarshal(item, &last) != nil {
			return errors.New("invalid maintenance completion output")
		}
	}
	if last.Status != "maintenance_complete" || last.Purpose != purpose || last.Version != version {
		return errors.New("maintenance completion identity mismatch")
	}
	return nil
}
