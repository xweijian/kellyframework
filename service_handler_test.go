package kellyframework

import (
	"net/http"
	"testing"
	"strings"

	"fmt"
	"net/http/httptest"
)

func TestNewServiceHandler(t *testing.T) {
	type args struct {
		ctxkey interface{}
	}
	tests := []struct {
		name string
		args args
	}{
		{"nil case", args{nil}},
		{"string case", args{"fafafdsfs"}},
		{"number case", args{123}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewServiceHandler(tt.args.ctxkey); got == nil {
				t.Errorf("NewServiceHandler() = %v", got)
			}
		})
	}
}

type empty struct {
}

type validatorEnabled struct {
	A int `validate:"required"`
}

func (e *empty) errorMethod(*ServiceMethodContext, *empty) (*empty, error) {
	return nil, fmt.Errorf("expected error")
}

func (e *empty) panicMethod(*ServiceMethodContext, *empty) (*empty, error) {
	panic("expected panic")
	return nil, nil
}

func emptyFunction(*ServiceMethodContext, *empty) (*empty, error) {
	return nil, nil
}

func validatorEnabledFunction(*ServiceMethodContext, *validatorEnabled) (*empty, error) {
	return nil, nil
}

func TestServiceHandler_RegisterMethodWithName(t *testing.T) {
	e := empty{}
	type args struct {
		callee interface{}
		name   string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"not method", args{1, "fafads"}, true},
		{"wrong method arguments", args{func() {}, "fafads"}, true},
		{"wrong method return values", args{func(int, int) {}, "fafads"}, true},
		{"wrong method prototype", args{func(*ServiceMethodContext, int) error { return nil }, "fafads"}, true},
		{"wrong method prototype", args{func(*ServiceMethodContext, int) (int, error) { return 0, nil }, "fafads"}, true},
		{"wrong method prototype", args{func(ServiceMethodContext, struct{}) (int, error) { return 0, nil }, "fafads"}, true},
		{"wrong method prototype", args{func(*ServiceMethodContext, struct{}) (int, error) { return 0, nil }, "fafads"}, true},
		{"wrong method prototype", args{func(*ServiceMethodContext, *struct{}) (int, error) { return 0, nil }, "fafads"}, true},
		{"empty function prototype", args{emptyFunction, "gsfdgs"}, false},
		{"object member", args{e.errorMethod, "fafads"}, false},
	}

	h := NewServiceHandler(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := h.RegisterMethodWithName(tt.args.callee, tt.args.name); (err != nil) != tt.wantErr {
				t.Errorf("ServiceHandler.RegisterMethodWithName() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestServiceHandler_ServeHTTP(t *testing.T) {
	h := NewServiceHandler(nil)
	e := empty{}

	h.RegisterMethodWithName(emptyFunction, "emptyFunction")
	h.RegisterMethodWithName(e.errorMethod, "errorMethod")
	h.RegisterMethodWithName(e.panicMethod, "panicMethod")
	h.RegisterMethodWithName(validatorEnabledFunction, "validatorEnabledFunction")

	emptyFunctionWrongPath := httptest.NewRequest("POST", "/wrong", strings.NewReader("{312}"))
	emptyFunctionWrongPath.Header.Add("content-type", "application/json")

	emptyFunctionWrongArguments := httptest.NewRequest("POST", "/emptyFunction", strings.NewReader("{312}"))
	emptyFunctionWrongArguments.Header.Add("content-type", "application/json")

	emptyFunctionEmptyArguments := httptest.NewRequest("POST", "/emptyFunction", strings.NewReader(""))
	emptyFunctionEmptyArguments.Header.Add("content-type", "application/json")

	errorMethodNormalArguments := httptest.NewRequest("POST", "/errorMethod", strings.NewReader("{}"))
	errorMethodNormalArguments.Header.Add("content-type", "application/json")

	panicMethodNormalArguments := httptest.NewRequest("POST", "/panicMethod", strings.NewReader("{}"))
	panicMethodNormalArguments.Header.Add("content-type", "application/json")

	emptyFunctionNormalArguments := httptest.NewRequest("POST", "/emptyFunction", strings.NewReader("{}"))
	emptyFunctionNormalArguments.Header.Add("content-type", "application/json")

	validatorEnabledFunctionInvalidArguments := httptest.NewRequest("POST", "/validatorEnabledFunction", strings.NewReader("{}"))
	validatorEnabledFunctionInvalidArguments.Header.Add("content-type", "application/json")

	validatorEnabledFunctionNormalArguments := httptest.NewRequest("POST", "/validatorEnabledFunction", strings.NewReader("{\"A\": 1}"))
	validatorEnabledFunctionNormalArguments.Header.Add("content-type", "application/json")

	type args struct {
		respWriter http.ResponseWriter
		req        *http.Request
	}

	tests := []struct {
		name string
		args args
	}{
		{"wrong path", args{httptest.NewRecorder(), emptyFunctionWrongPath}},
		{"wrong arguments", args{httptest.NewRecorder(), emptyFunctionWrongArguments}},
		{"empty arguments", args{httptest.NewRecorder(), emptyFunctionEmptyArguments}},
		{"error", args{httptest.NewRecorder(), errorMethodNormalArguments}},
		{"panic", args{httptest.NewRecorder(), panicMethodNormalArguments}},
		{"validator enabled invalid arguments", args{httptest.NewRecorder(), validatorEnabledFunctionInvalidArguments}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h.ServeHTTP(tt.args.respWriter, tt.args.req)
			if tt.args.respWriter.(*httptest.ResponseRecorder).Code == 200 {
				t.Errorf("code could not be 200!")
			}
			fmt.Println(tt.args.respWriter.(*httptest.ResponseRecorder).Body)
		})
	}

	tests = []struct {
		name string
		args args
	}{
		{"normal call", args{httptest.NewRecorder(), emptyFunctionNormalArguments}},
		{"validator enabled normal arguments", args{httptest.NewRecorder(), validatorEnabledFunctionNormalArguments}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h.ServeHTTP(tt.args.respWriter, tt.args.req)
			if tt.args.respWriter.(*httptest.ResponseRecorder).Code != 200 {
				t.Errorf("%v\n", tt.args.respWriter.(*httptest.ResponseRecorder).Body)
			}
			fmt.Println(tt.args.respWriter.(*httptest.ResponseRecorder).Body)
		})
	}
}
