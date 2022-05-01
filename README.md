# go-hotfix
* go-hotfix is a use plugin and debug symbol hotfix function

### 仅支持linux平台(windows不支持插件,macos delve不支持[插件调试](https://github.com/go-delve/delve/issues/1628))

# 特性
* 使用delve加载执行文件和so的调试符号,来找到函数路径名对应的代码地址
* 补丁包使用go plugin方便编译
* 线程安全 使用[tracer](http://github.com/lsg2020/go-hotfix/tree/master/tools/tracer/tracer.go) 阻塞住进程后再打补丁
* 运行时修复导出函数/非导出函数/结构体方法,比如:
  * `github.com/lsg2020/go-hotfix/examples/data.TestAdd`
  * `github.com/lsg2020/go-hotfix/examples/data.(*DataType).TestHotfix`
  * `github.com/lsg2020/go-hotfix/examples/data.testPrivateFunc`
  * `github.com/lsg2020/go-hotfix/examples/data.(*DataType).test`
  
# 注意
* 不要使用编译参数`-ldflags="-s -w"`会加载不到调试信息
* 找不到函数时可以考虑是不是被内联优化了,可以使用编译参数`-gcflags=all=-l`关闭内联优化
* 不要热更使用插件main包定义的类型作为参数或返回值的函数,main包内的类型在执行程序和补丁包中是两个不同的类型
* 补丁包中引用的类型和全局变量会加载第一次被实例化的对象
* 不要修改函数的参数及返回值类型
* 可以新增类型,但不要修改已有的类型定义
* 编译插件包时需要注意
  * 防止插件因版本不一至加载失败[注释代码](https://github.com/golang/go/blob/fd6c556dc82253722a7f7b9f554a1892b0ede36e/src/runtime/plugin.go#L51-L56) ,需要自己去保证加载的补丁包版本和主程序一至
  * 未被引用的函数是不会被编译的，可以考虑增加一个导出函数 `func Hotfix() { main() }` 来编译
  * 补丁包不同的版本main包最好有版本变化，防止出现 `plugin already loaded`

# 示例
* 防止插件版本不一至导致插件加载失败[注释代码](https://github.com/golang/go/blob/fd6c556dc82253722a7f7b9f554a1892b0ede36e/src/runtime/plugin.go#L51-L56)
* 编译tracer `go build tools/tracer/tracer.go`到当前目录,或者修改路径`go_hotfix.TracerPath = "./tracer"`
* 编译示例 `go build -gcflags=all=-l examples/hello/hello.go`
* 编译补丁包
  * 修改代码`github.com/lsg2020/go-hotfix/examples/data.TestAdd`
  * `go build -gcflags=all=-l -buildmode=plugin -ldflags="-X main.HotfixVersion=1" -o hello_v1.so examples/hello/hello.go`
  * 修改代码`github.com/lsg2020/go-hotfix/examples/data.TestAdd`
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
