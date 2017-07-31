package kellyframework

import (
	"net/http"
	"context"
	"time"
	"os"
	"fmt"
	"path"
	"strings"
	"strconv"
	"sync"
)

type AccessLogDecorator struct {
	http.Handler
	rowFillerContextKey interface{}
	rowFillerFactory    AccessLogRowFillerFactory
	logger              *logger
}

type AccessLogOptions struct {
	pathPrefix    string
	maxSize       int
	maxTime       string
	fieldSequence []string
}

type logger struct {
	pathPrefix      string
	maxSize         int
	maxTime         time.Duration
	fieldSequence   []string
	validRowFields  map[string]bool
	logFile         *os.File
	fileLineChannel chan string
	stopWait        *sync.WaitGroup
}

type AccessLogRow map[string]interface{}
type AccessLogRowFiller interface{}
type AccessLogRowFillerFactory func(AccessLogRow) AccessLogRowFiller

const timeLayout = "20060102-15-04"
const sequenceStart = 1

func newAccessLogRow() AccessLogRow {
	return make(AccessLogRow)
}

func (row AccessLogRow) SetRowField(field string, value interface{}) {
	row[field] = value
}

func newLogger(options *AccessLogOptions) (l *logger, err error) {
	validRowFields := make(map[string]bool)
	for _, v := range options.fieldSequence {
		validRowFields[v] = true
	}

	maxTime, err := time.ParseDuration(options.maxTime)
	if err != nil {
		return
	} else if maxTime < time.Minute {
		// maxTime can not less than 1 minute.
		maxTime = time.Minute
	}

	l = &logger{
		path.Clean(options.pathPrefix),
		options.maxSize,
		maxTime,
		options.fieldSequence,
		validRowFields,
		nil,
		make(chan string, 1048576),
		new(sync.WaitGroup),
	}

	l.stopWait.Add(1)
	go func() {
		defer func() {
			l.logFile.Close()
			l.stopWait.Done()
		}()

		for line := range l.fileLineChannel {
			l.writeFileLine(line)
		}
	}()

	return
}

func (l *logger) stop() {
	close(l.fileLineChannel)
	l.stopWait.Wait()
}

func (l *logger) refreshLogFile() {
	var expectedFilename string

	if l.logFile != nil {
		expectedFilename = l.expectedLogFilePath()
		if l.currentLogFilePath() == expectedFilename {
			return
		} else {
			l.logFile.Close()
			l.logFile = nil
		}
	} else {
		expectedFilename = l.buildLogFilePath(time.Now(), sequenceStart)
	}

	var err error
	l.logFile, err = os.OpenFile(expectedFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}

	return
}

func (l *logger) currentLogFilePath() string {
	return path.Clean(l.logFile.Name())
}

func (l *logger) buildLogFilePath(time time.Time, seq int) string {
	// the log file name is in this format: prefix.20060102-15-04.0001 .
	return fmt.Sprintf("%s.%s.%04d", l.pathPrefix, time.Local().Format(timeLayout), seq)
}

func (l *logger) expectedLogFilePath() string {
	// extract time and sequence number from current filename.
	filePath := l.currentLogFilePath()
	suffix := strings.TrimPrefix(filePath, l.pathPrefix+".")
	parts := strings.Split(suffix, ".")
	timePart := parts[0]
	seqPart := parts[1]

	fileTime, err := time.ParseInLocation(timeLayout, timePart, time.Local)
	if err != nil {
		panic(err)
	}

	fileSeq, err := strconv.Atoi(seqPart)
	if err != nil {
		panic(err)
	}

	// make expected filename.
	expectedTime := fileTime
	expectedSeq := fileSeq

	now := time.Now()
	if l.maxTime < now.Sub(fileTime) {
		expectedTime = now
		expectedSeq = sequenceStart
	} else {
		finfo, err := l.logFile.Stat()
		if err != nil {
			panic(err)
		} else if finfo.Size() >= int64(l.maxSize) {
			expectedSeq++
		}
	}

	return l.buildLogFilePath(expectedTime, expectedSeq)
}

func (l *logger) writeLogRow(row AccessLogRow) {
	values := make([]interface{}, len(row))
	for i, field := range l.fieldSequence {
		if _, ok := l.validRowFields[field]; !ok {
			panic(fmt.Sprintf("unknown row field: %s", field))
		} else {
			values[i] = row[field]
		}
	}

	l.fileLineChannel <- fmt.Sprintln(values...)
}

func (l *logger) writeFileLine(line string) {
	l.refreshLogFile()

	ret, err := l.logFile.WriteString(line)
	if err != nil {
		panic(err)
	} else if ret < len(line) {
		// unlikely, but should care.
		panic(fmt.Errorf("written length less than expected"))
	}
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func NewAccessLogDecorator(handler http.Handler, options *AccessLogOptions, rowFillerContextKey interface{},
	rowFillerFactory AccessLogRowFillerFactory) (d *AccessLogDecorator, err error) {
	l, err := newLogger(options)
	if err != nil {
		return
	}

	d = &AccessLogDecorator{
		handler,
		rowFillerContextKey,
		rowFillerFactory,
		l,
	}

	return
}

func (d *AccessLogDecorator) Stop() {
	d.logger.stop()
}

func (d *AccessLogDecorator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	beginTime := time.Now()
	row := newAccessLogRow()

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
	d.logger.writeLogRow(row)
}
