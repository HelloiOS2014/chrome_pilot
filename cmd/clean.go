package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/tmpfile"
	"github.com/spf13/cobra"
)

var cleanBefore string
var cleanDryRun bool

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean temporary files",
	RunE: func(cmd *cobra.Command, args []string) error {
		dataDir, err := config.DataDir()
		if err != nil {
			return err
		}
		mgr, err := tmpfile.NewManager(dataDir+"/tmp", 24*time.Hour)
		if err != nil {
			return err
		}

		// Default: remove everything (10 years is effectively "all").
		dur := 24 * time.Hour * 365 * 10
		if cleanBefore != "" {
			dur, err = parseDuration(cleanBefore)
			if err != nil {
				return err
			}
		}

		if cleanDryRun {
			count, size := mgr.DryRun(dur)
			fmt.Printf("{\"count\":%d,\"size\":\"%s\"}\n", count, formatBytes(size))
			return nil
		}

		count, freed, _ := mgr.Clean(dur)
		fmt.Printf("{\"deleted\":%d,\"freed\":\"%s\"}\n", count, formatBytes(freed))
		return nil
	},
}

func init() {
	cleanCmd.Flags().StringVar(&cleanBefore, "before", "", "Clean files older than (e.g., 3d, 1h)")
	cleanCmd.Flags().BoolVar(&cleanDryRun, "dry-run", false, "Preview without deleting")
	rootCmd.AddCommand(cleanCmd)
}

// parseDuration extends time.ParseDuration with a "d" (days) suffix.
// Examples: "3d", "1h", "30m", "2d12h".
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}

	// Handle plain days suffix, e.g. "3d" or "2d12h".
	// Replace each "d" (preceded by digits) with the equivalent hours.
	var total time.Duration
	rest := s
	for {
		idx := strings.IndexByte(rest, 'd')
		if idx == -1 {
			break
		}
		// Find the start of the numeric part before 'd'.
		numStart := idx
		for numStart > 0 && rest[numStart-1] >= '0' && rest[numStart-1] <= '9' {
			numStart--
		}
		// Parse and accumulate any duration before the number.
		if numStart > 0 {
			prefix := rest[:numStart]
			d, err := time.ParseDuration(prefix)
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q: %w", s, err)
			}
			total += d
		}
		days, err := strconv.Atoi(rest[numStart:idx])
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		total += time.Duration(days) * 24 * time.Hour
		rest = rest[idx+1:]
	}
	// Parse remaining standard Go duration (e.g. "12h", "30m").
	if rest != "" {
		d, err := time.ParseDuration(rest)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		total += d
	}
	return total, nil
}

// formatBytes converts a byte count to a human-readable string (e.g. "1.2 MB").
func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
