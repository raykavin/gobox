package logger

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fatih/color"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Color scheme for components
var (
	timestampColor = color.New(color.FgHiCyan, color.Italic)
	callerColor    = color.New(color.FgHiMagenta)
	messageColor   = color.New(color.FgWhite)
	fieldKeyColor  = color.New(color.FgHiYellow)
	fieldValColor  = color.New(color.FgCyan)
)

// consoleFormatter handles console output formatting
type consoleFormatter struct {
	config *Config
}

// formatLevel formats the log level
func (f *consoleFormatter) formatLevel(i any) string {
	levelStr, ok := i.(string)
	if !ok {
		return f.formatUnknownLevel()
	}

	level, exists := logLevels[levelStr]
	if !exists {
		return f.formatUnknownLevel()
	}

	return level.Color.Sprintf(" %s ", level.Text)
}

// formatUnknownLevel formats unknown levels
func (f *consoleFormatter) formatUnknownLevel() string {
	emoji := ""
	if f.config.UseEmoji {
		emoji = "❓ "
	}
	return color.New(color.FgHiWhite).Sprintf(" %sUNKN ", emoji)
}

// formatMessage formats the log message
func (f *consoleFormatter) formatMessage(i any) string {
	msg, ok := i.(string)
	if !ok || len(msg) == 0 {
		return messageColor.Sprint("│ (mensagem vazia)")
	}

	// Handle multiline messages
	if strings.Contains(msg, "\n") {
		return f.formatMultilineMessage(msg)
	}

	// Truncate and pad single line messages
	if len(msg) > maxMessageSize {
		msg = msg[:maxMessageSize]
	} else {
		msg = fmt.Sprintf("%-*s", maxMessageSize, msg)
	}

	return messageColor.Sprintf("│ %s", msg)
}

// formatMultilineMessage formats messages with multiple lines
func (*consoleFormatter) formatMultilineMessage(msg string) string {
	lines := strings.Split(msg, "\n")
	formatted := make([]string, len(lines))

	for i, line := range lines {
		formatted[i] = messageColor.Sprintf("│ %s", line)
	}

	return strings.Join(formatted, "\n")
}

// formatCaller formats the caller information
func (f *consoleFormatter) formatCaller(i any) string {
	fname, ok := i.(string)
	if !ok || len(fname) == 0 {
		return ""
	}

	caller := filepath.Base(fname)
	parts := strings.Split(caller, ":")
	if len(parts) != 2 {
		return callerColor.Sprintf("┤ %s ├", caller)
	}

	file := f.formatFileName(parts[0])
	line := f.formatLineNumber(parts[1])

	return callerColor.Sprintf("┤ %s:%s ├", file, line)
}

// formatFileName formats the file name
func (*consoleFormatter) formatFileName(name string) string {
	file := strings.TrimSuffix(name, ".go")
	if len(file) > maxFileSize {
		return file[:maxFileSize]
	}
	return fmt.Sprintf("%-*s", maxFileSize, file)
}

// formatLineNumber formats the line number
func (*consoleFormatter) formatLineNumber(line string) string {
	if len(line) > maxLineSize {
		return line[len(line)-maxLineSize:]
	}
	return fmt.Sprintf("%0*s", maxLineSize, line)
}

// formatTimestamp formats the timestamp
func (f *consoleFormatter) formatTimestamp(i any) string {
	strTime, ok := i.(string)
	if !ok {
		return timestampColor.Sprintf("[ %v ]", i)
	}

	ts, err := time.ParseInLocation(time.RFC3339, strTime, time.Local)
	if err != nil {
		return timestampColor.Sprintf("[ %s ]", strTime)
	}

	formatted := ts.In(time.Local).Format(f.config.DateTimeLayout)
	return timestampColor.Sprintf("[ %s ]", formatted)
}

// formatFieldName formats field names
func (*consoleFormatter) formatFieldName(i any) string {
	name, ok := i.(string)
	if !ok {
		return fmt.Sprintf("%v", i)
	}
	return fieldKeyColor.Sprint(name)
}

// formatError formats the error field in a structured way.
func (*consoleFormatter) formatError(i any) string {
	caser := cases.Title(language.Und)

	errStr := extractErrorString(i)
	if errStr == "" {
		return ""
	}

	var result strings.Builder
	// errStyle := color.New(color.FgHiRed, color.Bold)
	// _, _ = result.WriteString(errStyle.Sprint("\nDetalhes do Erro:"))

	parts := strings.Split(errStr, "]: ")

	for idx, part := range parts {
		part = strings.TrimSpace(part)
		if idx < len(parts)-1 {
			part += "]"
		}
		isLast := idx == len(parts)-1
		writeErrorTree(&result, part, "", isLast, caser)
	}

	return result.String()
}

// writeKeyValueTree prints key=value pairs as tree nodes
func writeKeyValueTree(sb *strings.Builder, kvStr, prefix string, caser cases.Caser) {
	errStyle := color.New(color.FgHiRed, color.Bold)
	boldStyle := color.New(color.Bold)

	// Regex to extract key=value properly
	re := regexp.MustCompile(`(\S+?)=([^\n]+?)(?:, |$)`)
	matches := re.FindAllStringSubmatch(kvStr, -1)

	for i, match := range matches {
		treeSymbol := "├──"
		if i == len(matches)-1 {
			treeSymbol = "└──"
		}
		key := caser.String(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(match[1]), "_", " ")))
		value := strings.TrimSpace(match[2])

		valueLines := strings.Split(value, "\n")
		for j, line := range valueLines {
			symbol := treeSymbol
			if j > 0 {
				symbol = "│   "
			}
			_, _ = fmt.Fprintf(sb, "\n%s%s %s: %s", prefix, errStyle.Sprint(symbol), boldStyle.Sprint(key), errStyle.Sprint(line))
		}
	}
}

// formatFieldValue formats field values
func (*consoleFormatter) formatFieldValue(i any) string {
	switch v := i.(type) {
	case string:
		// Only quote strings that contain special characters
		if strings.ContainsAny(v, " \t\n\r\"'") {
			return "=" + fieldValColor.Sprintf("%q", v)
		}
		return "=" + fieldValColor.Sprint(v)
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return fieldValColor.Sprintf("=%d", v)
	case float32, float64:
		return fieldValColor.Sprintf("=%.2f", v)
	case bool:
		if v {
			return "=" + color.HiGreenString("verdadeiro")
		}
		return "=" + color.HiRedString("falso")
	case nil:
		return "=" + color.HiBlackString("nulo")
	default:
		return fieldValColor.Sprintf("=%v", v)
	}
}
