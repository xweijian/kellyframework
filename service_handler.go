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
	loggerContextKey   interface{}
	method             *serviceMethod
	validator          *validator.Validate
	bypassRequestBody  bool
	bypassResponseBody bool
}

type ErrorResponse struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

type serviceMethod struct {
	value   reflect.Value
	argType reflect.Type
}

type panicStack struct {
	Panic string `json:"panic"`
	Stack string `json:"stack"`
}

const traceFamily = "kellyframework.ServiceHandler"

var formDecoder = schema.NewDecoder()

func init() {
	formDecoder.IgnoreUnknownKeys(true)
}

func checkServiceMethodPrototype(methodType reflect.Type) error {
	if methodType.Kind() != reflect.Func {
		return fmt.Errorf("you should provide a function or object method")
	}

	if methodType.NumIn() != 2 {
		return fmt.Errorf("the service method should have two arguments")
	}

	if methodType.In(0).Kind() != reflect.Ptr || methodType.In(0).Elem().Name() != "ServiceMethodContext" {
		return fmt.Errorf("the first argument should be type *ServiceMethodContext")
	}

	if methodType.In(1).Kind() != reflect.Ptr || methodType.In(1).Elem().Kind() != reflect.Struct {
		return fmt.Errorf("the second argument should be a struct pointer")
	}

	if methodType.NumOut() != 1 {
		return fmt.Errorf("the service method should have only one return value")
	}

	return nil
}

func NewServiceHandler(method interface{}, loggerContextKey interface{}, bypassRequestBody bool,
	bypassResponseBody bool) (h *ServiceHandler, err error) {
	// the method prototype like this: 'func(*ServiceMethodContext, *struct) (anything)'
	methodType := reflect.TypeOf(method)
	err = checkServiceMethodPrototype(methodType)
	if err != nil {
		return
	}

	h = &ServiceHandler{
		loggerContextKey,
		&serviceMethod{
			reflect.ValueOf(method),
			methodType.In(1),
		},
		validator.New(),
		bypassRequestBody,
		bypassResponseBody,
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
	json.NewEncoder(w).Encode(data)
}

func writeErrorResponse(w http.ResponseWriter, tr trace.Trace, resp *ErrorResponse) {
	tr.LazyPrintf("%s: %+v", resp.Msg, resp.Data)
	tr.SetError()
	setJSONResponseHeader(w)
	w.WriteHeader(resp.Code)
	json.NewEncoder(w).Encode(resp)
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
	if !h.bypassRequestBody && r.Header.Get("Content-Type") == "application/json" {
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
		writeErrorResponse(rw, tracer, &ErrorResponse{400, "parse argument failed", err.Error()})
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

	// write returned value or error to response.
	if methodPanic == nil && len(out) != 1 {
		// the method prototype have more than one return value, it is forbidden.
		panic(fmt.Sprintf("return values error: %+v", out))
	}

	var respData interface{}
	if methodPanic != nil {
		respData = &ErrorResponse{500, "service method panicked", methodPanic}
		writeErrorResponse(rw, tracer, respData.(*ErrorResponse))
	} else {
		methodReturn := out[0].Interface()
		ok := false
		if respData, ok = methodReturn.(*ErrorResponse); ok {
			if respData.(*ErrorResponse) != nil {
				writeErrorResponse(rw, tracer, respData.(*ErrorResponse))
			}
		} else if err, ok = methodReturn.(error); ok {
			respData = &ErrorResponse{500, "service method error", err.Error()}
			writeErrorResponse(rw, tracer, respData.(*ErrorResponse))
		} else if !h.bypassResponseBody {
			// write to response body as JSON encoded string
			respData = methodReturn
			writeJSONResponse(rw, tracer, respData)
		}
	}

	// record some thing if logger existed.
	if h.loggerContextKey != nil {
		logger := r.Context().Value(h.loggerContextKey).(MethodCallLogger)
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
