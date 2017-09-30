package kellyframework

import (
	"net/http"
	"context"
	"time"
	"io"
	"github.com/Sirupsen/logrus"
)

type AccessLogDecorator struct {
	http.Handler
	rowFillerContextKey interface{}
	rowFillerFactory    AccessLogRowFillerFactory
	logger              *logrus.Logger
}

type AccessLogRow struct {
	fields logrus.Fields
}

type AccessLogRowFiller interface{}
type AccessLogRowFillerFactory func(*AccessLogRow) AccessLogRowFiller

func (row *AccessLogRow) SetRowField(field string, value interface{}) {
	row.fields[field] = value
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func NewAccessLogDecorator(handler http.Handler, logWriter io.WriteCloser, rowFillerContextKey interface{},
	rowFillerFactory AccessLogRowFillerFactory) *AccessLogDecorator {
	logger := logrus.New()
	logger.Out = logWriter
	return &AccessLogDecorator{
		handler,
		rowFillerContextKey,
		rowFillerFactory,
		logger,
	}
}

func (d *AccessLogDecorator) Stop() {
	d.logger.Out.(io.WriteCloser).Close()
}

func (d *AccessLogDecorator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	beginTime := time.Now()
	row := &AccessLogRow{
		make(logrus.Fields),
	}

	if d.rowFillerContextKey != nil && d.rowFillerFactory != nil {
		rowFiller := d.rowFillerFactory(row)
		r = r.WithContext(context.WithValue(r.Context(), d.rowFillerContextKey, rowFiller))
	}

	sw := &statusResponseWriter{
		w,
		0,
	}

	d.Handler.ServeHTTP(sw, r)

	row.SetRowField("beginTime", beginTime.String())
	row.SetRowField("status", sw.status)
	row.SetRowField("duration", time.Now().Sub(beginTime).Seconds())
	row.SetRowField("remote", r.RemoteAddr)
	row.SetRowField("method", r.Method)
	row.SetRowField("uri", r.URL.RequestURI())
	logrus.WithFields(row.fields).Info()
}
