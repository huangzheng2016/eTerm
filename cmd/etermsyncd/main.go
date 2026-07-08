package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/glebarez/sqlite"
	"github.com/huangzheng2016/eTerm/internal/debugpprof"
	"github.com/huangzheng2016/eTerm/internal/syncd"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	listen := flag.String("listen", ":8443", "HTTP listen address")
	dbPath := flag.String("db", "sync.db", "SQLite database path")
	apiKey := flag.String("api-key", "", "Bearer token for auth (env: ETERMSYNCD_API_KEY)")
	certFile := flag.String("cert", "", "TLS certificate file")
	keyFile := flag.String("key", "", "TLS key file")
	stdio := flag.Bool("stdio", false, "Run in stdio mode (JSON over stdin/stdout)")
	pprofAddr := flag.String("pprof", "", "enable pprof HTTP server on address (env: ETERMSYNCD_PPROF_ADDR)")
	flag.Parse()

	if *apiKey == "" {
		*apiKey = os.Getenv("ETERMSYNCD_API_KEY")
	}
	if _, err := debugpprof.Start("etermsyncd", debugpprof.ResolveAddr(*pprofAddr, "ETERMSYNCD_PPROF_ADDR")); err != nil {
		log.Fatalf("pprof: %v", err)
	}

	db, err := gorm.Open(sqlite.Open(*dbPath+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)

	engine, err := syncd.NewEngine(db)
	if err != nil {
		log.Fatalf("init engine: %v", err)
	}

	if *stdio {
		if err := syncd.RunStdio(engine); err != nil {
			log.Fatalf("stdio: %v", err)
		}
		return
	}

	// HTTP mode requires an API key
	if *apiKey == "" {
		log.Fatal("--api-key or ETERMSYNCD_API_KEY is required in HTTP mode")
	}

	handler := syncd.NewHTTPHandler(engine, *apiKey)
	fmt.Fprintf(os.Stderr, "etermsyncd listening on %s\n", *listen)

	if *certFile != "" && *keyFile != "" {
		log.Fatal(http.ListenAndServeTLS(*listen, *certFile, *keyFile, handler))
	} else {
		log.Fatal(http.ListenAndServe(*listen, handler))
	}
}
