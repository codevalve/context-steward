package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/spf13/cobra"

	"context-steward/internal/config"
	"context-steward/internal/db"
	"context-steward/internal/packer"
)

var (
	packetBudget  int
	packetOut     string
	packetPreview bool
)

var packetCmd = &cobra.Command{
	Use:   "packet [task | session-start]",
	Short: "Generate context packets for AI sessions",
	Long: `Generate task-specific or session-start markdown context packets under a strict token budget.
If the argument is 'session-start', a general project baseline packet is built.
Otherwise, the arguments are treated as a task description, and relevant files are ranked and packed.`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve workspace root
		wsPath := workspace
		var err error
		if wsPath == "" {
			wsPath, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %w", err)
			}
		}
		wsPath, err = filepath.Abs(wsPath)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute workspace path: %w", err)
		}

		stewardDir := filepath.Join(wsPath, ".context-steward")
		dbPath := filepath.Join(stewardDir, "index.sqlite")

		// Load config
		cfg, err := config.LoadConfig(cfgFile, wsPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration (run 'init' first): %w", err)
		}

		// Use flag budget or config default
		budget := packetBudget
		if budget == 0 {
			budget = cfg.Packets.DefaultBudget
		}

		// Open DB connection
		dbConn, err := db.InitDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer dbConn.Close()

		tokenizer := packer.NewHeuristicTokenizer(4.2)
		var packetText string
		packetType := "task"
		taskName := args[0]

		if len(args) == 1 && args[0] == "session-start" {
			packetType = "session-start"
			var errBuild error
			packetText, errBuild = packer.BuildSessionStartPacket(dbConn, cfg, tokenizer, budget)
			if errBuild != nil {
				return fmt.Errorf("failed to build session-start packet: %w", errBuild)
			}
		} else {
			taskName = strings.Join(args, " ")
			var errBuild error
			packetText, errBuild = packer.BuildTaskSpecificPacket(dbConn, cfg, tokenizer, taskName, budget)
			if errBuild != nil {
				return fmt.Errorf("failed to build task packet: %w", errBuild)
			}
		}

		// Save packet record in database
		if !dryRun {
			p := db.Packet{
				PacketType:  packetType,
				Task:        taskName,
				TokenBudget: budget,
				PacketText:  packetText,
				CreatedAt:   time.Now(),
				Stale:       false,
			}
			_ = db.SavePacket(dbConn, &p)
		}

		// Handle output modes
		if packetOut != "" {
			if !dryRun {
				err = os.WriteFile(packetOut, []byte(packetText), 0644)
				if err != nil {
					return fmt.Errorf("failed to write packet to file: %w", err)
				}
				if !quiet {
					fmt.Printf("Successfully wrote context packet to: %s\n", packetOut)
				}
			}
		} else if packetPreview {
			// Render with glamour
			renderer, errRenderer := glamour.NewTermRenderer(
				glamour.WithAutoStyle(),
				glamour.WithWordWrap(100),
			)
			if errRenderer != nil {
				fmt.Print(packetText) // fallback to standard printing
			} else {
				rendered, errRender := renderer.Render(packetText)
				if errRender != nil {
					fmt.Print(packetText) // fallback
				} else {
					fmt.Print(rendered)
				}
			}
		} else {
			// Default: print plain text to stdout
			fmt.Print(packetText)
		}

		return nil
	},
}

func init() {
	packetCmd.Flags().IntVarP(&packetBudget, "budget", "b", 0, "token budget limit (defaults to config limit)")
	packetCmd.Flags().StringVarP(&packetOut, "out", "o", "", "file path to write the output to")
	packetCmd.Flags().BoolVarP(&packetPreview, "preview", "p", false, "preview rendered markdown in terminal")
	rootCmd.AddCommand(packetCmd)
}
