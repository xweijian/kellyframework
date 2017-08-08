package kellyframework

import (
	"testing"
	"time"
	"fmt"
)

func Test_logger(t *testing.T) {
	t.Run("new logger error options", func(t *testing.T) {
		_, err := newLogger(&AccessLogOptions{
			"/tmp/abc",
			100,
			"3ss",
			[]string{
				"a", "b", "c", "d",
			},
		})
		if err == nil {
			t.Errorf("newLogger() error = %v", err)
			return
		}
	})

	var logger *logger
	t.Run("new logger", func(t *testing.T) {
		l, err := newLogger(&AccessLogOptions{
			"/tmp/abc",
			100,
			"3s",
			[]string{
				"a", "b", "c", "d",
			},
		})
		if err != nil {
			t.Errorf("newLogger() error = %v", err)
			return
		}

		logger = l
	})

	t.Run("write log row", func(t *testing.T) {
		row := newAccessLogRow()
		row.SetRowField("a", 1)
		row.SetRowField("b", "fsafas")
		row.SetRowField("c", &struct {
			a int
			b string
			c interface{}
		}{1, "abc", nil})
		row.SetRowField("d", nil)
		logger.writeLogRow(row)

		row = newAccessLogRow()
		row.SetRowField("a", 1)
		row.SetRowField("b", "fsafas")
		row.SetRowField("c", &struct {
			a int
			b string
			c interface{}
		}{1, "abc", nil})
		row.SetRowField("d", nil)
		logger.writeLogRow(row)
		time.Sleep(3 * time.Second)
	})

	t.Run("refresh log file path", func(t *testing.T) {
		path1 := logger.currentLogFilePath()
		fmt.Printf("path1 %s\n", path1)

		row := newAccessLogRow()
		row.SetRowField("a", "llllllllllllllllllllllllllllllllllllllllllllllllll")
		row.SetRowField("b", "llllllllllllllllllllllllllllllllllllllllllllllllll")
		row.SetRowField("c", &struct {
			a int
			b string
			c interface{}
		}{1, "abc", nil})
		row.SetRowField("d", nil)
		logger.writeLogRow(row)
		time.Sleep(3 * time.Second)

		path2 := logger.currentLogFilePath()
		fmt.Printf("path2 %s\n", path2)

		row = newAccessLogRow()
		row.SetRowField("a", 1)
		row.SetRowField("b", 2)
		row.SetRowField("c", 3)
		row.SetRowField("d", nil)
		logger.writeLogRow(row)
		time.Sleep(3 * time.Second)

		path3 := logger.currentLogFilePath()
		fmt.Printf("path3 %s\n", path3)
		if path2 == path3 {
			t.Errorf("path2 %s, path3 %s\n", path2, path3)
		}

		time.Sleep(61 * time.Second)

		row = newAccessLogRow()
		row.SetRowField("a", 1)
		row.SetRowField("b", 2)
		row.SetRowField("c", 3)
		row.SetRowField("d", nil)
		logger.writeLogRow(row)
		time.Sleep(3 * time.Second)

		path4 := logger.currentLogFilePath()
		fmt.Printf("path4 %s\n", path4)
		if path3 == path4 {
			t.Errorf("path3 %s, path4 %s\n", path3, path4)
		}

		logger.stop()
	})
}
