// Copyright (c) 2012 VMware, Inc.

package sigar

// #include <stdlib.h>
// #include <windows.h>
import "C"

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/StackExchange/wmi"
)

var (
	modpsapi    = syscall.NewLazyDLL("psapi.dll")
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")

	procEnumProcesses            = modpsapi.NewProc("EnumProcesses")
	procGetProcessMemoryInfo     = modpsapi.NewProc("GetProcessMemoryInfo")
	procGetProcessTimes          = modkernel32.NewProc("GetProcessTimes")
	procGetProcessImageFileName  = modpsapi.NewProc("GetProcessImageFileNameA")
	procCreateToolhelp32Snapshot = modkernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32First           = modkernel32.NewProc("Process32FirstW")

	procGetDiskFreeSpaceExW     = modkernel32.NewProc("GetDiskFreeSpaceExW")
	procGetLogicalDriveStringsW = modkernel32.NewProc("GetLogicalDriveStringsW")
	procGetDriveType            = modkernel32.NewProc("GetDriveTypeW")
	provGetVolumeInformation    = modkernel32.NewProc("GetVolumeInformationW")
)

const (
	PROCESS_ALL_ACCESS = 0x001f0fff
	TH32CS_SNAPPROCESS = 0x02
	MAX_PATH           = 260
)

type PROCESS_MEMORY_COUNTERS_EX struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

// PROCESSENTRY32 is the Windows API structure that contains a process's
// information. Do not modify or reorder.
// https://msdn.microsoft.com/en-us/library/windows/desktop/ms684839(v=vs.85).aspx
type PROCESSENTRY32 struct {
	Size              uint32
	CntUsage          uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	CntThreads        uint32
	ParentProcessID   uint32
	PriorityClassBase int32
	Flags             uint32
	ExeFile           [MAX_PATH]uint16
}

// Win32_Process represents a process on the Windows operating system. If
// additional fields are added here (that match the Windows struct) they will
// automatically be populated when calling getWin32Process.
// https://msdn.microsoft.com/en-us/library/windows/desktop/aa394372(v=vs.85).aspx
type Win32_Process struct {
	CommandLine string
}

func init() {
}

func (self *LoadAverage) Get() error {
	return nil
}

func (self *Uptime) Get() error {
	return nil
}

func (self *Mem) Get() error {
	var statex C.MEMORYSTATUSEX
	statex.dwLength = C.DWORD(unsafe.Sizeof(statex))

	succeeded := C.GlobalMemoryStatusEx(&statex)
	if succeeded == C.FALSE {
		return syscall.GetLastError()
	}

	self.Total = uint64(statex.ullTotalPhys)
	self.Free = uint64(statex.ullAvailPhys)
	self.Used = self.Total - self.Free
	vtotal := uint64(statex.ullTotalVirtual)
	self.ActualFree = uint64(statex.ullAvailVirtual)
	self.ActualUsed = vtotal - self.ActualFree

	return nil
}

func (self *Swap) Get() error {
	//return notImplemented()
	return nil
}

func (self *Cpu) Get() error {

	var lpIdleTime, lpKernelTime, lpUserTime C.FILETIME

	succeeded := C.GetSystemTimes(&lpIdleTime, &lpKernelTime, &lpUserTime)
	if succeeded == C.FALSE {
		return syscall.GetLastError()
	}

	LOT := float64(0.0000001)
	HIT := (LOT * 4294967296.0)

	idle := ((HIT * float64(lpIdleTime.dwHighDateTime)) + (LOT * float64(lpIdleTime.dwLowDateTime)))
	user := ((HIT * float64(lpUserTime.dwHighDateTime)) + (LOT * float64(lpUserTime.dwLowDateTime)))
	kernel := ((HIT * float64(lpKernelTime.dwHighDateTime)) + (LOT * float64(lpKernelTime.dwLowDateTime)))
	system := (kernel - idle)

	self.Idle = uint64(idle)
	self.User = uint64(user)
	self.Sys = uint64(system)
	return nil
}

func (self *CpuList) Get() error {
	//return notImplemented()
	return nil
}

