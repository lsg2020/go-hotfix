# go-hotfix
* go-hotfix is a use plugin and debug symbols hotfix function

### 仅支持linux平台(windows不支持插件,macos delve不支持[插件调试](https://github.com/go-delve/delve/issues/1628))

# 特性
* 使用delve加载执行文件和so的调试符号,来找到函数路径名对应的代码地址
* 补丁包使用go plugin编译方便
* 线程安全 使用[tracer](http://github.com/lsg2020/go-hotfix/tree/master/tools/tracer/tracer.go) 阻塞住进程后再打补丁
* 支持运行时修复导出函数/非导出函数/结构体方法,比如:
  * `github.com/lsg2020/go-hotfix/examples/data.TestAdd`
  * `github.com/lsg2020/go-hotfix/examples/data.(*DataType).TestHotfix`
  * `github.com/lsg2020/go-hotfix/examples/data.testPrivateFunc`
  * `github.com/lsg2020/go-hotfix/examples/data.(*DataType).test`

# 原理
* 一切的基础
    * [`monkey`](https://github.com/bouk/monkey), [monkey 原理](https://bou.ke/blog/monkey-patching-in-go/)
    * [`go plugin`](https://pkg.go.dev/plugin) 很好的给我们提供了补丁包的编译,plugin中加载的类型和全局变量就是主程序中的类型和全局变量，那么我们直接`-buildmode=plugin`编译main包就可以编译出补丁包了
* `monkey`的补丁函数[func Patch(target, replacement interface{})](https://github.com/bouk/monkey/blob/b118a17387657e860bb85a0f3dafd15cad03c266/examples/bleep.go#L12-L18) ,需要把目标函数和替换函数当参数传进去，另外有三个问题
  * [jump code](https://github.com/bouk/monkey/blob/b118a17387657e860bb85a0f3dafd15cad03c266/monkey_amd64.go#L4-L17) 如果比原有的代码长，可能导致写越界了
  * 不是线程安全的,如果正在替换的时候有其他线程刚好执行到这段代码就有问题了
  * 新旧版本的函数怎么区分, 如果直接在plugin代码中调用`monkey.Patch`,目标函数和替换函数都是补丁包中的实现，根本影响不到主程序里的函数
* 问题一:
    * 我们发现go程序内嵌了调试符号[dwarf](http://dwarfstd.org/doc/dwarf-2.0.0.pdf) ,调试符号里就有函数名及函数代码的跳转代码块信息，
    * 更方便的是调试器[delve](https://github.com/go-delve/delve) ,直接提供了调试符号的[解析]((https://github.com/go-delve/delve/blob/9d269791d5b9a821eb5cc5d868029bff6e59d231/pkg/proc/bininfo.go#L654)) ,那就很方便了[直接加载](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L30-L38) [检查](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L91-L96)
* 问题二:
    * 我们可以参考gdb使用[ptrace](https://man7.org/linux/man-pages/man2/ptrace.2.html) ptrace可以阻塞住线程，并取出线程下一条执行指令的地址是多少
    * 如果我们实现一个[工具](https://github.com/lsg2020/go-hotfix/blob/master/tools/tracer/tracer.go) ，阻塞住当前进程的所有线程，并检查线程执行地址是否在要替换的代码内,如果在范围内则单步执行出去,则相对安全一些
    * 参考
        * [1](https://stackoverflow.com/questions/18577956/how-to-use-ptrace-to-get-a-consistent-view-of-multiple-threads)
        * [2](https://xz.aliyun.com/t/9815)
* 问题三:
    * 同样使用[delve](https://github.com/go-delve/delve) 加载补丁包,可以发现在主程序和补丁包的调试符号中对应的函数是同名的，那就可以得到新旧版本的函数的不同入口地址
* 新的问题: dwarf中找到的是函数的入口地址,`monkey.Patch`需要的参数是运行时的函数对象,怎么根据入口地址创建出对应的函数对象呢
    * 发现如果知道函数类型`reflect.Type`,就可以根据 `reflect.MakeFunc` 创建出函数对象来,[即可设置函数对象的代码入口地址](https://github.com/AlaxLee/go-forceexport/blob/e177a7245604bc2cffffc17df1df86544042a510/go116/forceexport.go#L33-L53)
    * 没找到安全的方法来自动根据函数名在运行时搜索出函数类型，所以需要[补丁包暴露](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/examples/hello/hello.go#L17-L30)
* 理清楚问题最后实现就比较简单 `Hotfix(path string, names []string, threadSafe bool) (string, error)`
    * 例: `Hotfix("hello_v1.so", []string{ "github.com/lsg2020/go-hotfix/examples/data.(*DataType).test"}, false)`
        * `path`: 插件包的加载路径
        * `names`: 需要更新的函数的路径名列表
        * `threadSafe`：是否线程安全,线程安全则直接用`monkey.Patch`替换即可，不安全时使用[tracer](https://github.com/lsg2020/go-hotfix/blob/master/tools/tracer/tracer.go)
    * [基本流程](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L23-L103)
        * [加载主程序调试符号](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L30-L38)
        * [加载热更的函数名列表的类型](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L41-L57)
        * 查找[旧版本函数](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L60-L67) ,[新版本函数](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L70-L89)
        * [做函数跳转地址替换](https://github.com/lsg2020/go-hotfix/blob/33e1482416241c52f2e78f6cb1afdb1484a83260/hotfix_linux.go#L98-L102)


# 注意
* 不要使用编译参数`-ldflags="-s -w"`会加载不到调试信息
* 找不到函数时可以考虑是不是被内联优化了,可以使用编译参数`-gcflags=all=-l`关闭内联优化
* main包内的类型在主程序和补丁包中是两个不同的类型,不要热更使用他作为参数/返回值的函数
* 补丁包中引用的类型和全局变量会是第一次被加载的对象
* 不要修改函数的参数及返回值类型
* 可以新增类型,但不要修改已有的类型定义
* 编译插件包时需要注意
  * 防止插件因版本不一至加载失败[注释代码](https://github.com/golang/go/blob/fd6c556dc82253722a7f7b9f554a1892b0ede36e/src/runtime/plugin.go#L51-L56) ,需要自己去保证加载的补丁包版本和主程序一至
  * 未被引用的函数是不会被编译的，可以考虑增加一个导出函数 `func Hotfix() { main() }` 来编译
  * 补丁包不同的版本main包最好有版本变化，防止出现 `plugin already loaded`
  * 在导出函数`HotfixFunctionType`里返回热更函数对应的类型

# 示例测试
* [注释代码](https://github.com/golang/go/blob/fd6c556dc82253722a7f7b9f554a1892b0ede36e/src/runtime/plugin.go#L51-L56)
* 编译tracer `go build tools/tracer/tracer.go`
* 编译示例 `go build -gcflags=all=-l examples/hello/hello.go`
* 编译补丁包
  * 修改代码`github.com/lsg2020/go-hotfix/examples/data`
  * `go build -gcflags=all=-l -buildmode=plugin -ldflags="-X main.HotfixVersion=1" -o hello_v1.so examples/hello/hello.go`
  * 修改代码`github.com/lsg2020/go-hotfix/examples/data`
  * `go build -gcflags=all=-l -buildmode=plugin -ldflags="-X main.HotfixVersion=2" -o hello_v2.so examples/hello/hello.go`
* 执行测试 `./hello`
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
