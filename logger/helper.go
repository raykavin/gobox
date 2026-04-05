package logger

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/text/cases"
)

// createJSONLogger creates a JSON formatted logger
func createJSONLogger(config *Config) zerolog.Logger {
	return log.Output(zerolog.ConsoleWriter{
		Out:           os.Stdout,
		NoColor:       !config.Colored,
		TimeFormat:    config.DateTimeLayout,
		PartsOrder:    []string{"time", "level", "caller", "message"},
		FieldsExclude: []string{"caller"},
	})
}

// createConsoleLogger creates a console formatted logger
func createConsoleLogger(config *Config) zerolog.Logger {
	output := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		NoColor:    !config.Colored,
		TimeFormat: config.DateTimeLayout,
		PartsOrder: []string{"time", "level", "caller", "message"},
	}

	if config.Colored {
		// Create formatter with config
		formatter := &consoleFormatter{config: config}

		output.FormatMessage = formatter.formatMessage
		output.FormatCaller = formatter.formatCaller
		output.FormatLevel = formatter.formatLevel
		output.FormatTimestamp = formatter.formatTimestamp
		output.FormatFieldName = formatter.formatFieldName
		output.FormatFieldValue = formatter.formatFieldValue
		output.FormatErrFieldValue = formatter.formatError
	}

	return log.Output(output)
}

func extractErrorString(i any) string {
	switch v := i.(type) {
	case error:
		return v.Error()
	case string:
		return v
	default:
		return ""
	}
}

// writeErrorTree prints any error part recursively with proper tree symbols
func writeErrorTree(sb *strings.Builder, part, prefix string, isLast bool, caser cases.Caser) {
	errStyle := color.New(color.FgHiRed, color.Bold)
	boldStyle := color.New(color.Bold)

	treeSymbol := "├──"
	nextPrefix := prefix + "│   "
	if isLast {
		treeSymbol = "└──"
		nextPrefix = prefix + "    "
	}

	part = strings.Trim(part, "\"")

	// Match message with bracket details
	if matches := regexp.MustCompile(`^([^\[]+)\[(.+)\]$`).FindStringSubmatch(part); len(matches) == 3 {
		_, _ = fmt.Fprintf(
			sb,
			"\n%s%s %s",
			errStyle.Sprint(prefix),
			errStyle.Sprint(treeSymbol),
			errStyle.Sprint(strings.TrimSpace(matches[1])),
		)
		writeKeyValueTree(sb, matches[2], nextPrefix, caser)
		return
	}

	// Handle Cause:Error
	if subParts := strings.SplitN(part, ": ", 2); len(subParts) == 2 {
		_, _ = fmt.Fprintf(
			sb,
			"\n%s%s %s",
			errStyle.Sprint(prefix),
			errStyle.Sprint(treeSymbol),
			errStyle.Sprint(strings.TrimSpace(subParts[0])),
		)

		errorDetails := subParts[1]

		if detailParts := strings.SplitN(errorDetails, ": ", 2); len(detailParts) == 2 {
			_, _ = fmt.Fprintf(
				sb,
				"\n%s%s %s %s",
				nextPrefix,
				errStyle.Sprint("├──"),
				boldStyle.Sprint("Cause:"),
				errStyle.Sprint(strings.TrimSpace(detailParts[0])),
			)

			_, _ = fmt.Fprintf(
				sb,
				"\n%s%s %s %s",
				nextPrefix,
				errStyle.Sprint("└──"),
				boldStyle.Sprint("Error:"),
				errStyle.Sprint(strings.TrimSpace(detailParts[1])),
			)
		} else {
			_, _ = fmt.Fprintf(
				sb,
				"\n%s%s %s",
				nextPrefix,
				errStyle.Sprint("└──"),
				errStyle.Sprint(errorDetails),
			)
		}
		return
	}

	// Just print the message
	_, _ = fmt.Fprintf(
		sb,
		"\n%s%s %s",
		errStyle.Sprint(prefix),
		errStyle.Sprint(treeSymbol),
		errStyle.Sprint(part),
	)
}
