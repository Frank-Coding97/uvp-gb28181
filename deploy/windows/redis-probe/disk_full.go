package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxDiskFullVolumeBytes uint64 = 128 * 1024 * 1024

type diskVolumeInfo struct {
	Root       string
	Label      string
	TotalBytes uint64
	FreeBytes  uint64
}

func validateDiskFullVolume(info diskVolumeInfo) error {
	if info.Root == "" {
		return &diskFullTargetError{reason: "volume root is empty"}
	}
	if info.Label != "UVP_P0_TEST" {
		return &diskFullTargetError{reason: "volume label must be exactly UVP_P0_TEST"}
	}
	if info.TotalBytes == 0 {
		return &diskFullTargetError{reason: "volume size is zero"}
	}
	if info.TotalBytes > maxDiskFullVolumeBytes {
		return &diskFullTargetError{reason: "volume size exceeds 128 MiB safety limit"}
	}
	return nil
}

type diskFullTargetError struct {
	reason string
}

func (err *diskFullTargetError) Error() string {
	return err.reason
}

func isNoSpaceError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no space left") ||
		strings.Contains(message, "not enough space") ||
		strings.Contains(message, "disk full") ||
		strings.Contains(message, "disk is full") ||
		strings.Contains(message, "device is full") ||
		strings.Contains(message, "device full") ||
		strings.Contains(message, "磁盘空间不足") ||
		strings.Contains(message, "空间不足") ||
		strings.Contains(message, "没有剩余空间") ||
		strings.Contains(message, "设备已满") ||
		strings.Contains(message, "设备上没有足够的空间") ||
		platformNoSpaceError(err)
}

const (
	diskFillCapBytes   uint64 = 128 * 1024 * 1024
	diskFillChunkBytes        = 1024 * 1024
	diskFillTimeout           = 2 * time.Minute
)

type diskFillResult struct {
	BytesWritten   uint64
	ReachedNoSpace bool
	Files          int
}

type diskFullConfirmedWrite struct {
	Key   string
	Value string
}

