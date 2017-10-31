package kellyframework

import (
	"reflect"
	"fmt"
	"net/http"
	"context"
	"io"
	"time"
	"gopkg.in/go-playground/validator.v9"
	"encoding/json"
	"golang.org/x/net/trace"
	"strconv"
	"runtime/debug"
	"github.com/julienschmidt/httprouter"
	"github.com/gorilla/schema"
	"net/url"
)

type ServiceMethodContext struct {
	Context            context.Context
	XForwardedFor      string
	RemoteAddr         string
	RequestBodyReader  io.ReadCloser
	ResponseBodyWriter io.Writer
}

type MethodCallLogger interface {
	Record(field string, value string)
}

type ServiceHandler struct {
	methodCallLoggerContextKey interface{}
	method                     *serviceMethod
	validator                  *validator.Validate
}

type PathFunctionPair struct {
	Path     string
	Function interface{}
}

type MethodPathFunctionTriple struct {
	Method   string
	Path     string
	Function interface{}
}

type serviceMethod struct {
	value   reflect.Value
	argType reflect.Type
}

type errorResponseBody struct {
	Code    int         `json:"code"`
	Summary string      `json:"summary"`
	Data    interface{} `json:"data"`
}

type panicStack struct {
	Panic string `json:"panic"`
	Stack string `json:"stack"`
}

const traceFamily = "kellyframework.ServiceHandler"

var formDecoder = schema.NewDecoder()

func checkServiceMethodPrototype(methodType reflect.Type) error {
	if methodType.Kind() != reflect.Func {
		return fmt.Errorf("you should provide a function or object method")
	}

	if methodType.NumIn() == 2 {
		if methodType.In(0).Kind() != reflect.Ptr || methodType.In(0).Elem().Name() != "ServiceMethodContext" {
			return fmt.Errorf("the first argument should be type *ServiceMethodContext")
		}

		if methodType.In(1).Kind() != reflect.Ptr || methodType.In(1).Elem().Kind() != reflect.Struct {
			return fmt.Errorf("the second argument should be a struct pointer")
		}
	} else {
		return fmt.Errorf("the service method should have two arguments")
	}

	if methodType.NumOut() == 2 {
		if methodType.Out(1).Kind() != reflect.Interface || methodType.Out(1).Name() != "error" {
			return fmt.Errorf("the second return value should be error interface")
		}
	} else if methodType.NumOut() == 1 {
		if methodType.Out(0).Kind() != reflect.Interface || methodType.Out(0).Name() != "error" {
			return fmt.Errorf("the return value should be error interface")
		}
	} else {
		return fmt.Errorf("the service method should have one or two return values")
	}

	return nil
}

func NewServiceHandler(method interface{}, methodCallLoggerContextKey interface{}) (h *ServiceHandler, err error) {
	// two method prototypes are accept:
	// 1. 'func(*ServiceMethodContext, *struct) (anything, error)' which for normal use.
	// 2. 'func(*ServiceMethodContext, *struct) (error)' which for custom response data such as a data stream.
	methodType := reflect.TypeOf(method)
	err = checkServiceMethodPrototype(methodType)
	if err != nil {
		return
	}

	h = &ServiceHandler{
		methodCallLoggerContextKey,
		&serviceMethod{
			reflect.ValueOf(method),
			methodType.In(1),
		},
		validator.New(),
	}

	return
}

func RegisterFunctionsToServeMux(mux *http.ServeMux, methodCallLoggerContextKey interface{},
	pairs []*PathFunctionPair) error {
	for _, pair := range pairs {
		handler, err := NewServiceHandler(pair.Function, methodCallLoggerContextKey)
		if err != nil {
			return err
		}

		mux.Handle(pair.Path, handler)
	}

	return nil
}

func RegisterFunctionsToHTTPRouter(r *httprouter.Router, methodCallLoggerContextKey interface{},
	triples []*MethodPathFunctionTriple) error {
	for _, t := range triples {
		handler, err := NewServiceHandler(t.Function, methodCallLoggerContextKey)
		if err != nil {
			return err
		}

		r.Handle(t.Method, t.Path, handler.ServeHTTPWithParams)
	}

	return nil
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

func doServiceMethodCall(method *serviceMethod, in []reflect.Value) (out []reflect.Value, ps *panicStack) {
	defer func() {
		if panicInfo := recover(); panicInfo != nil {
			ps = &panicStack{
				fmt.Sprintf("%s", panicInfo),
				fmt.Sprintf("%s", debug.Stack()),
			}
		}
	}()

	out = method.value.Call(in)
	return
}

func (h *ServiceHandler) parseArgument(r *http.Request, params httprouter.Params, arg interface{}) error {
	// query string has lowest priority.
	err := r.ParseForm()
	if err != nil {
		return err
	}

	err = formDecoder.Decode(arg, r.Form)
	if err != nil {
		return err
	}

	// json content is prior to query string.
	if r.Header.Get("Content-Type") == "application/json" {
		err := json.NewDecoder(r.Body).Decode(arg)
		if err != nil {
			return err
		}
	}

	// params is prior to json content.
	if params != nil {
		paramValues := url.Values{}
		for _, param := range params {
			paramValues.Set(param.Key, param.Value)
		}

		err = formDecoder.Decode(arg, paramValues)
		if err != nil {
			return err
		}
	}

	err = h.validator.Struct(arg)
	if err != nil {
		return err
	}

	return nil
}

func (h *ServiceHandler) ServeHTTP(respWriter http.ResponseWriter, req *http.Request) {
	h.ServeHTTPWithParams(respWriter, req, nil)
}

func (h *ServiceHandler) ServeHTTPWithParams(rw http.ResponseWriter, r *http.Request, params httprouter.Params) {
	tracer := trace.New(traceFamily, r.URL.Path)
	defer tracer.Finish()

	// extract arguments.
	arg := reflect.New(h.method.argType.Elem())
	err := h.parseArgument(r, params, arg.Interface())
	if err != nil {
		writeErrorResponse(rw, tracer, err.Error(), 400, "parse argument failed")
		return
	}

	// do method call.
	beginTime := time.Now()
	out, methodPanic := doServiceMethodCall(h.method, []reflect.Value{
		reflect.ValueOf(&ServiceMethodContext{
			r.Context(),
			r.Header.Get("X-Forwarded-For"),
			r.RemoteAddr,
			r.Body,
			rw,
		}),
		arg,
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
		writeErrorResponse(rw, tracer, respData, 500, "service method panicked")
	} else if methodError != nil {
		respData = methodError.(error).Error()
		writeErrorResponse(rw, tracer, respData, 500, "service method error")
	} else if len(out) == 2 {
		// write to response body as JSON encoded string while prototype has two return values, even when the response
		// data is nil.
		respData = methodReturn
		writeJSONResponse(rw, tracer, respData)
	}

	// record some thing if logger existed.
	if h.methodCallLoggerContextKey != nil {
		logger := r.Context().Value(h.methodCallLoggerContextKey).(MethodCallLogger)
		if logger != nil {
			marshaledArgs, err := json.Marshal(arg.Interface())
			if err != nil {
				panic(err)
			}

			marshaledData, err := json.Marshal(respData)
			if err != nil {
				panic(err)
			}

			logger.Record("methodCallArgument", string(marshaledArgs))
			logger.Record("methodCallResponseData", string(marshaledData))
			logger.Record("methodCallBeginTime", beginTime.Format("2006-01-02 03:04:05.999999999"))
			logger.Record("methodCallDuration", strconv.FormatFloat(duration.Seconds(), 'f', -1, 64))
		}
	}
}
