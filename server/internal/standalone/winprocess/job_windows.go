//go:build windows

package winprocess

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const procThreadAttributeJobList uintptr = 0x0002000d

var procIsProcessInJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

// Job owns an anonymous Windows Job Object. Closing it terminates all
// processes assigned to the job through KILL_ON_JOB_CLOSE.
type Job struct {
	mu     sync.Mutex
	handle windows.Handle
	closed bool
}

// Process owns the process handle returned by Start. The main thread handle is
// closed immediately after process creation.
type Process struct {
	mu       sync.Mutex
	handle   windows.Handle
	pid      uint32
	waited   bool
	exitCode uint32
}

// NewJob creates an anonymous kill-on-close Job Object.
func NewJob() (*Job, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create process job: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = windows.CloseHandle(handle)
		}
	}()
	if err := windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		return nil, fmt.Errorf("make process job non-inheritable: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	); err != nil {
		return nil, fmt.Errorf("configure process job: %w", err)
	}
	keep = true
	return &Job{handle: handle}, nil
}

// Start creates a process as a member of the Job. The JOB_LIST process
// attribute makes membership part of CreateProcess itself; there is no
// suspended-process assignment window or breakaway fallback.
func (j *Job) Start(spec StartSpec) (*Process, error) {
	return j.start(spec, nil)
}

// start is split from Start so Windows tests can inject a failure in the
// narrow interval after CreateProcess returns and before Process is exposed.
func (j *Job) start(spec StartSpec, afterCreate func(windows.ProcessInformation) error) (*Process, error) {
	if j == nil {
		return nil, ErrJobClosed
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.handle == windows.InvalidHandle {
		return nil, ErrJobClosed
	}
	if err := validateStartSpec(spec); err != nil {
		return nil, err
	}

	executable, err := windows.UTF16PtrFromString(spec.Path)
	if err != nil {
		return nil, errors.New("encode process executable path")
	}
	argv := make([]string, 0, len(spec.Args)+1)
	argv = append(argv, spec.Path)
	argv = append(argv, spec.Args...)
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return nil, errors.New("encode process command line")
	}
	directory, err := windows.UTF16PtrFromString(spec.Dir)
	if err != nil {
		return nil, errors.New("encode process working directory")
	}
	environment, err := windowsEnvironmentBlock(spec.Env)
	if err != nil {
		return nil, err
	}
	childHandles, err := prepareChildHandles(spec)
	if err != nil {
		return nil, err
	}
	defer closeChildHandles(childHandles)
	inheritedHandles := uniqueChildHandles(childHandles)
	if len(inheritedHandles) == 0 {
		return nil, errors.New("prepare process handle list")
	}

	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, fmt.Errorf("create process attribute list: %w", err)
	}
	defer attributes.Delete()
	jobList := []windows.Handle{j.handle}
	if err := attributes.Update(
		procThreadAttributeJobList,
		unsafe.Pointer(&jobList[0]),
		uintptr(len(jobList))*unsafe.Sizeof(jobList[0]),
	); err != nil {
		return nil, fmt.Errorf("set process job attribute: %w", err)
	}
	if err := attributes.Update(
		windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
		unsafe.Pointer(&inheritedHandles[0]),
		uintptr(len(inheritedHandles))*unsafe.Sizeof(inheritedHandles[0]),
	); err != nil {
		return nil, fmt.Errorf("set process handle attribute: %w", err)
	}

	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  childHandles[0],
			StdOutput: childHandles[1],
			StdErr:    childHandles[2],
		},
		ProcThreadAttributeList: attributes.List(),
	}
	var processInfo windows.ProcessInformation
	createErr := windows.CreateProcess(
		executable,
		commandLine,
		nil,
		nil,
		true,
		windows.CREATE_UNICODE_ENVIRONMENT|windows.EXTENDED_STARTUPINFO_PRESENT,
		&environment[0],
		directory,
		&startup.StartupInfo,
		&processInfo,
	)
	runtime.KeepAlive(executable)
	runtime.KeepAlive(commandLine)
	runtime.KeepAlive(directory)
	runtime.KeepAlive(environment)
	runtime.KeepAlive(jobList)
	runtime.KeepAlive(childHandles)
	runtime.KeepAlive(inheritedHandles)
	if createErr != nil {
		return nil, fmt.Errorf("create process: %w", createErr)
	}
	// The process handle is retained by Process; the main thread handle is not.
	_ = windows.CloseHandle(processInfo.Thread)
	if processInfo.Process == 0 || processInfo.Process == windows.InvalidHandle {
		return nil, errors.New("create process returned an invalid process handle")
	}
	if afterCreate != nil {
		if err := afterCreate(processInfo); err != nil {
			cleanupCreatedProcess(processInfo.Process)
			return nil, fmt.Errorf("after-create process hook: %w", err)
		}
	}
	inJob, err := processHandleInJob(processInfo.Process, j.handle)
	if err != nil {
		cleanupCreatedProcess(processInfo.Process)
		return nil, fmt.Errorf("verify process job membership: %w", err)
	}
	if !inJob {
		cleanupCreatedProcess(processInfo.Process)
		return nil, errors.New("created process is outside its Job")
	}
	return &Process{handle: processInfo.Process, pid: processInfo.ProcessId}, nil
}

