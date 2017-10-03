package kellyframework

import (
	"reflect"
	"fmt"
	"net/http"
	"code.corp.elong.com/aos/kellyframework/argument_extrator"
	"strings"
	"context"
	"io"
	"time"
	"gopkg.in/go-playground/validator.v9"
	"encoding/json"
	"golang.org/x/net/trace"
	"runtime"
	"strconv"
)

type ServiceMethodContext struct {
	Context            context.Context
	RemoteAddr         string
	RequestBodyReader  io.ReadCloser
	ResponseBodyWriter io.Writer
}

type MethodCallLogger interface {
	Record(field string, value string)
}

type ServiceHandler struct {
	methodCallLoggerContextKey interface{}
	methods                    map[string]*serviceMethod
	validator                  *validator.Validate
}

type serviceMethod struct {
	value      *reflect.Value
	argType    reflect.Type
	returnType reflect.Type
}

type errorResponseBody struct {
	Code    int         `json:"code"`
	Summary string      `json:"summary"`
	Data    interface{} `json:"data"`
}

func NewServiceHandler(methodCallLoggerContextKey interface{}) *ServiceHandler {
	return &ServiceHandler{
		methodCallLoggerContextKey,
		make(map[string]*serviceMethod),
		validator.New(),
	}
}

func (h *ServiceHandler) RegisterMethodWithName(callee interface{}, name string) error {
	// two method prototypes are accept:
	// 1. 'func(*ServiceMethodContext, *struct) (*struct, error)' which for normal use.
	// 2. 'func(*ServiceMethodContext, *struct) (error)' which for custom response data such as a data stream.
	calleeType := reflect.TypeOf(callee)
	calleeValue := reflect.ValueOf(callee)

	if calleeType.Kind() != reflect.Func {
		return fmt.Errorf("you should register a function or object method")
	}

	if calleeType.NumIn() == 2 {
		if calleeType.In(0).Kind() != reflect.Ptr || calleeType.In(0).Elem().Name() != "ServiceMethodContext" {
			return fmt.Errorf("the first argument should be type *ServiceMethodContext")
		}

		if calleeType.In(1).Kind() != reflect.Ptr || calleeType.In(1).Elem().Kind() != reflect.Struct {
			return fmt.Errorf("the second argument should be a struct pointer")
		}
	} else {
		return fmt.Errorf("the service method should have two arguments")
	}

	if calleeType.NumOut() == 2 {
		if calleeType.Out(0).Kind() != reflect.Ptr || calleeType.Out(0).Elem().Kind() != reflect.Struct{
			return fmt.Errorf("the first return value should be a struct pointer")
		}

		if calleeType.Out(1).Kind() != reflect.Interface || calleeType.Out(1).Name() != "error" {
			return fmt.Errorf("the second return value should be error interface")
		}
	} else if calleeType.NumOut() == 1 {
		if calleeType.Out(0).Kind() != reflect.Interface || calleeType.Out(0).Name() != "error" {
			return fmt.Errorf("the return value should be error interface")
		}
	} else {
		return fmt.Errorf("the service method should have one or two return values")
	}

	h.methods[name] = &serviceMethod{
		&calleeValue,
		calleeType.In(1),
		calleeType.Out(0),
	}

	return nil
}

func newArgumentExtractor(req *http.Request) (extractor argument_extrator.ArgumentExtractor) {
	contentType := req.Header.Get("Content-Type")
	if contentType == "application/json" {
		extractor = argument_extrator.NewJSONArgumentExtractor(req)
	} else {
		// even the content type was not "application/x-www-form-urlencoded", the form request codec also can parse
		// arguments encoded in query string.
		extractor = argument_extrator.NewFormArgumentExtractor(req)
	}

	return
}

func setJSONResponseHeader(w http.ResponseWriter) {
	// Prevents Internet Explorer from MIME-sniffing a response away from the declared content-type
	w.Header().Set("x-content-type-options", "nosniff")
	w.Header().Set("Content-Type", "application/json")
}

