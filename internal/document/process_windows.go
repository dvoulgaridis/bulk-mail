//go:build windows

package document

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsProcessExitWait = 5 * time.Second

type jobObjectBasicAccountingInformation struct {
	totalUserTime             int64
	totalKernelTime           int64
	thisPeriodTotalUserTime   int64
	thisPeriodTotalKernelTime int64
	totalPageFaultCount       uint32
	totalProcesses            uint32
	activeProcesses           uint32
	totalTerminatedProcesses  uint32
}

func runOwnedCommand(command *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create LibreOffice job object: %w", err)
	}
	defer windows.CloseHandle(job)

	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	result, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	)
	if err != nil {
		return fmt.Errorf("configure LibreOffice job object: %w", err)
	}
	if result == 0 {
		return errors.New("configure LibreOffice job object: operation failed")
	}

	command.Cancel = func() error {
		jobErr := windows.TerminateJobObject(job, 1)
		if command.Process == nil {
			if jobErr != nil {
				return jobErr
			}
			return os.ErrProcessDone
		}
		processErr := command.Process.Kill()
		if jobErr == nil || errors.Is(processErr, os.ErrProcessDone) {
			return nil
		}
		return errors.Join(jobErr, processErr)
	}

	if err := command.Start(); err != nil {
		return err
	}
	// os/exec does not expose the primary thread for suspended creation, so assign
	// immediately and fail closed if ownership cannot be established.
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(command.Process.Pid),
	)
	if err != nil {
		return stopUnownedProcess(command, fmt.Errorf("open LibreOffice process: %w", err))
	}
	assignErr := windows.AssignProcessToJobObject(job, process)
	closeErr := windows.CloseHandle(process)
	if assignErr != nil {
		return stopUnownedProcess(command, fmt.Errorf("assign LibreOffice process to job object: %w", assignErr))
	}
	if closeErr != nil {
		return stopUnownedProcess(command, fmt.Errorf("close LibreOffice process handle: %w", closeErr))
	}

	waitErr := command.Wait()
	terminateErr := windows.TerminateJobObject(job, 1)
	return errors.Join(waitErr, terminateErr, waitForJobExit(job, windowsProcessExitWait))
}

func stopUnownedProcess(command *exec.Cmd, cause error) error {
	killErr := command.Process.Kill()
	waitErr := command.Wait()
	if errors.Is(killErr, os.ErrProcessDone) {
		killErr = nil
	}
	return errors.Join(cause, killErr, waitErr)
}

func waitForJobExit(job windows.Handle, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var accounting jobObjectBasicAccountingInformation
		if err := windows.QueryInformationJobObject(
			job,
			windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&accounting)),
			uint32(unsafe.Sizeof(accounting)),
			nil,
		); err != nil {
			return fmt.Errorf("query LibreOffice job object: %w", err)
		}
		if accounting.activeProcesses == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("wait for LibreOffice job object: timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
