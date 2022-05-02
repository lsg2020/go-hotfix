# go-hotfix
* go-hotfix is a use plugin and debug symbol hotfix function

### Only support linux platform(windows does not support plugin,macos delve does not support [plugin debugging](https://github.com/go-delve/delve/issues/1628))
### [中文](http://github.com/lsg2020/go-hotfix/tree/master/README_CN.md)

# Features
* Debug symbols for loading `executables` and `so` using `delve`, to find the function entry address of function pathname
* Patch package uses `go plugin` for easy compilation
* Thread safety, using [tracer](http://github.com/lsg2020/go-hotfix/tree/master/tools/tracer/tracer.go) block the process and then apply the patch
* Runtime repair support: Export Functions/Private Functions/Member Methods
  * `github.com/lsg2020/go-hotfix/examples/data.TestAdd`
  * `github.com/lsg2020/go-hotfix/examples/data.(*DataType).TestHotfix`
  * `github.com/lsg2020/go-hotfix/examples/data.testPrivateFunc`
  * `github.com/lsg2020/go-hotfix/examples/data.(*DataType).test`

# Principle Analysis
* The foundation of everything
    * [`monkey`](https://bou.ke/blog/monkey-patching-in-go/)
    * [`go plugin`](https://pkg.go.dev/plugin) It's good to give us the compilation of the patch package, The types and global variables loaded in plugin are the same as in the main program，Then we compile the main package directly with `-buildmode=plugin` to compile the patch package
* `monkey` Patch function [func Patch(target, replacement interface{})](https://github.com/bouk/monkey/blob/b118a17387657e860bb85a0f3dafd15cad03c266/examples/bleep.go#L12-L18) ,You need to pass in the target function and the replacement function as parameters，There are three additional questions
  * [jump code](https://github.com/bouk/monkey/blob/b118a17387657e860bb85a0f3dafd15cad03c266/monkey_amd64.go#L4-L17) If it is longer than the original code, it may result in writing out of bounds
  * Not thread-safe, if there are other threads that happen to execute this code while it is being replaced, there will be a problem
  * How to distinguish between old and new versions of functions, If you call `monkey.Patch` directly in the plugin code, the target function and the replacement function are both implemented in the patch package and do not affect the functions in the main program at all
* Question 1:
    * We found that the go program has embedded debugging symbols[dwarf](http://dwarfstd.org/doc/dwarf-2.0.0.pdf) ,The debug symbol contains information about the function name and the jump code block of the function code
    * More convenient is the debugger[delve](https://github.com/go-delve/delve) ,Directly provides [parsing](https://github.com/go-delve/delve/blob/9d269791d5b9a821eb5cc5d868029bff6e59d231/pkg/proc/bininfo.go#L654) of debug symbols ,That would be very convenient [direct load](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L30-L38) [check](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L91-L96)
* Question 2:
    * We can learn gdb to use [ptrace](https://man7.org/linux/man-pages/man2/ptrace.2.html)
    * [Implement a tool](https://github.com/lsg2020/go-hotfix/blob/master/tools/tracer/tracer.go) ，Block all threads of the current process and check if the thread execution address is within the code to be replaced, if it is within the range, then execute it out in a single step
    * Reference
        * [1](https://stackoverflow.com/questions/18577956/how-to-use-ptrace-to-get-a-consistent-view-of-multiple-threads)
        * [2](https://xz.aliyun.com/t/9815)
* Question 3:
    * Using [delve](https://github.com/go-delve/delve) load the patch package, you can find that the corresponding functions in the debug symbols of the main program and the patch package are of the same name, so you can get the different entry addresses of the old and new versions of the functions
* New Question: `monkey.Patch` requires a runtime function object, how to create the corresponding function object based on the entry address?
    * If you know the function type `reflect.Type`, you can create a function object based on `reflect.MakeFunc`, [set the code entry address of the function object](https://github.com/AlaxLee/go-forceexport/blob/e177a7245604bc2cffffc17df1df86544042a510/go116/forceexport.go#L33-L53)
    * Didn't find a safe way to automatically search for function types at runtime based on function names, so [patch package exposure](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/examples/hello/hello.go#L17-L30) is needed
* The final implementation is relatively simple `Hotfix(path string, names []string, threadSafe bool) (string, error)`
    * Example: `Hotfix("hello_v1.so", []string{ "github.com/lsg2020/go-hotfix/examples/data.(*DataType).test"}, false)`
        * `path`: Patch package load path
        * `names`: List of pathnames of functions that need to be updated
        * `threadSafe`: Patch can be replaced by `monkey.Patch` if it is thread-safe, or [tracer](https://github.com/lsg2020/go-hotfix/blob/master/tools/tracer/tracer.go) if it is not
    * [Basic Process](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L23-L103)
        * [Load main program debug symbols](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L30-L38)
        * [Load types of function name](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L41-L57)
        * [Find functions entry address](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L60-L67) ,[Find functions entry address](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L70-L89)
        * [Patch](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L98-L102)


# Warning
* Don't use the compile parameter `-ldflags="-s -w"` it will not load debug information
* If you can't find the function, you can consider if it is optimized inline, you can use the compile parameter `-gcflags=all=-l` to turn off inline optimization
* Do not hotfix functions that use the types defined in the plugin's `main` package as parameters or return values, the types in the `main` package are two different types in the main program and the patch package
* Types and global variables referenced in patch packages are loaded the first time the object is loaded
* Do not modify the parameters and return value type of the function
* You can add new types, but do not modify existing type definitions
* Compiling plugin packages
  * Prevent plug-ins from failing to load due to version differences[Comment code](https://github.com/golang/go/blob/fd6c556dc82253722a7f7b9f554a1892b0ede36e/src/runtime/plugin.go#L51-L56) ,You need to make sure that the patch package version you load is the same as the main program.
  * Unreferenced functions will not compile, consider adding an exported function `func Hotfix() { main() }` to compile
  * It is better to have version changes for different versions of the main package to prevent `plugin already loaded`
  * In the export function `HotfixFunctionType` returns the type corresponding to the hotfix function

# Example
* [Comment code](https://github.com/golang/go/blob/fd6c556dc82253722a7f7b9f554a1892b0ede36e/src/runtime/plugin.go#L51-L56)
* Build tracer `go build tools/tracer/tracer.go`
* Build example `go build -gcflags=all=-l examples/hello/hello.go`
* Build patch package
  * Modify `github.com/lsg2020/go-hotfix/examples/data`
  * `go build -gcflags=all=-l -buildmode=plugin -ldflags="-X main.HotfixVersion=1" -o hello_v1.so examples/hello/hello.go`
  * Modify `github.com/lsg2020/go-hotfix/examples/data`
  * `go build -gcflags=all=-l -buildmode=plugin -ldflags="-X main.HotfixVersion=2" -o hello_v2.so examples/hello/hello.go`
* `./hello`
```
in testPrivateFunc
in func (d *DataType) test()
--------------------------- hello_v1.so
in testPrivateFunc v1
in func (d *DataType) test() v1
--------------------------- hello_v2.so
in testPrivateFunc v2
in func (d *DataType) test() v2
```