// Close releases the Job handle. Windows then enforces KILL_ON_JOB_CLOSE for
// every process in the Job. Close is idempotent after the first call.
func (j *Job) Close() error {
	if j == nil {
		return ErrJobClosed
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.handle == windows.InvalidHandle {
		j.closed = true
		return nil
	}
	handle := j.handle
	j.handle = windows.InvalidHandle
	j.closed = true
	if err := windows.CloseHandle(handle); err != nil {
		return fmt.Errorf("close process job: %w", err)
	}
	return nil
}

// PID returns the process identifier assigned by CreateProcess.
func (p *Process) PID() int {
	if p == nil {
		return 0
	}
	return int(p.pid)
}

// IsInJob reports whether the process currently belongs to any Job Object.
// It is primarily useful to assert the CreateProcess membership contract.
func (p *Process) IsInJob() (bool, error) {
	if p == nil {
		return false, ErrProcessClosed
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handle == windows.InvalidHandle {
		return false, ErrProcessClosed
	}
	return processHandleInJob(p.handle, 0)
}

// Contains reports whether p belongs to this exact Job Object.
func (j *Job) Contains(p *Process) (bool, error) {
	if j == nil || p == nil {
		return false, ErrProcessClosed
	}
	// Wait may hold p.mu until exit; never hold the Job lock while waiting
	// for it, otherwise Job.Close could not terminate that process.
	p.mu.Lock()
	defer p.mu.Unlock()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.handle == windows.InvalidHandle {
		return false, ErrJobClosed
	}
	if p.handle == windows.InvalidHandle {
		return false, ErrProcessClosed
	}
	return processHandleInJob(p.handle, j.handle)
}

// Wait waits for process termination, returns its exit code, and releases the
// process handle. Repeated Wait calls return the cached exit code.
func (p *Process) Wait() (uint32, error) {
	if p == nil {
		return 0, ErrProcessClosed
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.waited {
		return p.exitCode, nil
	}
	if p.handle == windows.InvalidHandle {
		return 0, ErrProcessClosed
	}
	status, err := windows.WaitForSingleObject(p.handle, windows.INFINITE)
	if err != nil {
		return 0, fmt.Errorf("wait for process: %w", err)
	}
	if status != windows.WAIT_OBJECT_0 {
		return 0, fmt.Errorf("wait for process: unexpected wait status 0x%x", status)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(p.handle, &exitCode); err != nil {
		return 0, fmt.Errorf("read process exit code: %w", err)
	}
	closeErr := windows.CloseHandle(p.handle)
	p.handle = windows.InvalidHandle
	p.waited = true
	p.exitCode = exitCode
	if closeErr != nil {
		return 0, fmt.Errorf("close process handle: %w", closeErr)
	}
	return exitCode, nil
}

// Close releases the process handle without terminating the process. Job.Close
// owns process termination; Process.Close only relinquishes this handle.
func (p *Process) Close() error {
	if p == nil {
		return ErrProcessClosed
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handle == windows.InvalidHandle {
		return nil
	}
	handle := p.handle
	p.handle = windows.InvalidHandle
	if err := windows.CloseHandle(handle); err != nil {
		return fmt.Errorf("close process handle: %w", err)
	}
	return nil
}

func validateStartSpec(spec StartSpec) error {
	if !isAbsoluteWindowsPath(spec.Path) {
		return errors.New("process executable must be an absolute path")
	}
	if !isAbsoluteWindowsPath(spec.Dir) {
		return errors.New("process working directory must be an absolute path")
	}
	return nil
}

func isAbsoluteWindowsPath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.VolumeName(path) != ""
}

func prepareChildHandles(spec StartSpec) ([]windows.Handle, error) {
	files := []*os.File{spec.Stdin, spec.Stdout, spec.Stderr}
	access := []uint32{windows.GENERIC_READ, windows.GENERIC_WRITE, windows.GENERIC_WRITE}
	handles := make([]windows.Handle, len(files))
	duplicates := make(map[windows.Handle]windows.Handle, len(files))
	nullSources := make(map[uint32]windows.Handle, 2)
	defer func() {
		for _, source := range nullSources {
			_ = windows.CloseHandle(source)
		}
	}()
	for index, file := range files {
		source, err := standardHandleSource(file, access[index], nullSources)
		if err != nil {
			closeChildHandles(handles[:index])
			return nil, fmt.Errorf("prepare standard handle %d: %w", index, err)
		}
		if handle, ok := duplicates[source]; ok {
			handles[index] = handle
			continue
		}
		handle, err := duplicateChildHandle(source)
		if err != nil {
			closeChildHandles(handles[:index])
			return nil, fmt.Errorf("prepare standard handle %d: %w", index, err)
		}
		duplicates[source] = handle
		handles[index] = handle
	}
	return handles, nil
}

func standardHandleSource(file *os.File, access uint32, nullSources map[uint32]windows.Handle) (windows.Handle, error) {
	if file != nil {
		source := windows.Handle(file.Fd())
		if source == 0 || source == windows.InvalidHandle {
			return windows.InvalidHandle, errors.New("standard handle is invalid")
		}
		return source, nil
	}
	if source, ok := nullSources[access]; ok {
		return source, nil
	}
	source, err := openNullHandle(access)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("open NUL standard handle: %w", err)
	}
	nullSources[access] = source
	return source, nil
}

func duplicateChildHandle(source windows.Handle) (windows.Handle, error) {
	if source == 0 || source == windows.InvalidHandle {
		return windows.InvalidHandle, errors.New("standard handle is invalid")
	}
	var duplicate windows.Handle
	if err := windows.DuplicateHandle(
		windows.CurrentProcess(),
		source,
		windows.CurrentProcess(),
		&duplicate,
		0,
		true,
		windows.DUPLICATE_SAME_ACCESS,
	); err != nil {
		return windows.InvalidHandle, fmt.Errorf("duplicate standard handle: %w", err)
	}
	if duplicate == 0 || duplicate == windows.InvalidHandle {
		return windows.InvalidHandle, errors.New("duplicate standard handle is invalid")
	}
	return duplicate, nil
}

func openNullHandle(access uint32) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString("NUL")
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(
		name,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
}

func closeChildHandles(handles []windows.Handle) {
	closed := make(map[windows.Handle]struct{}, len(handles))
	for _, handle := range handles {
		if handle == 0 || handle == windows.InvalidHandle {
			continue
		}
		if _, ok := closed[handle]; ok {
			continue
		}
		closed[handle] = struct{}{}
		_ = windows.CloseHandle(handle)
	}
}

func uniqueChildHandles(handles []windows.Handle) []windows.Handle {
	unique := make([]windows.Handle, 0, len(handles))
	seen := make(map[windows.Handle]struct{}, len(handles))
	for _, handle := range handles {
		if handle == 0 || handle == windows.InvalidHandle {
			continue
		}
		if _, ok := seen[handle]; ok {
			continue
		}
		seen[handle] = struct{}{}
		unique = append(unique, handle)
	}
	return unique
}

func processHandleInJob(process, job windows.Handle) (bool, error) {
	if process == 0 || process == windows.InvalidHandle {
		return false, ErrProcessClosed
	}
	var inJob int32
	result, _, callErr := procIsProcessInJob.Call(
		uintptr(process),
		uintptr(job),
		uintptr(unsafe.Pointer(&inJob)),
	)
	if result == 0 {
		if callErr == nil {
			callErr = errors.New("IsProcessInJob failed")
		}
		return false, callErr
	}
	return inJob != 0, nil
}

func cleanupCreatedProcess(handle windows.Handle) {
	if handle == 0 || handle == windows.InvalidHandle {
		return
	}
	_ = windows.TerminateProcess(handle, 1)
	_, _ = windows.WaitForSingleObject(handle, 5000)
	_ = windows.CloseHandle(handle)
}

type environmentEntry struct {
	value string
	key   string
}

func windowsEnvironmentBlock(env []string) ([]uint16, error) {
	if env == nil {
		env = os.Environ()
	}
	entries := make([]environmentEntry, 0, len(env))
	seen := make(map[string]struct{}, len(env))
	for index := len(env) - 1; index >= 0; index-- {
		value := env[index]
		if strings.IndexByte(value, 0) >= 0 {
			return nil, fmt.Errorf("environment entry %d contains NUL", index)
		}
		key, ok := windowsEnvironmentKey(value)
		if !ok {
			return nil, fmt.Errorf("environment entry %d is invalid", index)
		}
		normalized := strings.ToLower(key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		entries = append(entries, environmentEntry{value: value, key: key})
	}
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	sort.SliceStable(entries, func(left, right int) bool {
		return strings.ToLower(entries[left].key) < strings.ToLower(entries[right].key)
	})
	if len(entries) == 0 {
		return []uint16{0, 0}, nil
	}
	block := make([]uint16, 0, len(entries)+1)
	for _, entry := range entries {
		encoded, err := windows.UTF16FromString(entry.value)
		if err != nil {
			return nil, errors.New("encode environment block")
		}
		block = append(block, encoded...)
	}
	block = append(block, 0)
	return block, nil
}

func windowsEnvironmentKey(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	separator := strings.IndexByte(value, '=')
	if separator == 0 {
		rest := strings.IndexByte(value[1:], '=')
		if rest < 0 {
			return "", false
		}
		separator = rest + 1
	}
	if separator <= 0 {
		return "", false
	}
	return value[:separator], true
}
