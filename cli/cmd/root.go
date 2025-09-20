package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	animesucc "github.com/mrschyzo/animesucc/core"
	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var rootCmd = &cobra.Command{
	Use:   "animesucc <search>",
	Short: "Download anime episodes from AnimeSaturn",
	Args:  cobra.ExactArgs(1),
	RunE:  run,
}

func init() {
	rootCmd.Flags().StringP("episodes", "e", "", `Episode filter (e.g. "1-5,8,10-12")`)
	rootCmd.Flags().IntP("position", "p", 0, "Pick Nth search result (1-based)")
	rootCmd.Flags().StringP("output", "o", ".", "Output directory")
	rootCmd.Flags().IntP("parallel", "j", 3, "Max concurrent downloads")
}

func Execute() error {
	return rootCmd.Execute()
}

func run(cmd *cobra.Command, args []string) error {
	query := args[0]
	episodeFilter, _ := cmd.Flags().GetString("episodes")
	position, _ := cmd.Flags().GetInt("position")
	outputDir, _ := cmd.Flags().GetString("output")
	parallel, _ := cmd.Flags().GetInt("parallel")

	ctx := context.Background()
	client := animesucc.NewClient()

	// 1. Search
	fmt.Printf("Searching for %q...\n", query)
	results, err := client.Search(ctx, query)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}
	if len(results) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	// 2. Pick result
	var chosen animesucc.SearchResult
	if position > 0 {
		if position > len(results) {
			return fmt.Errorf("position %d out of range (1-%d)", position, len(results))
		}
		chosen = results[position-1]
	} else {
		chosen, err = promptSelection(results)
		if err != nil {
			return err
		}
	}
	fmt.Printf("Selected: %s\n", chosen.Name)

	// 3. Get episodes
	fmt.Println("Fetching episode list...")
	episodes, err := client.GetEpisodes(ctx, chosen.Link)
	if err != nil {
		return fmt.Errorf("fetching episodes: %w", err)
	}
	if len(episodes) == 0 {
		fmt.Println("No episodes found.")
		return nil
	}

	// 4. Apply filter
	if episodeFilter != "" {
		filter, err := animesucc.ParseEpisodeFilter(episodeFilter)
		if err != nil {
			return fmt.Errorf("invalid episode filter: %w", err)
		}
		episodes = filter.Apply(episodes)
		if len(episodes) == 0 {
			fmt.Println("No episodes match the filter.")
			return nil
		}
	}
	fmt.Printf("Downloading %d episode(s) to %s\n", len(episodes), outputDir)

	// 5. Ensure output dir exists
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// 6. Download
	progress := mpb.New(mpb.WithWidth(64))
	reporter := &cliProgressReporter{
		animeName: sanitizeFilename(chosen.Name),
		progress:  progress,
		bars:      make(map[int]*barState),
	}

	output := fileOutputFactory(outputDir, sanitizeFilename(chosen.Name))

	err = client.DownloadAll(ctx, episodes, animesucc.DownloadOptions{MaxParallel: parallel}, output, reporter)

	progress.Wait()

	if err != nil {
		return err
	}
	return nil
}

func promptSelection(results []animesucc.SearchResult) (animesucc.SearchResult, error) {
	for i, r := range results {
		state := ""
		if r.State == "0" {
			state = " [ongoing]"
		}
		fmt.Printf("  %d. %s (%s)%s\n", i+1, r.Name, r.Release, state)
	}
	fmt.Print("\nSelect (1-", len(results), "): ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return animesucc.SearchResult{}, fmt.Errorf("reading input: %w", err)
	}
	line = strings.TrimSpace(line)
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(results) {
		return animesucc.SearchResult{}, fmt.Errorf("invalid selection: %q", line)
	}
	return results[n-1], nil
}

func fileOutputFactory(dir, animeName string) animesucc.OutputFactory {
	return func(_ context.Context, ep animesucc.Episode) (io.WriteSeeker, func() error, error) {
		filename := fmt.Sprintf("%s_Ep_%03d.mp4", animeName, ep.Number)
		path := filepath.Join(dir, filename)
		f, err := os.Create(path)
		if err != nil {
			return nil, nil, err
		}
		return f, f.Close, nil
	}
}

func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(name)
}

type barState struct {
	bar            *mpb.Bar
	lastDownloaded int64
	lastUpdated    time.Time
}

// cliProgressReporter implements animesucc.ProgressReporter using mpb progress bars.
type cliProgressReporter struct {
	animeName string
	progress  *mpb.Progress
	mu        sync.Mutex
	bars      map[int]*barState
}

func (r *cliProgressReporter) OnEpisodeStart(ep animesucc.Episode, index, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := fmt.Sprintf("Ep %d", ep.Number)
	bar := r.progress.AddBar(0,
		mpb.PrependDecorators(
			decor.Name(name, decor.WCSyncSpaceR),
			decor.CountersKibiByte("% .2f / % .2f"),
		),
		mpb.AppendDecorators(
			decor.EwmaETA(decor.ET_STYLE_GO, 30),
			decor.Name(" "),
			decor.EwmaSpeed(decor.SizeB1024(0), "% .2f", 30),
		),
	)
	r.bars[ep.Number] = &barState{bar: bar, lastUpdated: time.Now()}
}

func (r *cliProgressReporter) OnProgress(ep animesucc.Episode, downloaded, total int64) {
	r.mu.Lock()
	state, ok := r.bars[ep.Number]
	r.mu.Unlock()
	if !ok {
		return
	}

	now := time.Now()
	delta := downloaded - state.lastDownloaded
	elapsed := now.Sub(state.lastUpdated)
	state.lastDownloaded = downloaded
	state.lastUpdated = now

	if total >= 0 {
		state.bar.SetTotal(total, false)
	}
	if delta > 0 && elapsed > 0 {
		state.bar.EwmaIncrBy(int(delta), elapsed)
	}
}

func (r *cliProgressReporter) OnEpisodeComplete(ep animesucc.Episode, err error) {
	r.mu.Lock()
	state, ok := r.bars[ep.Number]
	r.mu.Unlock()
	if !ok {
		return
	}

	if err != nil {
		state.bar.Abort(true)
	} else {
		state.bar.SetTotal(-1, true)
	}
}

func (r *cliProgressReporter) OnAllComplete(succeeded, failed int) {
	fmt.Printf("\nDone: %d succeeded, %d failed\n", succeeded, failed)
}
