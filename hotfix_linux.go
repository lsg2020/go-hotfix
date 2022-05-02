package go_hotfix

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"plugin"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/agiledragon/gomonkey"
	"github.com/go-delve/delve/pkg/proc"
)

func hotfix(path string, names []string, threadSafe bool) (string, error) {
	p, err := plugin.Open(path)
	if err != nil {
		return "", err
	}

	// load main debug symbol
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	mainBI := proc.NewBinaryInfo(runtime.GOOS, runtime.GOARCH)
	err = mainBI.LoadBinaryInfo(exePath, 0, nil)
	if err != nil {
		return "", err
	}

	// load hotfix function type
	functionTypes := make([]reflect.Type, len(names))
	hotfixFunctionType, err := p.Lookup("HotfixFunctionType")
	for i, name := range names {
		functionTypes[i] = reflect.FuncOf(nil, nil, false)
		if err == nil {
			if f, ok := hotfixFunctionType.(func(string) reflect.Type); ok {
				fType := f(name)
				if fType != nil {
					functionTypes[i] = fType
				}
			}
		}

		if functionTypes[i] == nil {
			return "", fmt.Errorf("function not found:%s", name)
		}
	}

	// search old function
	oldFunctions := []*proc.Function{}
	for _, name := range names {
		f := searchFunction(mainBI.Functions, name)
		if f == nil || f.Entry == 0 {
			return "", ErrNotFoundFunctionInMainPackage
		}
		oldFunctions = append(oldFunctions, f)
	}

	// search dynamic library
	lib, addr, err := searchElfSharedObjects(mainBI, path)
	if err != nil {
		return "", err
	}
	if lib == "" {
		return "", ErrSearchPluginFailed
	}
	err = mainBI.AddImage(lib, addr)
	if err != nil {
		return "", err
	}

	newFunctions := []*proc.Function{}
	for i, name := range names {
		f := searchFunction(mainBI.Functions, name)
		if f == nil || f.Entry == 0 || f.Entry == oldFunctions[i].Entry {
			return "", ErrNotFoundFunctionInPlugin
		}
		newFunctions = append(newFunctions, f)
	}

	for i := 0; i < len(oldFunctions); i++ {
		jumpCode := buildJmpDirective(0)
		if (oldFunctions[i].End - oldFunctions[i].Entry) < uint64(len(jumpCode)) {
			return "", ErrJumpCodeError
		}
	}

	if threadSafe {
		monkeyPatch(oldFunctions, newFunctions, functionTypes)
		return "", nil
	}
	return patch(path, names, mainBI, oldFunctions, newFunctions, functionTypes)
}

type Func struct {
	codePtr uintptr
}

const (
	maxNumLibraries      = 1000000 // maximum number of loaded libraries, to avoid loading forever on corrupted memory
	maxLibraryPathLength = 1000000 // maximum length for the path of a library, to avoid loading forever on corrupted memory
)

const (
	_DT_NULL  = 0  // DT_NULL as defined by SysV ABI specification
	_DT_DEBUG = 21 // DT_DEBUG as defined by SysV ABI specification
)

type TracerParam struct {
	Pid                   int
	Path                  string
	Names                 []string
	FunctionEntry         []uint64
	JumpCode              [][]byte
	BreakpointInstruction []byte
}

// guard patch functions
var patchFuncMutex sync.Mutex
var patchFuncs []reflect.Value

func patch(path string, names []string, bi *proc.BinaryInfo, oldFunctions []*proc.Function, newFunctions []*proc.Function, functionTypes []reflect.Type) (string, error) {
	param := TracerParam{
		Pid:                   os.Getpid(),
		Path:                  path,
		Names:                 names,
		BreakpointInstruction: bi.Arch.BreakpointInstruction(),
	}

	for i := 0; i < len(oldFunctions); i++ {
		newFunc := reflect.MakeFunc(functionTypes[i], nil)
		{
			funcPtrVal := reflect.ValueOf(newFunc).FieldByName("ptr").Pointer()
			funcPtr := (*Func)(unsafe.Pointer(funcPtrVal))
			funcPtr.codePtr = uintptr(newFunctions[i].Entry)
		}

		patchFuncMutex.Lock()
		patchFuncs = append(patchFuncs, newFunc)
		patchFuncMutex.Unlock()

		param.FunctionEntry = append(param.FunctionEntry, oldFunctions[i].Entry)
		param.JumpCode = append(param.JumpCode, buildJmpDirective((uintptr)(getPointer(newFunc))))
	}

	paramBuf, err := json.Marshal(param)
	if err != nil {
		return "", err
	}

	paramStr := base64.StdEncoding.EncodeToString(paramBuf)

	cmd := exec.Command(TracerPath, paramStr)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%d %s", exitError.ExitCode(), output.String())
		}
		return "", fmt.Errorf("%v %s", err, output.String())
	}
	// fmt.Println(output.String())
	return output.String(), nil
}

