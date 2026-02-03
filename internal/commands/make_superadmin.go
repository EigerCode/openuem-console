package commands

import (
	"fmt"
	"log"

	"github.com/EigerCode/openuem-console/internal/models"
	"github.com/urfave/cli/v2"
)

func MakeSuperAdmin() *cli.Command {
	return &cli.Command{
		Name:  "make-superadmin",
		Usage: "Make a user a super admin",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "dburl",
				Usage:    "the Postgres database connection url",
				EnvVars:  []string{"DATABASE_URL"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "username",
				Usage:    "the username to make super admin",
				Required: true,
			},
		},
		Action: makeSuperAdmin,
	}
}

func makeSuperAdmin(cCtx *cli.Context) error {
	dbURL := cCtx.String("dburl")
	username := cCtx.String("username")

	// Connect to database
	model, err := models.New(dbURL, "pgx", "")
	if err != nil {
		log.Fatalf("[FATAL]: could not connect to database: %v", err)
	}
	defer model.Client.Close()

	// Set user as super admin
	if err := model.SetSuperAdmin(username, true); err != nil {
		log.Fatalf("[FATAL]: could not set super admin: %v", err)
	}

	fmt.Printf("✅ Successfully set '%s' as SuperAdmin!\n", username)
	return nil
}
