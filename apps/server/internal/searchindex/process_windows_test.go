package searchindex

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() {
	measureProcess = func() (processUsage, error) {
		memory := struct {
			Size, PageFaults                                                                      uint32
			PeakRSS, WorkingSet, PeakPaged, Paged, PeakNonPaged, NonPaged, Pagefile, PeakPagefile uintptr
		}{}
		memory.Size = uint32(unsafe.Sizeof(memory))
		readMemory := windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
		result, _, err := readMemory.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&memory)), uintptr(memory.Size))
		if result == 0 {
			return processUsage{}, fmt.Errorf("process memory measurement: %w", err)
		}
		io := struct{ ReadOperations, WriteOperations, OtherOperations, ReadBytes, WriteBytes, OtherBytes uint64 }{}
		readIO := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessIoCounters")
		result, _, err = readIO.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&io)))
		if result == 0 {
			return processUsage{}, fmt.Errorf("process I/O measurement: %w", err)
		}
		return processUsage{PeakRSS: uint64(memory.PeakRSS), ReadBytes: io.ReadBytes, WriteBytes: io.WriteBytes}, nil
	}
}