func (self *FileSystemList) Get() error {

	/*
		Get a list of the disks:
		fsutil fsinfo drives

		Get driver type:
		fsutil fsinfo drivetype C:

		Get volume info:
		fsutil fsinfo volumeinfo C:
	*/

	NullTermToStrings := func(b []byte) []string {
		list := []string{}
		for _, x := range bytes.SplitN(b, []byte{0, 0}, -1) {
			x = bytes.Replace(x, []byte{0}, []byte{}, -1)
			if len(x) == 0 {
				break
			}
			list = append(list, string(x))
		}
		return list
	}

	GetDriveTypeString := func(drivetype uintptr) string {
		switch drivetype {
		case 1:
			return "Invalid"
		case 2:
			return "Removable drive"
		case 3:
			return "Fixed drive"
		case 4:
			return "Remote drive"
		case 5:
			return "CDROM"
		case 6:
			return "RAM disk"
		default:
			return "Unknown"
		}
	}

	lpBuffer := make([]byte, 254)

	ret, _, _ := procGetLogicalDriveStringsW.Call(
		uintptr(len(lpBuffer)),
		uintptr(unsafe.Pointer(&lpBuffer[0])))
	if ret == 0 {
		return fmt.Errorf("GetLogicalDriveStringsW %v", syscall.GetLastError())
	}
	fss := NullTermToStrings(lpBuffer)

	for _, fs := range fss {
		typepath, _ := syscall.UTF16PtrFromString(fs)
		typeret, _, _ := procGetDriveType.Call(uintptr(unsafe.Pointer(typepath)))
		if typeret == 0 {
			return fmt.Errorf("GetDriveTypeW %v", syscall.GetLastError())
		}
		/* TODO volumeinfo by calling GetVolumeInformationW */

		d := FileSystem{
			DirName:  fs,
			DevName:  fs,
			TypeName: GetDriveTypeString(typeret),
		}
		self.List = append(self.List, d)
	}
	return nil
}

// Retrieves the process identifier for each process object in the system.

func (self *ProcList) Get() error {

	var enumSize int
	var pids [1024]C.DWORD

	// If the function succeeds, the return value is nonzero.
	ret, _, _ := procEnumProcesses.Call(
		uintptr(unsafe.Pointer(&pids[0])),
		uintptr(unsafe.Sizeof(pids)),
		uintptr(unsafe.Pointer(&enumSize)),
	)
	if ret == 0 {
		return syscall.GetLastError()
	}

	results := []int{}

	pids_size := enumSize / int(unsafe.Sizeof(pids[0]))

	for _, pid := range pids[:pids_size] {
		results = append(results, int(pid))
	}

	self.List = results

	return nil
}

func FiletimeToDuration(ft *syscall.Filetime) time.Duration {
	n := int64(ft.HighDateTime)<<32 + int64(ft.LowDateTime) // in 100-nanosecond intervals
	return time.Duration(n*100) * time.Nanosecond
}

func CarrayToString(c [MAX_PATH]byte) string {
	end := 0
	for {
		if c[end] == 0 {
			break
		}
		end++
	}
	return string(c[:end])
}

func (self *ProcState) Get(pid int) error {

	var err error

	self.Name, err = GetProcName(pid)
	if err != nil {
		return err
	}

	self.State, err = GetProcStatus(pid)
	if err != nil {
		return err
	}

	self.Ppid, err = GetParentPid(pid)
	if err != nil {
		return err
	}
	return nil
}

func GetProcName(pid int) (string, error) {

	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))

	defer syscall.CloseHandle(handle)

	if err != nil {
		return "", fmt.Errorf("OpenProcess fails with %v", err)
	}

	var nameProc [MAX_PATH]byte

	ret, _, _ := procGetProcessImageFileName.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&nameProc)),
		uintptr(MAX_PATH),
	)
	if ret == 0 {
		return "", syscall.GetLastError()
	}

	return filepath.Base(CarrayToString(nameProc)), nil

}

func GetProcStatus(pid int) (RunState, error) {

	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))

	defer syscall.CloseHandle(handle)

	if err != nil {
		return RunStateUnknown, fmt.Errorf("OpenProcess fails with %v", err)
	}

	var ec uint32
	e := syscall.GetExitCodeProcess(syscall.Handle(handle), &ec)
	if e != nil {
		return RunStateUnknown, os.NewSyscallError("GetExitCodeProcess", e)
	}
	if ec == 259 { //still active
		return RunStateRun, nil
	}
	return RunStateSleep, nil
}

