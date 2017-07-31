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
)

type ServiceMethodContext struct {
	context            context.Context
	remoteAddr         string
	requestBodyReader  io.ReadCloser
	responseBodyWriter io.Writer
}

type MethodCallRecorder interface {
	Record(field string, value interface{})
}

type ServiceHandler struct {
	recorderContextKey interface{}
	methods            map[string]*serviceMethod
	validator          *validator.Validate
}

type serviceMethod struct {
	value      *reflect.Value
	argType    reflect.Type
	returnType reflect.Type
}

func NewServiceHandler(ctxkey interface{}) *ServiceHandler {
	return &ServiceHandler{
		ctxkey,
		make(map[string]*serviceMethod),
		validator.New(),
	}
}

func (h *ServiceHandler) RegisterMethodWithName(callee interface{}, name string) error {
	// the ServiceHandler method prototype should be func(ServiceMethodContext, struct) (struct, error) .
	calleeType := reflect.TypeOf(callee)
	calleeValue := reflect.ValueOf(callee)

	if calleeType.Kind() != reflect.Func {
		return fmt.Errorf("you should register a function or object method!")
	}

	if calleeType.NumIn() != 2 {
		return fmt.Errorf("the service method should have two arguments!")
	}

	if calleeType.NumOut() != 2 {
		return fmt.Errorf("the service method should have two return values!")
	}

	if calleeType.In(0).Kind() != reflect.Ptr ||
		strings.HasSuffix(calleeType.In(0).Name(), ".ServiceMethodContext") {
		return fmt.Errorf("the first argument should be type ServiceMethodContext!")
	}

	if calleeType.In(1).Kind() != reflect.Ptr ||
		strings.HasSuffix(calleeType.In(1).Name(), ".error") {
		return fmt.Errorf("the second argument should be a struct!")
	}

	if calleeType.Out(0).Kind() != reflect.Ptr {
		return fmt.Errorf("the first return value should be a struct!")
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

type responseBody struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

func writeResponse(w http.ResponseWriter, tr trace.Trace, data interface{}, code int, msg string) {
	if code != 200 {
		tr.LazyPrintf("%s: %+v", msg, data)
		tr.SetError()
	} else {
		tr.LazyPrintf(msg)
	}

	// Prevents Internet Explorer from MIME-sniffing a response away from the declared content-type
	w.Header().Set("x-content-type-options", "nosniff")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.Encode(&responseBody{code, msg, data})
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
		writeResponse(respWriter, tracer, nil, 404, "service method not found")
		return
	}

	// extract arguments.
	argExtractor := newArgumentExtractor(req)
	args := reflect.New(method.argType.Elem())

	err := argExtractor.ExtractTo(args.Interface())
	if err != nil {
		writeResponse(respWriter, tracer, err.Error(), 400, "parse arguments failed")
		return
	}

	if args.Elem().Kind() == reflect.Struct {
		err = h.validator.Struct(args.Interface())
		if err != nil {
			writeResponse(respWriter, tracer, err.Error(), 400, "arguments invalid")
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

	if h.recorderContextKey != nil {
		recorder := req.Context().Value(h.recorderContextKey).(MethodCallRecorder)
		if recorder != nil {
			marshaled, err := json.Marshal(args.Interface())
			if err != nil {
				panic(err)
			}

			recorder.Record("serviceMethodArgument", string(marshaled))
			recorder.Record("serviceMethodBeginTime", beginTime.String())
			recorder.Record("serviceMethodDuration", duration.Seconds())
		}
	}

	// write return values or errors to response.
	if methodPanic != nil {
		writeResponse(respWriter, tracer, methodPanic, 500, "service method panicked")
	} else {
		retData := out[0].Interface()
		retErr := out[1].Interface()
		if retErr != nil {
			writeResponse(respWriter, tracer, retErr.(error).Error(), 500, "service method error")
		} else if retData != nil {
			writeResponse(respWriter, tracer, retData, 200, "success")
		}
		// nothing to do if retData is nil because it means the service method has written custom data to response body
		// already.
	}
}
