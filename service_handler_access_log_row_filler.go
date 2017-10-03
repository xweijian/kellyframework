package kellyframework

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
