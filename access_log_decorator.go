package kellyframework

import (
	"net/http"
	"context"
	"time"
	"io"
	"github.com/Sirupsen/logrus"
	"strconv"
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

func (row *AccessLogRow) SetRowField(field string, value string) {
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

func NewAccessLogDecorator(handler http.Handler, logWriter io.Writer, rowFillerContextKey interface{},
	rowFillerFactory AccessLogRowFillerFactory) *AccessLogDecorator {
	logger := logrus.New()
	logger.Formatter = &logrus.TextFormatter{DisableTimestamp: true}
	logger.Out = logWriter
	return &AccessLogDecorator{
		handler,
		rowFillerContextKey,
		rowFillerFactory,
		logger,
	}
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
		http.StatusOK,
	}

	d.Handler.ServeHTTP(sw, r)

	row.SetRowField("beginTime", beginTime.Format("2006-01-02 03:04:05.999999999"))
	row.SetRowField("status", strconv.Itoa(sw.status))
	row.SetRowField("duration", strconv.FormatFloat(time.Now().Sub(beginTime).Seconds(), 'f', -1, 64))
	row.SetRowField("remote", r.RemoteAddr)
	row.SetRowField("httpMethod", r.Method)
	row.SetRowField("uri", r.URL.RequestURI())
	if sw.status < http.StatusBadRequest {
		d.logger.WithFields(row.fields).Info()
	} else {
		d.logger.WithFields(row.fields).Error()
	}
}
