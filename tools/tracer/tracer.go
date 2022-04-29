package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	go_hotfix "github.com/lsg2020/go-hotfix"
	sys "golang.org/x/sys/unix"
)

const (
	ExitCodeParam  = -1
	ExitCodeAttach = -2
	ExitCodePatch  = -3
)

func logPrint(param *go_hotfix.TracerParam, template string, args ...interface{}) {
	msg := time.Now().Format("2006-01-02 15:04:05") + " " + fmt.Sprintf(template, args...)
	fmt.Println(msg)

	filename := fmt.Sprintf("patch_%s_%d_%d.log", filepath.Base(param.Path), param.Pid, os.Getpid())
	fl, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_RDWR, os.ModeAppend)
	if err != nil {
		return
	}
	defer fl.Close()
	_, err = fl.WriteString(msg + "\n")
	if err != nil {
		fmt.Println("log error", err)
	}
}

func getTids(pid int) ([]int, error) {
	threadIds := []int{}
	tids, _ := filepath.Glob(fmt.Sprintf("/proc/%d/task/*", pid))
	for _, tidpath := range tids {
		tidstr := filepath.Base(tidpath)
		tid, err := strconv.Atoi(tidstr)
		if err != nil {
			return nil, err
		}

		threadIds = append(threadIds, tid)
	}
	return threadIds, nil
}

func waitStopSignal(param *go_hotfix.TracerParam, tid int, signal syscall.Signal) error {
	for {
		var s sys.WaitStatus
		wpid, err := sys.Wait4(tid, &s, sys.WALL, nil)
		if err != nil {
			return err
		}
		logPrint(param, "waitStopSignal %v %v %v %v %v %v", tid, signal, s.Stopped(), s.StopSignal(), s.StopSignal() != signal, s)
		if !s.Stopped() || s.StopSignal() != signal {
			sys.Kill(wpid, signal)
			continue
		}
		break
	}
	return nil
}

func searchSliceInt(list []int, val int) int {
	for i, v := range list {
		if v == val {
			return i
		}
	}
	return -1
}

var ptraceChan = make(chan func())
var ptraceDoneChan = make(chan interface{})

func execPtraceFunc(fn func()) {
	ptraceChan <- fn
	<-ptraceDoneChan
}

var attachThreadList []int

func checkThreadPC(param *go_hotfix.TracerParam, tid int) error {
	var err error
	var regs syscall.PtraceRegs
	execPtraceFunc(func() { err = syscall.PtraceGetRegs(tid, &regs) })
	if err != nil {
		return err
	}

	for step := 0; step < 200; step++ {
		success := true

		for i, entry := range param.FunctionEntry {
			codeLen := uint64(len(param.JumpCode[i]))
			if regs.PC() >= entry+1 && regs.PC() <= entry+codeLen {
				logPrint(param, "step thread %v %v %v %v", tid, regs.PC(), entry, codeLen)
				success = false

				execPtraceFunc(func() { err = syscall.PtraceSingleStep(tid) })
				if err != nil {
					return err
				}
				err = waitStopSignal(param, tid, sys.SIGTRAP)
				if err != nil {
					return err
				}
				logPrint(param, "step thread finish %v %v %v %v", tid, regs.PC(), entry, codeLen)
			}
		}

		if success {
			return nil
		}
	}

	return fmt.Errorf("check thread pc error %d", tid)
}

func attachThread(param *go_hotfix.TracerParam, tid int) error {
	logPrint(param, "attach thread %v", tid)
	var err error
	execPtraceFunc(func() { err = syscall.PtraceAttach(tid) })
	if err != nil {
		return err
	}
	attachThreadList = append(attachThreadList, tid)
	err = waitStopSignal(param, tid, sys.SIGSTOP)
	if err != nil {
		return err
	}

	logPrint(param, "check thread pc %v", tid)
	err = checkThreadPC(param, tid)
	if err != nil {
		return err
	}
	return nil
}

func refreshThreadList(param *go_hotfix.TracerParam) (int, error) {
	tids, err := getTids(param.Pid)
	if err != nil {
		return 0, err
	}

	amount := 0
	for _, tid := range tids {
		if searchSliceInt(attachThreadList, tid) >= 0 {
			continue
		}

		err = attachThread(param, tid)
		if err != nil {
			return 0, err
		}
		amount++
	}
	return amount, nil
}

func main() {
	param := go_hotfix.TracerParam{}

	// exit error code
	var exitCode int
	errorMsg := func(code int, template string, msgs ...interface{}) {
		logPrint(&param, template, msgs...)
		exitCode = code
	}
	defer func() {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	}()

	// parse param
	if len(os.Args) < 2 {
		errorMsg(ExitCodeParam, "args amount error")
		return
	}
	paramBuf, err := base64.StdEncoding.DecodeString(os.Args[1])
	if err != nil {
		errorMsg(ExitCodeParam, "param error %v", err)
		return
	}
	err = json.Unmarshal(paramBuf, &param)
	if err != nil {
		errorMsg(ExitCodeParam, "param error %v", err)
		return
	}
	logPrint(&param, "start patch %d \n%v", param.Pid, string(paramBuf))

	go func() {
		// We must ensure here that we are running on the same thread during
		// while invoking the ptrace(2) syscall. This is due to the fact that ptrace(2) expects
		// all commands after PTRACE_ATTACH to come from the same thread.
		runtime.LockOSThread()

		for fn := range ptraceChan {
			fn()
			ptraceDoneChan <- nil
		}
	}()

	defer func() {
		for _, tid := range attachThreadList {
			logPrint(&param, "--- detach %v", tid)
			execPtraceFunc(func() { syscall.PtraceDetach(tid) })
		}
	}()

	logPrint(&param, "refresh thread %v", param.Pid)
	i := 0
	for i < 10 {
		amount, err := refreshThreadList(&param)
		if err != nil {
			errorMsg(ExitCodeAttach, "refresh thread error %v", err)
			return
		}
		if amount == 0 {
			i++
		}
		time.Sleep(time.Millisecond * 100)
	}

	// patch code
	patchFlag := false
	logPrint(&param, "set jump code")
	patchCodes := make([][]byte, len(param.FunctionEntry))
	for i, entry := range param.FunctionEntry {
		logPrint(&param, "save patch code %v", entry)
		original := make([]byte, len(param.JumpCode[i]))
		execPtraceFunc(func() { _, err = syscall.PtracePeekData(param.Pid, uintptr(entry), original) })
		if err != nil {
			errorMsg(ExitCodePatch, "save patch code %v", err)
			return
		}
		patchCodes[i] = original
		logPrint(&param, "save patch code finish %v %v", entry, original)
	}
	defer func() {
		if !patchFlag {
			for i, entry := range param.FunctionEntry {
				logPrint(&param, "--- recovery patch code %v %v", entry, patchCodes[i])
				execPtraceFunc(func() { syscall.PtracePokeData(param.Pid, uintptr(entry), patchCodes[i]) })
			}
		}
	}()
	for i, entry := range param.FunctionEntry {
		logPrint(&param, "patch code %v", entry)
		execPtraceFunc(func() { _, err = syscall.PtracePokeData(param.Pid, uintptr(entry), param.JumpCode[i]) })
		if err != nil {
			errorMsg(ExitCodePatch, "patch error %v", err)
			return
		}
		logPrint(&param, "patch code finish %v %v", entry, param.JumpCode[i])
	}
	patchFlag = true
}
