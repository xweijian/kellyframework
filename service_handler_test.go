package kellyframework

import (
	"testing"
	"strings"
	"fmt"
	"net/http/httptest"
	"reflect"
)

type empty struct {
}

type validatorEnabled struct {
	A int `validate:"required"`
}

var e = empty{}

func (e *empty) errorMethod(*ServiceMethodContext, *empty) (*struct{}, error) {
	return nil, fmt.Errorf("expected error")
}

func (e *empty) panicMethod(*ServiceMethodContext, *empty) (*struct{}, error) {
	panic("expected panic")
	return nil, nil
}

func emptyFunction(*ServiceMethodContext, *empty) (*struct{ A int }, error) {
	return &struct {
		A int
	}{1}, nil
}

func validatorEnabledFunction(*ServiceMethodContext, *validatorEnabled) (error) {
	return nil
}

func TestServiceHandlerCheckServiceMethodPrototype(t *testing.T) {
	t.Run("not function", func(t *testing.T) {
		if err := checkServiceMethodPrototype(reflect.TypeOf(1)); err == nil {
			t.Error()
		}
	})

	t.Run("argument count wrong", func(t *testing.T) {
		if err := checkServiceMethodPrototype(reflect.TypeOf(func() {})); err == nil {
			t.Error()
		}
	})

	t.Run("first argument type wrong", func(t *testing.T) {
		if err := checkServiceMethodPrototype(reflect.TypeOf(func(*struct{}, *struct{}) {})); err == nil {
			t.Error()
		}
	})

	t.Run("second argument type wrong", func(t *testing.T) {
		if err := checkServiceMethodPrototype(reflect.TypeOf(func(*ServiceMethodContext, struct{}) {})); err == nil {
			t.Error()
		}
	})

	t.Run("return value count wrong", func(t *testing.T) {
		if err := checkServiceMethodPrototype(reflect.TypeOf(func(*ServiceMethodContext, *struct{}) {})); err == nil {
			t.Error()
		}
	})

	t.Run("single return value type wrong", func(t *testing.T) {
		if err := checkServiceMethodPrototype(reflect.TypeOf(func(*ServiceMethodContext, *struct{}) int { return 0 })); err == nil {
			t.Error()
		}
	})

	t.Run("two return values second type wrong", func(t *testing.T) {
		if err := checkServiceMethodPrototype(reflect.TypeOf(func(*ServiceMethodContext, *struct{}) (int, int) { return 0, 0 })); err == nil {
			t.Error()
		}
	})

	t.Run("normal function", func(t *testing.T) {
		if err := checkServiceMethodPrototype(reflect.TypeOf(emptyFunction)); err != nil {
			t.Error()
		}
	})

	t.Run("normal object method", func(t *testing.T) {
		if err := checkServiceMethodPrototype(reflect.TypeOf(emptyFunction)); err != nil {
			t.Error()
		}
	})

	t.Run("normal object method", func(t *testing.T) {
		if err := checkServiceMethodPrototype(reflect.TypeOf(e.errorMethod)); err != nil {
			t.Error()
		}
	})
}

func TestServiceHandlerServeHTTP(t *testing.T) {
	h1, _ := NewServiceHandler(emptyFunction, nil)
	h2, _ := NewServiceHandler(e.errorMethod, nil)
	h3, _ := NewServiceHandler(e.panicMethod, nil)
	h4, _ := NewServiceHandler(validatorEnabledFunction, nil)

	emptyFunctionWrongArguments := httptest.NewRequest("POST", "/emptyFunction", strings.NewReader("{312}"))
	emptyFunctionWrongArguments.Header.Add("content-type", "application/json")
	t.Run("wrong arguments", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		h1.ServeHTTP(recorder, emptyFunctionWrongArguments)
		if recorder.Code == 200 {
			t.Error("code could not be 200, body:", recorder.Body)
		}
	})

	emptyFunctionEmptyArguments := httptest.NewRequest("POST", "/emptyFunction", strings.NewReader(""))
	emptyFunctionEmptyArguments.Header.Add("content-type", "application/json")
	t.Run("empty arguments", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		h1.ServeHTTP(recorder, emptyFunctionEmptyArguments)
		if recorder.Code == 200 {
			t.Error("code could not be 200, body:", recorder.Body)
		}
	})

	emptyFunctionNormalArguments := httptest.NewRequest("POST", "/emptyFunction", strings.NewReader("{}"))
	emptyFunctionNormalArguments.Header.Add("content-type", "application/json")
	t.Run("normal arguments", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		h1.ServeHTTP(recorder, emptyFunctionNormalArguments)
		if recorder.Code != 200 {
			t.Error("code is not 200, body:", recorder.Body)
		}
	})

	errorMethodNormalArguments := httptest.NewRequest("POST", "/errorMethod", strings.NewReader("{}"))
	errorMethodNormalArguments.Header.Add("content-type", "application/json")
	t.Run("error", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		h2.ServeHTTP(recorder, errorMethodNormalArguments)
		if recorder.Code == 200 {
			t.Error("code could not be 200, body:", recorder.Body)
		}
	})

	panicMethodNormalArguments := httptest.NewRequest("POST", "/panicMethod", strings.NewReader("{}"))
	panicMethodNormalArguments.Header.Add("content-type", "application/json")
	t.Run("panic", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		h3.ServeHTTP(recorder, panicMethodNormalArguments)
		if recorder.Code == 200 {
			t.Error("code could not be 200, body:", recorder.Body)
		}
	})

	validatorEnabledFunctionInvalidArguments := httptest.NewRequest("POST", "/validatorEnabledFunction", strings.NewReader("{}"))
	validatorEnabledFunctionInvalidArguments.Header.Add("content-type", "application/json")
	t.Run("validator enabled invalid arguments", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		h4.ServeHTTP(recorder, validatorEnabledFunctionInvalidArguments)
		if recorder.Code == 200 {
			t.Error("code could not be 200, body:", recorder.Body)
		}
	})

	validatorEnabledFunctionInvalidQueryString := httptest.NewRequest("POST", "/validatorEnabledFunction?fadfa", strings.NewReader("{\"A\": 1}"))
	validatorEnabledFunctionInvalidQueryString.Header.Add("content-type", "application/json")
	t.Run("validator enabled invalid arguments", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		h4.ServeHTTP(recorder, validatorEnabledFunctionInvalidQueryString)
		if recorder.Code == 200 {
			t.Error("code could not be 200, body:", recorder.Body)
		}
	})

	validatorEnabledFunctionNormalArguments := httptest.NewRequest("POST", "/validatorEnabledFunction", strings.NewReader("{\"A\": 1}"))
	validatorEnabledFunctionNormalArguments.Header.Add("content-type", "application/json")
	t.Run("validator enabled normal arguments", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		h4.ServeHTTP(recorder, validatorEnabledFunctionNormalArguments)
		if recorder.Code != 200 {
			t.Error("code is not 200, body:", recorder.Body)
		}
	})
}
