# KellyFramework

## TL;DR

kellyframework是一个golang的HTTP JSON API框架. 简而言之, 它可以很容易地把一个golang的函数或者对象方法转换成HTTP JSON API, 使用者只需要
考虑该函数或方法应当对应哪个URL路径, 而不用关心json编解码/url query string解析/url pattern解析等各种琐事.

## Example

它用起来就像这样:
```go
package main

import "os"
import "net/http"
import "github.com/abadcafe/kellyframework"

type userInfo struct {
    Age int
    Address string
}

type userName struct {
    Name string
}

type user struct {
    userName
    userInfo
}

func addUser(ctx *kellyframework.ServiceMethodContext, user *user) error {
    // do add user or return error
    return nil
}

func getUser(ctx *kellyframework.ServiceMethodContext, name *userName) (*userInfo, error) {
    // return userInfo or return error
    return &userInfo{}, nil
}

func deleteUser(ctx *kellyframework.ServiceMethodContext, name *userName) error {
    // do delete user or return error
    return nil
}

func main() {
    accessLogFile, err := os.OpenFile("access.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        panic(err)
    }

    triples := []*kellyframework.MethodPathFunctionTriple{
        {"POST", "/user/:Name", addUser},
        {"POST", "/user/", addUser},
        {"GET", "/user/:Name", getUser},
        {"DELETE", "/user/:Name", deleteUser},
    }
    loggingServiceRouter, err := kellyframework.NewLoggingHTTPRouter(triples, accessLogFile)
    if err != nil {
        panic(err)
    }

    http.Handle("/", loggingServiceRouter)
    http.HandleFunc("/hc/status.html", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
    http.ListenAndServe(":8080", nil)
}
```
就这么几行, 就得到了一个带access log功能的HTTP JSON API服务器.

可以用curl与之交互:
```shell
# 添加用户, name这个字段放在url pattern里, 优先级最高
curl -H "Content-Type: application/json" -d '{"Age": 18, "Address": "beijing"}' http://127.0.0.1:8080/user/test
# 或者把name字段直接放在json body里, 优先级次高
curl -H "Content-Type: application/json" -d '{"Name": "test", "Age": 18, "Address": "beijing"}' http://127.0.0.1:8080/user/
# 或者把name字段放在query string里, 优先级最低
curl -H "Content-Type: application/json" -d '{"Age": 18, "Address": "beijing"}' http://127.0.0.1:8080/user/?name=test

# 获取用户信息
curl -G http://127.0.0.1:8080/user/test

# 删除用户
curl -X "DELETE" http://127.0.0.1:8080/user/test
```
**如果函数返回error或者panic了, 该框架会自动封装成HTTP的500错误.**

## FAQ

### kellyframework对函数原型是否有要求?

是的, 只支持两种函数原型:

`func(*ServiceMethodContext, *struct) (anything, error)`

这种情况下, 框架会把url pattern/json/query string解析成struct, 并把第一个返回值给json encode之后放在response body里输出. 如果error
不为空, 则按以下格式输出:
```json
{
  "Code": 500,
  "Msg": "service method error", 
  "Data": "error messages"
}
```

`func(*ServiceMethodContext, *struct) (error)`

这种情况下, 框架会把url pattern/json/query string解析成struct, 然后用户需要自己定制化返回response body. 如果error不为空, 也会按上一种
那样输出response body.

**以上两种函数原型基本上囊括了绝大部分http json api的场景.**

### 我的函数并不关心输入的参数怎么办?

直接这样定义函数就好了:
```go
func getSomething(_ *kellyframework.ServiceMethodContext, _ *struct{}) (anything, error) {
    return anything
}
```

### 看上去不错, 但那些struct的字段的有效性验证起来很麻烦.

kellyframework集成了[validator](https://godoc.org/gopkg.in/go-playground/validator.v9), 可以使用validator的struct tag语法为
struct添加各种约束.

例如:
```go
type userInfo struct {
    Age int        `validate:"max=140,min=0"`
    Address string
}
```

一旦输入的字段不符合约束, http框架即会返回HTTP的400错误.

### `kellyframework.ServiceMethodContext`是干吗用的?

这个结构体包含以下字段:
```go
type ServiceMethodContext struct {
	Context            context.Context // 该HTTP请求的context
	XForwardedFor      string // HTTP的X-Forwarded-For请求头, 通常这意味着真正发起请求的IP地址
	RemoteAddr         string // 发起请求的远端地址, 有可能是反向代理服务器的IP地址.
	RequestBodyReader  io.ReadCloser // request body
	ResponseBodyWriter io.Writer // response body
}
```
这些字段都可以随便使用.

但需要注意的是, 如果你想自己处理request body, 那么你的client就不应当添加"content-type: application/json"的头, 否则框架就会尝试去读取
request body并按json格式去decode.

而如果你想自己返回response body, 那么你的函数就应当只有一个error的返回值, 这样框架就不会尝试按json格式去encode你返回的结构体并在response 
body里返给client.

### access log是否支持自动切分?

日志切分这个事情并不应当由HTTP JSON API框架来完成, 你可以用[autosplitfile](https://github.com/abadcafe/autosplitfile)来替代普通的
os.File传进kellyframework.NewLoggingHTTPRouter()方法里, 这样你的access log就是自动切分的了.
