package gomigration

import (
	"context"
	"log"
	"strconv"

	"github.com/spf13/cobra"
)

type CliConfig struct {
	GoMigration *GoMigration
	CliName     string
}

type Cli struct {
	migration *GoMigration
	cliName   string
}

func NewCli(config CliConfig) (*Cli, error) {
	if config.GoMigration == nil {
		return nil, ErrGoMigrationNotProvided
	}
	if config.CliName == "" {
		config.CliName = "migration"
	}

	return &Cli{
		migration: config.GoMigration,
		cliName:   config.CliName,
	}, nil
}

func (c *Cli) ListCommand(ctx context.Context) *cobra.Command {
	var listCmd = &cobra.Command{
		Use:   "list",
		Short: "List all migrations",
		Run: func(cmd *cobra.Command, args []string) {
			list, err := c.migration.List(ctx)
			if err != nil {
				log.Println("Error listing migrations:", err)
				return
			}
			list.Print()
		},
	}

	return listCmd
}

func (c *Cli) MigrateCommand(ctx context.Context) *cobra.Command {
	var migrateCmd = &cobra.Command{
		Use:   "migrate",
		Short: "Run all pending migrations",
		Run: func(cmd *cobra.Command, args []string) {
			fresh := false
			var err error
			freshFlag := cmd.Flags().Lookup("fresh")
			if freshFlag != nil && freshFlag.Changed {
				fresh, err = strconv.ParseBool(freshFlag.Value.String())
				if err != nil {
					log.Println("Invalid fresh flag:", err)
					return
				}
			}
			if fresh {
				err = c.migration.Fresh(ctx)
				if err != nil {
					log.Println("Error running fresh migrations:", err)
					return
				}
			} else {
				err = c.migration.Migrate(ctx)
				if err != nil {
					log.Println("Error running migrations:", err)
					return
				}
			}
		},
	}

	migrateCmd.Flags().BoolP("fresh", "f", false, "Run fresh migrations")

	return migrateCmd
}

func (c *Cli) RollbackCommand(ctx context.Context) *cobra.Command {
	var rollbackCmd = &cobra.Command{
		Use:   "rollback",
		Short: "Rollback the last migration",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			var err error
			step := 1
			stepFlag := cmd.Flags().Lookup("step")
			if stepFlag != nil && stepFlag.Changed {
				step, err = strconv.Atoi(stepFlag.Value.String())
				if err != nil {
					log.Println("Invalid step:", err)
					return
				}
				if step < 1 {
					log.Println("Step must be greater than 0")
					return
				}
			}

			err = c.migration.Rollback(ctx, step)
			if err != nil {
				log.Println("Error rolling back migrations:", err)
				return
			}
		},
	}

	rollbackCmd.Flags().IntP("step", "s", 1, "Number of migrations to rollback")

	return rollbackCmd
}

func (c *Cli) ResetCommand(ctx context.Context) *cobra.Command {
	var resetCmd = &cobra.Command{
		Use:   "reset",
		Short: "Rollback all migrations and re-run all migrations",
		Run: func(cmd *cobra.Command, args []string) {
			err := c.migration.Reset(ctx)
			if err != nil {
				log.Println("Error resetting migrations:", err)
				return
			}
		},
	}

	return resetCmd
}

func (c *Cli) CleanCommand(ctx context.Context) *cobra.Command {
	var cleanCmd = &cobra.Command{
		Use:   "clean",
		Short: "Clean database (delete all tables)",
		Run: func(cmd *cobra.Command, args []string) {
			err := c.migration.Clean(ctx)
			if err != nil {
				log.Println("Error cleaning database:", err)
				return
			}
		},
	}

	return cleanCmd
}

func (c *Cli) CreateCommand(ctx context.Context) *cobra.Command {
	var createCmd = &cobra.Command{
		Use:   "create",
		Short: "Create a new migration",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			migrationName := args[0]
			err := c.migration.Create(migrationName)
			if err != nil {
				log.Println("Error creating migration:", err)
				return
			}
		},
	}

	return createCmd
}

func (c *Cli) Execute(ctx context.Context) error {
	var rootCmd = &cobra.Command{
		Use: c.cliName,
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
		Short: "GoMigration CLI",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	rootCmd.AddCommand(
		c.ListCommand(ctx),
		c.MigrateCommand(ctx),
		c.RollbackCommand(ctx),
		c.ResetCommand(ctx),
		c.CleanCommand(ctx),
		c.CreateCommand(ctx),
	)

	return rootCmd.Execute()
}
