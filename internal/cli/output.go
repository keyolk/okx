package cli

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
)

// writeJSON prints v as indented JSON on stdout.
func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// table is a thin wrapper over tabwriter for the aligned text output.
type table struct {
	w *tabwriter.Writer
}

func newTable(out io.Writer) *table {
	return &table{w: tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)}
}

func (t *table) row(cells ...string) {
	fmt.Fprintln(t.w, strings.Join(cells, "\t"))
}

func (t *table) flush() { _ = t.w.Flush() }

// writeCSV prints rows with a header on stdout.
func writeCSV(header []string, rows [][]string) error {
	w := csv.NewWriter(os.Stdout)
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write(r); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// confirm asks the user to approve a mutation. A non-interactive stdin is
// treated as a refusal: an unattended run must not silently apply writes.
//
// The check is on the file descriptor being a terminal, not on the file mode —
// /dev/null is a character device and would otherwise pass.
func confirm(prompt string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, errors.New("stdin is not a terminal; pass --yes to confirm non-interactively")
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false, nil // EOF or a bare newline both mean "no"
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
