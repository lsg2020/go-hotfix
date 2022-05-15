package go_hotfix

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"plugin"
	"reflect"
	"sync"
	"unsafe"

	"github.com/agiledragon/gomonkey"
	"github.com/go-delve/delve/pkg/proc"
	"github.com/lsg2020/gort"
)

func hotfix(path string, names []string, variadic []bool, threadSafe bool) (string, error) {
	dwarf, err := gort.NewDwarfRT("")
	if err != nil {
		return "", err
	}

	isVariadic := func(i int) bool {
		if i >= len(variadic) {
			return false
		}
		return variadic[i]
	}

	// search old function
	oldFunctionEntrys := make([]*proc.Function, 0, len(names))
	for _, name := range names {
		entry, err := dwarf.FindFuncEntry(name)
		if err != nil {
			return "", fmt.Errorf("not found function: %s in main package err:%s", name, err)
		}
		oldFunctionEntrys = append(oldFunctionEntrys, entry)
	}

	// load patch package
	p, err := plugin.Open(path)
	if err != nil {
		return "", err
	}

	// search dynamic library
	lib, addr, err := dwarf.SearchPluginByName(path)
	if err != nil {
		return "", err
	}
	if lib == "" {
		return "", ErrSearchPluginFailed
	}
	err = dwarf.AddImage(lib, addr)
	if err != nil {
		return "", err
	}

	hotfixFunctionType, hotfixFunctionTypeErr := p.Lookup("HotfixFunctionType")
	getHotfixFunction := func(name string) (reflect.Value, bool) {
		if hotfixFunctionTypeErr != nil {
			return reflect.Value{}, false
		}
		hotfixTypeFn, ok := hotfixFunctionType.(func(string) reflect.Type)
		if !ok {
			return reflect.Value{}, false
		}
		typ := hotfixTypeFn(name)
		if typ == nil {
			return reflect.Value{}, false
		}
		pc, err := dwarf.FindFuncPc(name)
		if err != nil || pc == 0 {
			return reflect.Value{}, false
		}

		f := reflect.MakeFunc(typ, nil)
		funcPtrVal := reflect.ValueOf(f).FieldByName("ptr").Pointer()
		funcPtr := (*Func)(unsafe.Pointer(funcPtrVal))
		funcPtr.codePtr = uintptr(pc)
		return f, true
	}

	newFunctions := make([]reflect.Value, 0, len(names))
	oldFunctions := make([]reflect.Value, 0, len(names))
	for i, name := range names {
		f, ok := getHotfixFunction(name)
		if !ok {
			f, err = dwarf.FindFunc(name, isVariadic(i))
			if err != nil {
				return "", fmt.Errorf("not found function %s in plugin err:%#v", name, err)
			}
		}
		if uint64(f.Pointer()) == oldFunctionEntrys[i].Entry {
			return "", fmt.Errorf("not found function %s in plugin err:%#v", name, err)
		}
		newFunctions = append(newFunctions, f)

		oldFunc := reflect.MakeFunc(f.Type(), nil)
		funcPtrVal := reflect.ValueOf(oldFunc).FieldByName("ptr").Pointer()
		funcPtr := (*Func)(unsafe.Pointer(funcPtrVal))
		funcPtr.codePtr = uintptr(oldFunctionEntrys[i].Entry)
		oldFunctions = append(oldFunctions, oldFunc)
	}

	for i := 0; i < len(oldFunctionEntrys); i++ {
		jumpCode := buildJmpDirective(0)
		if (oldFunctionEntrys[i].End - oldFunctionEntrys[i].Entry) < uint64(len(jumpCode)) {
			return "", fmt.Errorf("jump code error %s", names[i])
		}
	}

	if threadSafe {
		monkeyPatch(oldFunctions, newFunctions)
		return "", nil
	}
	return patch(path, names, dwarf.BI(), oldFunctionEntrys, newFunctions)
}

type Func struct {
	codePtr uintptr
}

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

func patch(path string, names []string, bi *proc.BinaryInfo, oldFunctions []*proc.Function, newFunctions []reflect.Value) (string, error) {
	param := TracerParam{
		Pid:                   os.Getpid(),
		Path:                  path,
		Names:                 names,
		BreakpointInstruction: bi.Arch.BreakpointInstruction(),
	}

	for i := 0; i < len(oldFunctions); i++ {
		newFunc := newFunctions[i]
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

func monkeyPatch(oldFunctions []reflect.Value, newFunctions []reflect.Value) {
	for i := 0; i < len(oldFunctions); i++ {
		gomonkey.ApplyFunc(oldFunctions[i].Interface(), newFunctions[i].Interface())
	}
}

//go:linkname buildJmpDirective github.com/agiledragon/gomonkey.buildJmpDirective
func buildJmpDirective(double uintptr) []byte

//go:linkname getPointer github.com/agiledragon/gomonkey.getPointer
func getPointer(v reflect.Value) unsafe.Pointer
