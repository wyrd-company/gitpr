package tui

import (
	"bytes"
	"context"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"

	"github.com/wyrd-company/gitpr/internal/gitutil"
	"github.com/wyrd-company/gitpr/internal/model"
)

var (
	diffAddStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	diffDeleteStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	diffContextStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
	diffMetaStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	commentMetaStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("221")).Bold(true)
	commentBodyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	commentBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("221")).
				Bold(true)
	chromaFormatter = formatters.Get("terminal256")
	chromaStyle     = styles.Get("github")
)

type fileHighlight struct {
	old []string
	new []string
}

func buildHighlightCache(ctx context.Context, pr model.PR) map[string]fileHighlight {
	repo, err := gitutil.Open(pr.RepositoryRoot)
	if err != nil {
		return nil
	}

	cache := map[string]fileHighlight{}
	for _, file := range pr.FileDiffs {
		displayPath := file.NewPath
		if displayPath == "" {
			displayPath = file.OldPath
		}

		var oldContent string
		if content, err := repo.FileContentAtRef(ctx, pr.BaseHeadSHA, file.OldPath); err == nil {
			oldContent = content
		} else if err != nil && !os.IsNotExist(err) {
			continue
		}

		var newContent string
		if content, err := repo.FileContentAtRef(ctx, pr.SourceHeadSHA, file.NewPath); err == nil {
			newContent = content
		} else if err != nil && !os.IsNotExist(err) {
			continue
		}

		cache[displayPath] = fileHighlight{
			old: highlightFileLines(preferValue(file.OldPath, displayPath), oldContent),
			new: highlightFileLines(preferValue(file.NewPath, displayPath), newContent),
		}
	}

	return cache
}

func highlightFileLines(filePath, content string) []string {
	if content == "" {
		return nil
	}
	if chromaFormatter == nil || chromaStyle == nil {
		return splitHighlightedLines(content)
	}

	lexer := lexers.Match(filePath)
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		return splitHighlightedLines(content)
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return splitHighlightedLines(content)
	}

	var buf bytes.Buffer
	if err := chromaFormatter.Format(&buf, chromaStyle, iterator); err != nil {
		return splitHighlightedLines(content)
	}

	return splitHighlightedLines(strings.TrimRight(buf.String(), "\n"))
}

func splitHighlightedLines(content string) []string {
	if content == "" {
		return nil
	}

	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func renderDiffCell(kind, prefix, highlighted, fallback string) string {
	content := fallback
	if highlighted != "" || fallback == "" {
		content = highlighted
	}

	switch kind {
	case "add":
		return diffAddStyle.Render(prefix) + content
	case "delete":
		return diffDeleteStyle.Render(prefix) + content
	case "meta":
		return diffMetaStyle.Render(prefix) + content
	default:
		return diffContextStyle.Render(prefix) + content
	}
}

func highlightedLine(lines []string, line int) (string, bool) {
	if line <= 0 || line > len(lines) {
		return "", false
	}
	return lines[line-1], true
}

func preferValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