func monkeyPatch(oldFunctions []*proc.Function, newFunctions []*proc.Function, functionTypes []reflect.Type) {
	for i := 0; i < len(oldFunctions); i++ {
		oldFunc := reflect.MakeFunc(functionTypes[i], nil)
		{
			funcPtrVal := reflect.ValueOf(oldFunc).FieldByName("ptr").Pointer()
			funcPtr := (*Func)(unsafe.Pointer(funcPtrVal))
			funcPtr.codePtr = uintptr(oldFunctions[i].Entry)
		}

		newFunc := reflect.MakeFunc(functionTypes[i], nil)
		{
			funcPtrVal := reflect.ValueOf(newFunc).FieldByName("ptr").Pointer()
			funcPtr := (*Func)(unsafe.Pointer(funcPtrVal))
			funcPtr.codePtr = uintptr(newFunctions[i].Entry)
		}

		gomonkey.ApplyFunc(oldFunc.Interface(), newFunc.Interface())
	}
}

func searchFunction(functions []proc.Function, name string) *proc.Function {
	for i := len(functions) - 1; i >= 0; i-- {
		if functions[i].Name == name {
			return &functions[i]
		}
	}
	return nil
}

func searchElfSharedObjects(bi *proc.BinaryInfo, name string) (string, uint64, error) {
	if bi.ElfDynamicSection.Addr == 0 {
		// no dynamic section, therefore nothing to do here
		return "", 0, nil
	}
	debugAddr, err := dynamicSearchDebug(bi)
	if err != nil {
		return "", 0, err
	}
	if debugAddr == 0 {
		// no DT_DEBUG entry
		return "", 0, nil
	}

	// Offsets of the fields of the r_debug and link_map structs,
	// see /usr/include/elf/link.h for a full description of those structs.
	debugMapOffset := uint64(bi.Arch.PtrSize())

	r_map, err := readPtr(bi, debugAddr+debugMapOffset)
	if err != nil {
		return "", 0, err
	}

	libs := []string{}

	for {
		if r_map == 0 {
			break
		}
		if len(libs) > maxNumLibraries {
			return "", 0, ErrTooManyLibraries
		}
		lm, err := readLinkMapNode(bi, r_map)
		if err != nil {
			return "", 0, err
		}
		if strings.LastIndex(lm.name, name) >= 0 {
			return lm.name, lm.addr, nil
		}

		libs = append(libs, lm.name)
		r_map = lm.next
	}

	return "", 0, nil
}

func readPtr(bi *proc.BinaryInfo, addr uint64) (uint64, error) {
	ptrbuf := entryAddress(uintptr(addr), bi.Arch.PtrSize())
	return readUintRaw(bytes.NewReader(ptrbuf), binary.LittleEndian, bi.Arch.PtrSize())
}

// readUintRaw reads an integer of ptrSize bytes, with the specified byte order, from reader.
func readUintRaw(reader io.Reader, order binary.ByteOrder, ptrSize int) (uint64, error) {
	switch ptrSize {
	case 4:
		var n uint32
		if err := binary.Read(reader, order, &n); err != nil {
			return 0, err
		}
		return uint64(n), nil
	case 8:
		var n uint64
		if err := binary.Read(reader, order, &n); err != nil {
			return 0, err
		}
		return n, nil
	}
	return 0, fmt.Errorf("not supprted ptr size %d", ptrSize)
}

// dynamicSearchDebug searches for the DT_DEBUG entry in the .dynamic section
func dynamicSearchDebug(bi *proc.BinaryInfo) (uint64, error) {
	dynbuf := entryAddress(uintptr(bi.ElfDynamicSection.Addr), int(bi.ElfDynamicSection.Size))
	rd := bytes.NewReader(dynbuf)

	for {
		var tag, val uint64
		var err error
		if tag, err = readUintRaw(rd, binary.LittleEndian, bi.Arch.PtrSize()); err != nil {
			return 0, err
		}
		if val, err = readUintRaw(rd, binary.LittleEndian, bi.Arch.PtrSize()); err != nil {
			return 0, err
		}
		switch tag {
		case _DT_NULL:
			return 0, nil
		case _DT_DEBUG:
			return val, nil
		}
	}
}

type linkMap struct {
	addr       uint64
	name       string
	ld         uint64
	next, prev uint64
}

func readLinkMapNode(bi *proc.BinaryInfo, r_map uint64) (*linkMap, error) {
	var lm linkMap
	var ptrs [5]uint64
	for i := range ptrs {
		var err error
		ptrs[i], err = readPtr(bi, r_map+uint64(bi.Arch.PtrSize()*i))
		if err != nil {
			return nil, err
		}
	}
	lm.addr = ptrs[0]
	var err error
	lm.name, err = readCString(ptrs[1])
	if err != nil {
		return nil, err
	}
	lm.ld = ptrs[2]
	lm.next = ptrs[3]
	lm.prev = ptrs[4]
	return &lm, nil
}

func readCString(addr uint64) (string, error) {
	if addr == 0 {
		return "", nil
	}
	r := []byte{}
	for {
		if len(r) > maxLibraryPathLength {
			return "", fmt.Errorf("error reading libraries: string too long (%d)", len(r))
		}
		buf := entryAddress(uintptr(addr), 1)
		if buf[0] == 0 {
			break
		}
		r = append(r, buf[0])
		addr++
	}
	return string(r), nil
}

//go:linkname buildJmpDirective github.com/agiledragon/gomonkey.buildJmpDirective
func buildJmpDirective(double uintptr) []byte

//go:linkname entryAddress github.com/agiledragon/gomonkey.entryAddress
func entryAddress(p uintptr, l int) []byte

//go:linkname getPointer github.com/agiledragon/gomonkey.getPointer
func getPointer(v reflect.Value) unsafe.Pointer