func runDiskFullCheck(binary, root string) (result map[string]any) {
	result = map[string]any{
		"status": "not_executed",
		"root":   root,
	}
	if root == "" {
		result["reason"] = "-disk-full-root was not supplied"
		return result
	}
	if !diskFullTestSupported() {
		result["reason"] = "disk-full volume probing requires native Windows"
		return result
	}
	volume, err := inspectDiskFullTarget(root)
	if err != nil {
		return diskFullFailure(result, err.Error())
	}
	result["volume"] = map[string]any{
		"root":        volume.Root,
		"label":       volume.Label,
		"total_bytes": volume.TotalBytes,
		"free_bytes":  volume.FreeBytes,
	}
	if err := validateDiskFullVolume(volume); err != nil {
		return diskFullFailure(result, err.Error())
	}

	caseDir, err := os.MkdirTemp(volume.Root, "uvp redis disk full 中文-")
	if err != nil {
		return diskFullFailure(result, "create marked-volume case directory: "+err.Error())
	}
	caseRemoved := false
	defer func() {
		if !caseRemoved {
			if removeErr := os.RemoveAll(caseDir); removeErr != nil {
				result["cleanup_error"] = removeErr.Error()
				result["status"] = "failed"
				result["reason"] = "remove disk-full case directory: " + removeErr.Error()
			}
		}
	}()

	instance, err := newRedisInstance(binary, caseDir, "redis with spaces", "")
	if err != nil {
		return diskFullFailure(result, "prepare Redis on marked volume: "+err.Error())
	}
	defer func() {
		if stopErr := instance.stop(true); stopErr != nil {
			result["stop_error"] = stopErr.Error()
			result["status"] = "failed"
			result["reason"] = "stop disk-full Redis instance: " + stopErr.Error()
		}
		if removeErr := os.RemoveAll(caseDir); removeErr != nil {
			result["cleanup_error"] = removeErr.Error()
			result["status"] = "failed"
			result["reason"] = "remove disk-full case directory: " + removeErr.Error()
		} else {
			caseRemoved = true
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	client, err := instance.start(ctx)
	cancel()
	if err != nil {
		return diskFullFailure(result, "start Redis on marked volume: "+err.Error())
	}
	defer func() {
		if client != nil {
			_ = client.close()
		}
	}()
	result["redis_workspace"] = instance.workspace

	token, err := randomToken(10)
	if err != nil {
		return diskFullFailure(result, "create disk-full key suffix: "+err.Error())
	}
	confirmedWrites := []diskFullConfirmedWrite{
		{Key: fmt.Sprintf("__uvp_disk_full_confirmed_%s_0", token), Value: "confirmed-before-disk-full-0"},
		{Key: fmt.Sprintf("__uvp_disk_full_confirmed_%s_1", token), Value: "confirmed-before-disk-full-1"},
		{Key: fmt.Sprintf("__uvp_disk_full_confirmed_%s_2", token), Value: "confirmed-before-disk-full-2"},
	}
	result["confirmed_writes"] = len(confirmedWrites)
	for _, expected := range confirmedWrites {
		if _, err := client.do(context.Background(), "SET", expected.Key, expected.Value); err != nil {
			return diskFullFailure(result, "confirmed SET before fill: "+err.Error())
		}
	}

	fillerDir, err := os.MkdirTemp(caseDir, "filler with spaces 中文-")
	if err != nil {
		return diskFullFailure(result, "create fill directory: "+err.Error())
	}
	fillCtx, fillCancel := context.WithTimeout(context.Background(), diskFillTimeout)
	fillResult, fillErr := fillDiskUntilFull(fillCtx, fillerDir, diskFillCapBytes)
	fillCancel()
	result["fill"] = map[string]any{
		"bytes_written":    fillResult.BytesWritten,
		"files":            fillResult.Files,
		"cap_bytes":        diskFillCapBytes,
		"reached_no_space": fillResult.ReachedNoSpace,
	}
	if fillErr != nil {
		removeFillErr := os.RemoveAll(fillerDir)
		result["filler_released"] = removeFillErr == nil
		if removeFillErr != nil {
			result["filler_release_error"] = removeFillErr.Error()
		}
		return diskFullFailure(result, "fill marked volume: "+fillErr.Error())
	}
	if !fillResult.ReachedNoSpace {
		removeFillErr := os.RemoveAll(fillerDir)
		result["filler_released"] = removeFillErr == nil
		if removeFillErr != nil {
			result["filler_release_error"] = removeFillErr.Error()
		}
		return diskFullFailure(result, "volume did not report ENOSPC before 128 MiB fill cap")
	}

	writeResult, writeErr := writeAOFUntilRejected(client, instance, token, &confirmedWrites)
	result["aof_write_under_full"] = writeResult
	result["confirmed_writes"] = len(confirmedWrites)
	removeFillErr := os.RemoveAll(fillerDir)
	result["filler_released"] = removeFillErr == nil
	if removeFillErr != nil {
		result["filler_release_error"] = removeFillErr.Error()
	}

	// The full-volume write may intentionally terminate Redis. Release the
	// filler first, then always attempt a fresh process and recovery checks so
	// an expected AOF failure cannot skip durability validation.
	recovery := map[string]any{
		"status":           "failed",
		"confirmed_writes": len(confirmedWrites),
	}
	recoveryFailures := make([]string, 0)
	if closeErr := client.close(); closeErr != nil {
		result["client_close_error"] = closeErr.Error()
		recoveryFailures = append(recoveryFailures, "close full-volume Redis connection: "+closeErr.Error())
	}
	client = nil
	restartCtx, restartCancel := context.WithTimeout(context.Background(), startupTimeout)
	restartedClient, restartErr := instance.restart(restartCtx)
	restartCancel()
	if restartErr != nil {
		recoveryFailures = append(recoveryFailures, "restart after releasing fill files: "+restartErr.Error())
	} else {
		client = restartedClient
		for _, expected := range confirmedWrites {
			readCtx, readCancel := context.WithTimeout(context.Background(), probeTimeout)
			recovered, readErr := client.do(readCtx, "GET", expected.Key)
			readCancel()
			got, valueErr := recovered.stringValue()
			if readErr != nil || valueErr != nil || got != expected.Value {
				recoveryFailures = append(recoveryFailures, fmt.Sprintf("confirmed key %q value=%q want=%q command_err=%v value_err=%v", expected.Key, got, expected.Value, readErr, valueErr))
			}
		}
		recoveryKey := "__uvp_disk_full_recovered_" + token
		if _, err := client.do(context.Background(), "SET", recoveryKey, "recovered"); err != nil {
			recoveryFailures = append(recoveryFailures, "recovery SET: "+err.Error())
		} else {
			info, infoErr := redisInfo(client, "persistence")
			if infoErr != nil || info["aof_last_write_status"] != "ok" {
				recoveryFailures = append(recoveryFailures, fmt.Sprintf("aof_last_write_status=%q info_err=%v", info["aof_last_write_status"], infoErr))
			} else {
				recovery["aof_last_write_status"] = info["aof_last_write_status"]
			}
		}
		cleanupKeys := []string{recoveryKey}
		for _, expected := range confirmedWrites {
			cleanupKeys = append(cleanupKeys, expected.Key)
		}
		if _, err := client.do(context.Background(), append([]string{"DEL"}, cleanupKeys...)...); err != nil {
			recoveryFailures = append(recoveryFailures, "recovery cleanup: "+err.Error())
		}
	}
	result["recovery"] = recovery
	if after, afterErr := inspectDiskFullTarget(volume.Root); afterErr != nil {
		recoveryFailures = append(recoveryFailures, "inspect volume after fill release: "+afterErr.Error())
	} else {
		result["free_bytes_after_release"] = after.FreeBytes
	}
	if len(recoveryFailures) > 0 {
		recovery["reason"] = strings.Join(recoveryFailures, "; ")
		return diskFullFailure(result, recovery["reason"].(string))
	}
	recovery["status"] = "passed"
	if writeErr != nil {
		return diskFullFailure(result, writeErr.Error())
	}
	if removeFillErr != nil {
		return diskFullFailure(result, "release fill files: "+removeFillErr.Error())
	}
	result["status"] = "passed"
	return result
}

func diskFullFailure(result map[string]any, reason string) map[string]any {
	result["status"] = "failed"
	result["reason"] = reason
	return result
}

func fillDiskUntilFull(ctx context.Context, directory string, capBytes uint64) (diskFillResult, error) {
	buffer := make([]byte, diskFillChunkBytes)
	for fileIndex := 0; ; fileIndex++ {
		if err := ctx.Err(); err != nil {
			return diskFillResult{BytesWritten: uint64(fileIndex) * uint64(diskFillChunkBytes), Files: fileIndex}, fmt.Errorf("fill timeout: %w", err)
		}
		if fileIndex >= int(capBytes/uint64(diskFillChunkBytes))+1 {
			return diskFillResult{BytesWritten: uint64(fileIndex) * uint64(diskFillChunkBytes), Files: fileIndex}, nil
		}
		remaining := capBytes
		if uint64(fileIndex)*uint64(diskFillChunkBytes) < capBytes {
			remaining = capBytes - uint64(fileIndex)*uint64(diskFillChunkBytes)
		} else {
			remaining = 0
		}
		if remaining == 0 {
			return diskFillResult{BytesWritten: uint64(fileIndex) * uint64(diskFillChunkBytes), Files: fileIndex}, nil
		}
		filePath := filepath.Join(directory, fmt.Sprintf("block-%04d.bin", fileIndex))
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			if isNoSpaceError(err) {
				return diskFillResult{BytesWritten: uint64(fileIndex) * uint64(diskFillChunkBytes), ReachedNoSpace: true, Files: fileIndex}, nil
			}
			return diskFillResult{BytesWritten: uint64(fileIndex) * uint64(diskFillChunkBytes), Files: fileIndex}, err
		}
		fileBytes := uint64(0)
		for fileBytes < remaining {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return diskFillResult{BytesWritten: uint64(fileIndex)*uint64(diskFillChunkBytes) + fileBytes, Files: fileIndex + 1}, fmt.Errorf("fill timeout: %w", err)
			}
			chunkSize := uint64(len(buffer))
			if remaining-fileBytes < chunkSize {
				chunkSize = remaining - fileBytes
			}
			written, writeErr := file.Write(buffer[:chunkSize])
			fileBytes += uint64(written)
			if writeErr != nil {
				_ = file.Close()
				if isNoSpaceError(writeErr) {
					return diskFillResult{BytesWritten: uint64(fileIndex)*uint64(diskFillChunkBytes) + fileBytes, ReachedNoSpace: true, Files: fileIndex + 1}, nil
				}
				return diskFillResult{BytesWritten: uint64(fileIndex)*uint64(diskFillChunkBytes) + fileBytes, Files: fileIndex + 1}, writeErr
			}
			if written != int(chunkSize) {
				_ = file.Close()
				return diskFillResult{BytesWritten: uint64(fileIndex)*uint64(diskFillChunkBytes) + fileBytes, Files: fileIndex + 1}, fmt.Errorf("short fill write: wrote %d of %d bytes", written, chunkSize)
			}
		}
		if err := file.Close(); err != nil {
			if isNoSpaceError(err) {
				return diskFillResult{BytesWritten: uint64(fileIndex)*uint64(diskFillChunkBytes) + fileBytes, ReachedNoSpace: true, Files: fileIndex + 1}, nil
			}
			return diskFillResult{BytesWritten: uint64(fileIndex)*uint64(diskFillChunkBytes) + fileBytes, Files: fileIndex + 1}, err
		}
	}
}