func writeJSONResponse(w http.ResponseWriter, tr trace.Trace, data interface{}) {
	tr.LazyPrintf("%+v", data)
	setJSONResponseHeader(w)
	enc := json.NewEncoder(w)
	enc.Encode(data)
}

func writeErrorResponse(w http.ResponseWriter, tr trace.Trace, data interface{}, code int, summary string) {
	tr.LazyPrintf("%s: %+v", summary, data)
	tr.SetError()
	setJSONResponseHeader(w)
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.Encode(&errorResponseBody{code, summary, data})
}

func doServiceMethodCall(method *serviceMethod, in []reflect.Value) (out []reflect.Value, panic interface{}) {
	defer func() {
		panic = recover()
	}()

	out = method.value.Call(in)
	return
}

func extractMethodName(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func (h *ServiceHandler) ServeHTTP(respWriter http.ResponseWriter, req *http.Request) {
	pc, _, _, ok := runtime.Caller(0)
	if !ok {
		panic("can not ascend the stack!")
	}

	tracer := trace.New(runtime.FuncForPC(pc).Name(), req.URL.Path)
	defer tracer.Finish()

	methodName := extractMethodName(req.URL.Path)
	method, ok := h.methods[methodName]
	if !ok {
		writeErrorResponse(respWriter, tracer, nil, 404, "service method not found")
		return
	}

	// extract arguments.
	argExtractor := newArgumentExtractor(req)
	args := reflect.New(method.argType.Elem())

	argError := argExtractor.ExtractTo(args.Interface())
	if argError != nil {
		writeErrorResponse(respWriter, tracer, argError.Error(), 400, "parse arguments failed")
		return
	}

	if args.Elem().Kind() == reflect.Struct {
		argError = h.validator.Struct(args.Interface())
		if argError != nil {
			writeErrorResponse(respWriter, tracer, argError.Error(), 400, "arguments invalid")
			return
		}
	}

	// do method call.
	beginTime := time.Now()
	out, methodPanic := doServiceMethodCall(method, []reflect.Value{
		reflect.ValueOf(&ServiceMethodContext{
			req.Context(),
			req.RemoteAddr,
			req.Body,
			respWriter,
		}),
		args,
	})
	duration := time.Now().Sub(beginTime)

	// write return values or errors to response.
	var methodReturn interface{}
	var methodError interface{}
	if len(out) == 2 {
		methodReturn = out[0].Interface()
		methodError = out[1].Interface()
	} else if len(out) == 1 {
		methodError = out[0].Interface()
	} else if methodPanic == nil {
		// the method prototype is neither 1 return value nor 2 return values, it is unlikely
		panic(fmt.Sprintf("return values error: %+v", out))
	}

	var respData interface{}
	if methodPanic != nil {
		respData = methodPanic
		writeErrorResponse(respWriter, tracer, respData, 500, "service method panicked")
	} else if methodError != nil {
		respData = methodError.(error).Error()
		writeErrorResponse(respWriter, tracer, respData, 500, "service method error")
	} else if len(out) == 2 {
		// write to response body as JSON encoded string while prototype has two return values, even when the response
		// data is nil.
		respData = methodReturn
		writeJSONResponse(respWriter, tracer, respData)
	}

	// record some thing if logger existed.
	if h.methodCallLoggerContextKey != nil {
		logger := req.Context().Value(h.methodCallLoggerContextKey).(MethodCallLogger)
		if logger != nil {
			marshaledArgs, err := json.Marshal(args.Interface())
			if err != nil {
				panic(err)
			}

			marshaledData, err := json.Marshal(respData)
			if err != nil {
				panic(err)
			}

			logger.Record("methodCallArgument", string(marshaledArgs))
			logger.Record("methodCallResponseData", string(marshaledData))
			logger.Record("methodCallBeginTime", beginTime.String())
			logger.Record("methodCallDuration", strconv.FormatFloat(duration.Seconds(), 'f', -1, 64))
		}
	}
}
