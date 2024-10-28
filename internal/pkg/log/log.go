package log

import (
	log "github.com/sirupsen/logrus"
)

type typedLog struct {
	Proxy     *log.Entry
	Collector *log.Entry
	General   *log.Entry
}

var (
	Logger *typedLog
)

// Init logger on start
func init() {
	Logger = &typedLog{
		Proxy:     log.WithFields(log.Fields{"module": "proxy"}),
		Collector: log.WithFields(log.Fields{"module": "collector"}),
		General:   log.WithFields(log.Fields{"module": "general"}),
	}
}

func Setup(lvl string) error {
	logLevel, err := log.ParseLevel(lvl)
	if err != nil {
		return err
	}

	// log format
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	log.SetLevel(logLevel)

	return nil
}
