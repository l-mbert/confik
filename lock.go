package main

import (
	"fmt"
	"os"
	"time"
)

type FileLock struct {
	file *os.File
}

func acquireLock(lockPath string) (*FileLock, error) {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	locked, err := tryLockFile(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !locked {
		fmt.Fprintln(os.Stderr, "confik: another instance is running, waiting for lock...")
		if err := lockFile(file); err != nil {
			_ = file.Close()
			return nil, err
		}
	}

	_ = writeLockMetadata(file)

	return &FileLock{file: file}, nil
}

func (l *FileLock) Unlock() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unlockFile(l.file)
	_ = l.file.Close()
	l.file = nil
	return err
}

func writeLockMetadata(file *os.File) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	pid := os.Getpid()
	stamp := time.Now().UTC().Format(time.RFC3339)
	_, err := fmt.Fprintf(file, "pid=%d\ncreated_at=%s\n", pid, stamp)
	if err != nil {
		return err
	}
	return file.Sync()
}
