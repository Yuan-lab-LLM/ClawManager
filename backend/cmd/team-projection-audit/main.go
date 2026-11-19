// team-projection-audit exports an exact historical Team snapshot for human
// review. Applying a reviewed manifest never guesses or repairs business facts.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/maintenancecli"
	"clawreef/internal/repository"
	"github.com/upper/db/v4"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	team := flag.Int("team", 0, "Team ID to export")
	output := flag.String("output", "", "new private audit manifest file")
	apply := flag.String("apply", "", "reviewed manifest file to apply")
	reviewed := flag.Bool("reviewed", false, "confirm the named reviewer verified every business effect, outbound obligation, and retained source window")
	flag.Parse()
	if (*apply == "" && (*team <= 0 || *output == "")) || (*apply != "" && (!*reviewed || *team != 0 || *output != "")) {
		return fmt.Errorf("use -team ID -output NEW_FILE, or -apply REVIEWED_FILE -reviewed")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// Connect does not migrate, provision resources, or start application workers.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var sess db.Session
	var release func()
	if *apply != "" {
		sess, release, err = maintenancecli.Open(ctx, cfg.Database)
	} else {
		sess, err = appdb.Connect(cfg.Database)
	}
	if err != nil {
		return err
	}
	if release != nil {
		defer release()
	} else {
		defer sess.Close()
	}
	repo := repository.NewTeamRepository(sess).(interface {
		ExportTeamProjectionAudit(context.Context, int) (json.RawMessage, error)
		ApplyTeamProjectionAudit(context.Context, json.RawMessage) error
	})
	if *apply != "" {
		file, err := os.Open(*apply)
		if err != nil {
			return err
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, (40<<20)+1))
		if err != nil {
			return err
		}
		if len(raw) > 40<<20 {
			return fmt.Errorf("audit file exceeds 40 MiB")
		}
		if err = repo.ApplyTeamProjectionAudit(ctx, raw); err != nil {
			return err
		}
		fmt.Println("Reviewed Team projection audit committed; unresolved retry obligations still block trimming.")
		return nil
	}
	raw, err := repo.ExportTeamProjectionAudit(ctx, *team)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	fmt.Println("Private snapshot exported. It does not authorize trimming until reviewed evidence and every legacy disposition are applied.")
	return nil
}
