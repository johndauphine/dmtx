//go:build windows

package config

import (
	"context"
	"errors"
	"fmt"
	"math"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsMemoryProbe struct{}

type windowsMemoryStatusEx struct {
	Length                uint32
	MemoryLoad            uint32
	TotalPhysical         uint64
	AvailablePhysical     uint64
	TotalPageFile         uint64
	AvailablePageFile     uint64
	TotalVirtual          uint64
	AvailableVirtual      uint64
	AvailableExtendedVirt uint64
}

var globalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// NewSystemMemoryProbe returns the Windows physical-memory and Job Object
// probe. A finite Job Object limit is treated as an additional upper bound.
func NewSystemMemoryProbe() MemoryProbe {
	return windowsMemoryProbe{}
}

func (windowsMemoryProbe) ProbeMemory(ctx context.Context) (MemorySnapshot, error) {
	if err := ctx.Err(); err != nil {
		return MemorySnapshot{}, err
	}
	var status windowsMemoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	ok, _, callErr := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ok == 0 {
		return MemorySnapshot{}, fmt.Errorf("read Windows host memory evidence: %w", callErr)
	}
	capacity, err := finiteWindowsBytes("physical capacity", status.TotalPhysical)
	if err != nil {
		return MemorySnapshot{}, err
	}
	available, err := finiteWindowsBytes("physical availability", status.AvailablePhysical)
	if err != nil {
		return MemorySnapshot{}, err
	}
	processLimit, err := readWindowsJobMemoryEvidence()
	if err != nil {
		return MemorySnapshot{}, err
	}
	return MemorySnapshot{
		HostCapacityBytes:  capacity,
		HostAvailableBytes: available,
		CgroupV2:           CgroupMemoryEvidence{State: CgroupLimitAbsent},
		CgroupV1:           CgroupMemoryEvidence{State: CgroupLimitAbsent},
		ProcessLimit:       processLimit,
	}, nil
}

func finiteWindowsBytes(label string, value uint64) (int64, error) {
	if value == 0 || value > math.MaxInt64 {
		return 0, fmt.Errorf("Windows host memory %s is not safely finite", label)
	}
	return int64(value), nil
}

func readWindowsJobMemoryEvidence() (CgroupMemoryEvidence, error) {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	err := windows.QueryInformationJobObject(
		0,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	)
	if errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		return CgroupMemoryEvidence{State: CgroupLimitAbsent}, nil
	}
	if err != nil {
		return CgroupMemoryEvidence{}, fmt.Errorf("read Windows Job Object memory evidence: %w", err)
	}
	flags := info.BasicLimitInformation.LimitFlags
	switch {
	case flags&windows.JOB_OBJECT_LIMIT_JOB_MEMORY != 0:
		return finiteWindowsJobLimit(info.JobMemoryLimit, info.PeakJobMemoryUsed)
	case flags&windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY != 0:
		return finiteWindowsJobLimit(info.ProcessMemoryLimit, info.PeakProcessMemoryUsed)
	default:
		return CgroupMemoryEvidence{State: CgroupLimitAbsent}, nil
	}
}

func finiteWindowsJobLimit(limit, peak uintptr) (CgroupMemoryEvidence, error) {
	if uint64(limit) == 0 || uint64(limit) > math.MaxInt64 || uint64(peak) > math.MaxInt64 {
		return CgroupMemoryEvidence{}, fmt.Errorf("Windows Job Object memory evidence is not safely finite")
	}
	return CgroupMemoryEvidence{
		State:        CgroupLimitFinite,
		LimitBytes:   int64(limit),
		CurrentBytes: int64(peak),
	}, nil
}