func writeAOFUntilRejected(client *redisClient, instance *redisInstance, token string, confirmed *[]diskFullConfirmedWrite) (map[string]any, error) {
	result := map[string]any{"attempts": 0, "status": "failed"}
	payload := strings.Repeat("a", 64*1024)
	for attempt := 1; attempt <= 16; attempt++ {
		result["attempts"] = attempt
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		key := fmt.Sprintf("__uvp_disk_full_write_%s_%d", token, attempt)
		_, err := client.do(ctx, "SET", key, payload)
		cancel()
		if err == nil {
			*confirmed = append(*confirmed, diskFullConfirmedWrite{Key: key, Value: payload})
			info, infoErr := redisInfo(client, "persistence")
			if infoErr != nil {
				result["reason"] = "SET replied success but persistence status could not be read: " + infoErr.Error()
				result["process_exit"] = captureAOFProcessExit(instance).json()
				return result, errors.New(result["reason"].(string))
			}
			if info["aof_last_write_status"] == "err" {
				result["reason"] = "SET replied success while aof_last_write_status=err"
				result["process_exit"] = captureAOFProcessExit(instance).json()
				return result, errors.New(result["reason"].(string))
			}
			continue
		}
		result["observed_error"] = err.Error()
		evidence := captureAOFProcessExit(instance)
		result["process_exit"] = evidence.json()
		if validAOFExitEvidence(evidence) {
			result["status"] = "passed"
			return result, nil
		}
		result["reason"] = "Redis write did not produce exit-code-1 plus explicit AOF/ENOSPC log evidence: " + err.Error()
		return result, errors.New(result["reason"].(string))
	}
	result["reason"] = "Redis accepted 16 writes after the marked volume reported ENOSPC"
	result["process_exit"] = captureAOFProcessExit(instance).json()
	return result, errors.New(result["reason"].(string))
}

func captureAOFProcessExit(instance *redisInstance) processExitEvidence {
	if instance.waitForExit(5 * time.Second) {
		return instance.processExitEvidence()
	}
	return instance.processExitEvidence()
}

func validAOFExitEvidence(evidence processExitEvidence) bool {
	if !evidence.Exited || evidence.ExitCode != 1 {
		return false
	}
	log := strings.ToUpper(evidence.Log)
	hasAOF := strings.Contains(log, "AOF")
	hasNoSpace := strings.Contains(log, "ENOSPC") ||
		strings.Contains(log, "NO SPACE") ||
		strings.Contains(log, "NOT ENOUGH SPACE") ||
		strings.Contains(log, "DISK FULL") ||
		strings.Contains(log, "磁盘空间不足") ||
		strings.Contains(log, "空间不足") ||
		strings.Contains(log, "没有剩余空间")
	hasExit := strings.Contains(log, "EXIT")
	return hasAOF && hasNoSpace && hasExit
}
