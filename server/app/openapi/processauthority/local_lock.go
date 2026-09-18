package processauthority

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrLocalAuthorityUnavailable = errors.New("local process authority unavailable")
	ErrLocalAuthorityBusy        = errors.New("local process authority already owned")
)

const lockName = "api-authority.lock"

type localDomain struct {
	Version      int    `json:"version"`
	DomainID     string `json:"domainID"`
	FileIdentity string `json:"fileIdentity"`
}

// LocalLock is an OS-backed root lifetime resource, not takeover permission.
// Root must retain it until all effect owners join. Stop/Reload of a component
// cannot release it. The later DB authority must also bind this domain and all
// owner generations; an acquired file lock alone never authorizes recovery.
type LocalLock struct{ state *localLockState }

type localLockState struct {
	mu                sync.Mutex
	path              string
	root              *os.Root
	dir, file         *os.File
	dirInfo, fileInfo os.FileInfo
	domain            string
	encoded           []byte
	closed, poisoned  bool
}

// AcquireLocalLock requires a pre-provisioned private directory on a supported
// local persistent filesystem. It never replaces/unlinks an existing lock file
// or repairs invalid state. Directory provisioning and DB binding belong to root.
func AcquireLocalLock(path string) (*LocalLock, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrLocalAuthorityUnavailable
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrLocalAuthorityUnavailable
	}
	dir, err := os.Open(path)
	if err != nil {
		return nil, ErrLocalAuthorityUnavailable
	}
	s := &localLockState{path: path, dir: dir, dirInfo: info}
	retained := false
	defer func() {
		if !retained {
			s.close()
		}
	}()
	if !secureLocalFile(dir, true) {
		return nil, ErrLocalAuthorityUnavailable
	}
	if actual, err := dir.Stat(); err != nil || !os.SameFile(info, actual) {
		return nil, ErrLocalAuthorityUnavailable
	}
	s.root, err = os.OpenRoot(path)
	if err != nil {
		return nil, ErrLocalAuthorityUnavailable
	}
	if actual, err := s.root.Stat("."); err != nil || !os.SameFile(info, actual) {
		return nil, ErrLocalAuthorityUnavailable
	}
	created := true
	s.file, err = s.root.OpenFile(lockName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		created = false
		before, statErr := s.root.Lstat(lockName)
		if statErr != nil || !before.Mode().IsRegular() {
			return nil, ErrLocalAuthorityUnavailable
		}
		s.file, err = s.root.OpenFile(lockName, os.O_RDWR, 0)
	}
	if err != nil || !secureLocalFile(s.file, false) {
		return nil, ErrLocalAuthorityUnavailable
	}
	if err = lockLocalFile(s.file); err != nil {
		return nil, err
	}
	s.fileInfo, err = s.file.Stat()
	if err != nil {
		return nil, ErrLocalAuthorityUnavailable
	}
	fileIdentity, err := localFileIdentity(s.file)
	if err != nil {
		return nil, ErrLocalAuthorityUnavailable
	}
	if created {
		var random [16]byte
		if _, err = rand.Read(random[:]); err != nil {
			return nil, ErrLocalAuthorityUnavailable
		}
		encoded, err := json.Marshal(localDomain{Version: 1, DomainID: hex.EncodeToString(random[:]), FileIdentity: fileIdentity})
		if err != nil {
			return nil, ErrLocalAuthorityUnavailable
		}
		encoded = append(encoded, '\n')
		if n, err := s.file.WriteAt(encoded, 0); err != nil || n != len(encoded) {
			return nil, ErrLocalAuthorityUnavailable
		}
		if s.file.Sync() != nil || syncLocalDirectory(dir) != nil {
			return nil, ErrLocalAuthorityUnavailable
		}
	}
	s.encoded, err = readLocalDomain(s.file)
	if err != nil {
		return nil, ErrLocalAuthorityUnavailable
	}
	var domain localDomain
	decoder := json.NewDecoder(bytes.NewReader(s.encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&domain) != nil || domain.Version != 1 || len(domain.DomainID) != 32 || domain.FileIdentity != fileIdentity {
		return nil, ErrLocalAuthorityUnavailable
	}
	raw, err := hex.DecodeString(domain.DomainID)
	if err != nil || hex.EncodeToString(raw) != domain.DomainID || bytes.Equal(raw, make([]byte, 16)) {
		return nil, ErrLocalAuthorityUnavailable
	}
	canonical, err := json.Marshal(domain)
	if err != nil || !bytes.Equal(append(canonical, '\n'), s.encoded) {
		return nil, ErrLocalAuthorityUnavailable
	}
	s.domain = domain.DomainID
	if !s.check() {
		return nil, ErrLocalAuthorityUnavailable
	}
	retained = true
	return &LocalLock{state: s}, nil
}

func readLocalDomain(file *os.File) ([]byte, error) {
	var data [513]byte
	n, err := file.ReadAt(data[:], 0)
	if (err != nil && !errors.Is(err, io.EOF)) || n == 0 || n > 512 {
		return nil, ErrLocalAuthorityUnavailable
	}
	return data[:n], nil
}

func (s *localLockState) check() bool {
	if s.closed || s.poisoned {
		return false
	}
	dirInfo, err := os.Lstat(s.path)
	if err != nil || !dirInfo.IsDir() || !os.SameFile(s.dirInfo, dirInfo) || !secureLocalFile(s.dir, true) {
		s.poisoned = true
		return false
	}
	fileInfo, err := s.root.Lstat(lockName)
	if err != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(s.fileInfo, fileInfo) || !secureLocalFile(s.file, false) {
		s.poisoned = true
		return false
	}
	encoded, err := readLocalDomain(s.file)
	if err != nil || !bytes.Equal(s.encoded, encoded) {
		s.poisoned = true
		return false
	}
	return true
}

// DomainID revalidates the held file and directory identity. Any mismatch is
// sticky until root exits; repairing a file cannot revive poisoned authority.
func (l *LocalLock) DomainID() (string, error) {
	if l == nil || l.state == nil {
		return "", ErrLocalAuthorityUnavailable
	}
	s := l.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.check() {
		return "", ErrLocalAuthorityUnavailable
	}
	return s.domain, nil
}

// Check must be called at every effect authorization CAS, not only at startup.
// A cached domain string does not prove that the root still owns authority.
func (l *LocalLock) Check() error {
	_, err := l.DomainID()
	return err
}

func (s *localLockState) close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.file != nil {
		err = errors.Join(err, s.file.Close())
	}
	if s.root != nil {
		err = errors.Join(err, s.root.Close())
	}
	if s.dir != nil {
		err = errors.Join(err, s.dir.Close())
	}
	return err
}

// Close is reserved for the root's final joined shutdown, never a component
// restart. Copies share lifetime state. The persistent file is never removed.
func (l *LocalLock) Close() error {
	if l == nil || l.state == nil {
		return nil
	}
	l.state.mu.Lock()
	defer l.state.mu.Unlock()
	return l.state.close()
}
