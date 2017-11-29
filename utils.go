package kellyframework

import (
	"net/http"
	"io"
	"github.com/julienschmidt/httprouter"
)

type methodCallLogger struct {
	row *AccessLogRow
}

const ServiceHandlerAccessLogRowFillerContextKey = "kellyframework.ServiceHandlerAccessLogRowFiller"

func (l *methodCallLogger) Record(field string, value string) {
	l.row.SetRowField(field, value)
}

func ServiceHandlerAccessLogRowFillerFactory(row *AccessLogRow) AccessLogRowFiller {
	return &methodCallLogger{row}
}

type MethodPathFunctionTriple struct {
	Method   string
	Path     string
	Function interface{}
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

func NewHTTPRouter(triples []*MethodPathFunctionTriple) (*httprouter.Router, error) {
	router := httprouter.New()
	err := RegisterFunctionsToHTTPRouter(router, ServiceHandlerAccessLogRowFillerContextKey, triples)
	if err != nil {
		return nil, err
	}

	return router, nil
}

func NewLoggingHTTPRouter(triples []*MethodPathFunctionTriple, logWriter io.Writer) (http.Handler, error) {
	router, err := NewHTTPRouter(triples)
	if err != nil {
		return nil, err
	}

	return NewAccessLogDecorator(router, logWriter, ServiceHandlerAccessLogRowFillerContextKey,
		ServiceHandlerAccessLogRowFillerFactory), nil
}