func GetParentPid(pid int) (int, error) {

	handle, _, _ := procCreateToolhelp32Snapshot.Call(
		uintptr(TH32CS_SNAPPROCESS),
		uintptr(uint32(pid)),
	)
	if handle < 0 {
		return 0, syscall.GetLastError()
	}
	defer syscall.CloseHandle(syscall.Handle(handle))

	var entry PROCESSENTRY32
	entry.Size = uint32(unsafe.Sizeof(entry))

	ret, _, _ := procProcess32First.Call(handle, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		return 0, fmt.Errorf("Error retrieving process info.")
	}
	return int(entry.ParentProcessID), nil

}

func (self *ProcMem) Get(pid int) error {
	handle, err := syscall.OpenProcess(PROCESS_ALL_ACCESS, false, uint32(pid))

	defer syscall.CloseHandle(handle)

	if err != nil {
		return fmt.Errorf("OpenProcess fails with %v", err)
	}

	var mem PROCESS_MEMORY_COUNTERS_EX
	mem.CB = uint32(unsafe.Sizeof(mem))

	r1, _, e1 := procGetProcessMemoryInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&mem)),
		uintptr(mem.CB),
	)
	if r1 == 0 {
		if e1 != nil {
			return error(e1)
		} else {
			return syscall.EINVAL
		}
	}

	self.Resident = uint64(mem.WorkingSetSize)
	self.Size = uint64(mem.PrivateUsage)
	// Size contains only to the Private Bytes
	// Virtual Bytes are the Working Set plus paged Private Bytes and standby list.
	return nil
}

func (self *ProcTime) Get(pid int) error {
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))

	defer syscall.CloseHandle(handle)

	if err != nil {
		return fmt.Errorf("OpenProcess fails with %v", err)

	}
	var CPU syscall.Rusage
	if err := syscall.GetProcessTimes(handle, &CPU.CreationTime, &CPU.ExitTime, &CPU.KernelTime, &CPU.UserTime); err != nil {
		return fmt.Errorf("GetProcessTimes fails with %v", err)
	}

	// convert to millis
	self.StartTime = uint64(FiletimeToDuration(&CPU.CreationTime).Nanoseconds() / 1e6)

	self.User = uint64(FiletimeToDuration(&CPU.UserTime).Nanoseconds() / 1e6)

	self.Sys = uint64(FiletimeToDuration(&CPU.KernelTime).Nanoseconds() / 1e6)

	self.Total = self.User + self.Sys

	return nil
}

func (self *ProcArgs) Get(pid int) error {
	process, err := getWin32Process(int32(pid))
	if err != nil {
		return fmt.Errorf("could not get CommandLine: %v", err)
	}

	var args []string
	args = append(args, process.CommandLine)
	self.List = args

	return nil
}

func (self *ProcExe) Get(pid int) error {
	return notImplemented()
}

func (self *FileSystemUsage) Get(path string) error {

	/*
		Get free, available, total free bytes:
		fsutil volume diskfree C:
	*/
	var availableBytes C.ULARGE_INTEGER
	var totalBytes C.ULARGE_INTEGER
	var totalFreeBytes C.ULARGE_INTEGER

	pathChars := C.CString(path)
	defer C.free(unsafe.Pointer(pathChars))

	succeeded := C.GetDiskFreeSpaceEx((*C.CHAR)(pathChars), &availableBytes, &totalBytes, &totalFreeBytes)
	if succeeded == C.FALSE {
		return syscall.GetLastError()
	}

	self.Total = *(*uint64)(unsafe.Pointer(&totalBytes))
	self.Free = *(*uint64)(unsafe.Pointer(&totalFreeBytes))
	self.Used = self.Total - self.Free
	self.Avail = *(*uint64)(unsafe.Pointer(&availableBytes))

	return nil
}

func notImplemented() error {
	panic("Not Implemented")
	return nil
}

// getWin32Process gets information about the process with the given process ID.
// It uses a WMI query to get the information from the local system.
func getWin32Process(pid int32) (Win32_Process, error) {
	var dst []Win32_Process
	query := fmt.Sprintf("WHERE ProcessId = %d", pid)
	q := wmi.CreateQuery(&dst, query)
	err := wmi.Query(q, &dst)
	if err != nil {
		return Win32_Process{}, fmt.Errorf("could not get Win32_Process %s: %v", query, err)
	}
	if len(dst) < 1 {
		return Win32_Process{}, fmt.Errorf("could not get Win32_Process %s: Process not found", query)
	}
	return dst[0], nil
}
