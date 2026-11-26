package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"clawreef/internal/systembackuptrial"
	"github.com/go-sql-driver/mysql"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "trial preparation accepts configuration through deployment-mounted files and environment")
		os.Exit(2)
	}
	log.SetOutput(io.Discard)
	_ = mysql.SetLogger(&mysql.NopLogger{})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	if err := systembackuptrial.New(os.Getenv).PrepareEnvironment(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("trial environment prepared")
}
