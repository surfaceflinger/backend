package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"

	"database/sql"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	_ "github.com/uptrace/bun/driver/pgdriver"
)

const logPath = "log.txt"

func setupLogger(verbose bool) {
	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: time.Stamp,
		FullTimestamp:   true,
	})
	if verbose {
		logrus.SetLevel(logrus.DebugLevel)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logrus.Fatalf("Failed to open log file %s for output: %s", logPath, err)
	}

	logrus.SetOutput(io.MultiWriter(os.Stderr, logFile))
	logrus.RegisterExitHandler(func() {
		if logFile == nil {
			return
		}
		logFile.Close()
	})
}

func logHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestLog(r).Infoln("Handling request.")
		next.ServeHTTP(w, r)
	})
}

func openDb(pgDsn string) *bun.DB {
	sqldb, err := sql.Open("pg", pgDsn)
	if err != nil {
		logrus.WithError(err).Errorln("Database open failed.")
	}
	defer sqldb.Close()
	return bun.NewDB(sqldb, pgdialect.New())
}

func createHttpHandler(db *bun.DB) http.Handler {
	router := mux.NewRouter()
	router.NotFoundHandler = router.NewRoute().BuildOnly().HandlerFunc(notFoundHandler).GetHandler()
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "dzialam")
	})

	versionRouter := router.PathPrefix("/version").Subrouter()
	versionController := VersionController{Repo: &PgVersionRepo{DB: db}}
	versionRouter.HandleFunc("/latest", versionController.ServeLatestVersions).Methods("GET")

	return logHandler(router)
}

func awaitInterruption() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}

func shutdown(ctx context.Context, server *http.Server) {
	err := server.Shutdown(ctx)
	if err != nil {
		logrus.WithError(err).Warningln("Http server shutdown failed.")
	}
	logrus.Exit(0)
}

func main() {
	flag.Parse()
	setupLogger(os.Getenv("verbose") == "true")
	logrus.Infoln("Starting backend.")

	pgDsn := os.Getenv("POSTGRES_DSN")
	if pgDsn == "" {
		logrus.Errorln("Environment variable POSTGRES_DSN is not set!")
	}

	logrus.Infoln("Opening database.")
	db := openDb(pgDsn)

	logrus.Infoln("Creating http handler.")
	h := createHttpHandler(db)
	server := &http.Server{Addr: "127.0.0.1:2137", Handler: h}
	go server.ListenAndServe()

	logrus.Infoln("Starting listening... To shut down use ^C")

	awaitInterruption()
	logrus.Infoln("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdown(ctx, server)
}
