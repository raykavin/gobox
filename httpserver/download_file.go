package httpserver

import (
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// DownloadFile writes content to the HTTP response as a downloadable file.
func DownloadFile(
	writer http.ResponseWriter,
	filename string,
	contentType string,
	content []byte,
) error {
	filename = sanitizeFilename(filename)

	if contentType == "" {
		contentType = detectContentType(filename, content)
	}

	headers := writer.Header()

	headers.Set("Content-Type", contentType)
	headers.Set("Content-Length", strconv.Itoa(len(content)))
	headers.Set("Content-Disposition", contentDisposition(filename))
	headers.Set("Cache-Control", "no-store, no-cache, must-revalidate")
	headers.Set("Pragma", "no-cache")
	headers.Set("Expires", "0")
	headers.Set("X-Content-Type-Options", "nosniff")

	writer.WriteHeader(http.StatusOK)

	_, err := writer.Write(content)
	return err
}

func detectContentType(filename string, content []byte) string {
	if extension := filepath.Ext(filename); extension != "" {
		if contentType := mime.TypeByExtension(extension); contentType != "" {
			return contentType
		}
	}

	if len(content) > 0 {
		return http.DetectContentType(content)
	}

	return "application/octet-stream"
}

func contentDisposition(filename string) string {
	if filename == "" {
		return "attachment"
	}

	return mime.FormatMediaType(
		"attachment",
		map[string]string{
			"filename": filename,
		},
	)
}

func sanitizeFilename(filename string) string {
	filename = strings.ReplaceAll(filename, `\`, "/")
	filename = path.Base(filename)

	filename = strings.Map(func(character rune) rune {
		switch character {
		case '\r', '\n', '\x00':
			return -1
		default:
			return character
		}
	}, filename)

	if filename == "." || filename == "/" {
		return ""
	}

	return filename
}
